package gui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strconv"
	"strings"
	"time"

	"emanuelelongo/proview/internal/markdown"
	"emanuelelongo/proview/internal/pdfengine"
	"emanuelelongo/proview/internal/printing"
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/widget"
)

type printBackend func(context.Context, printing.Job) error

type printState struct {
	ctx      context.Context
	cancel   context.CancelFunc
	snapshot *document.Document
	markdown *markdown.Document
	progress *dialog.CustomDialog
	started  time.Time
}

// Serialize native print panels across editor windows. Accessed on the UI thread.
var printOwner *Gui

func (g *Gui) requestPrint() {
	g.activateEditor()
	if printOwner != nil {
		printOwner.window.RequestFocus()
		return
	}
	if g.waitAnnotationChanges(g.requestPrint) {
		return
	}
	if g.closed || g.working || g.document == nil && !g.showingMarkdown() {
		return
	}
	g.window.Canvas().Focus(nil)
	ctx, cancel := context.WithCancel(context.Background())
	state := &printState{ctx: ctx, cancel: cancel, started: time.Now()}
	if g.showingMarkdown() {
		state.markdown = g.markdownView.doc
	} else {
		state.snapshot = g.document.RenderingSnapshot()
	}
	g.printJob, printOwner, g.working = state, g, true
	if state.markdown != nil || state.snapshot.Format() != document.FormatPDF || state.snapshot.PageCount() == 1 {
		g.preparePrint(state, nil)
		return
	}
	g.choosePrintPages(state)
}

func (g *Gui) choosePrintPages(state *printState) {
	ranges := widget.NewEntry()
	ranges.SetPlaceHolder("For example: 1-3, 5")
	ranges.Hide()
	choice := widget.NewSelect([]string{"All pages", "Current page", "Page range"}, func(value string) {
		if value == "Page range" {
			ranges.Show()
		} else {
			ranges.Hide()
		}
	})
	choice.SetSelected("All pages")
	current := min(max(g.activePDFPage, 0), state.snapshot.PageCount()-1)
	content := container.NewVBox(widget.NewLabel(fmt.Sprintf("%d pages · Current page: %d", state.snapshot.PageCount(), current+1)), choice, ranges)
	prompt := dialog.NewCustomWithoutButtons("Print pages", content, g.window)
	proceed := false
	prompt.SetOnClosed(func() {
		if !proceed {
			g.finishPrint(state, printing.ErrCanceled)
		}
	})
	continueButton := widget.NewButton("Continue", func() {
		var pages []int
		switch choice.Selected {
		case "Current page":
			pages = []int{current}
		case "Page range":
			var err error
			pages, err = parsePrintPages(ranges.Text, state.snapshot.PageCount())
			if err != nil {
				ranges.SetValidationError(err)
				return
			}
		}
		proceed = true
		prompt.Hide()
		g.preparePrint(state, pages)
	})
	continueButton.Importance = widget.HighImportance
	prompt.SetButtons([]fyne.CanvasObject{widget.NewButton("Cancel", prompt.Hide), continueButton})
	prompt.Show()
}

// parsePrintPages returns unique pages in document order, without ever iterating
// an unchecked user range. Input page numbers are one-based.
func parsePrintPages(value string, count int) ([]int, error) {
	selected := make(map[int]bool)
	invalid := fmt.Errorf("Enter page numbers from 1 to %d, for example 1-3, 5.", count)
	for _, part := range strings.Split(value, ",") {
		bounds := strings.Split(strings.TrimSpace(part), "-")
		if len(bounds) < 1 || len(bounds) > 2 {
			return nil, invalid
		}
		first, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
		if err != nil || first < 1 || first > count {
			return nil, invalid
		}
		last := first
		if len(bounds) == 2 {
			last, err = strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil || last < first || last > count {
				return nil, invalid
			}
		}
		for page := first - 1; page < last; page++ {
			selected[page] = true
		}
	}
	pages := make([]int, 0, len(selected))
	for page := 0; page < count; page++ {
		if selected[page] {
			pages = append(pages, page)
		}
	}
	if len(pages) == 0 {
		return nil, invalid
	}
	return pages, nil
}

func (g *Gui) preparePrint(state *printState, pages []int) {
	state.progress = dialog.NewCustomWithoutButtons("Preparing print", widget.NewProgressBarInfinite(), g.window)
	state.progress.SetButtons([]fyne.CanvasObject{widget.NewButton("Cancel", func() { g.finishPrint(state, printing.ErrCanceled) })})
	state.progress.Show()
	g.logEvent("print_started", "selected_pages=%d", len(pages))
	go func() {
		var job printing.Job
		var err error
		if state.markdown != nil {
			job, err = prepareMarkdownPrintJob(state.ctx, state.markdown)
		} else {
			job, err = preparePrintJob(state.ctx, state.snapshot, pages)
		}
		fyne.Do(func() {
			if g.closed || g.printJob != state {
				state.cancel()
				return
			}
			state.progress.Hide()
			if err != nil {
				g.finishPrint(state, err)
				return
			}
			if native, ok := g.window.(driver.NativeWindow); ok {
				native.RunNative(func(value any) {
					switch value := value.(type) {
					case driver.MacWindowContext:
						job.ParentWindow = value.NSWindow
					case driver.WindowsWindowContext:
						job.ParentWindow = value.HWND
					case driver.X11WindowContext:
						job.ParentID = fmt.Sprintf("x11:%x", value.WindowHandle)
						// Fyne exposes only the raw Wayland surface, not an exported
						// xdg-foreign token. The portal permits an unparented dialog.
					}
				})
			}
			backend := g.printBackend
			if backend == nil {
				backend = printing.Print
			}
			go func() {
				err := backend(state.ctx, job)
				fyne.Do(func() { g.finishPrint(state, err) })
			}()
		})
	}()
}

func preparePrintJob(ctx context.Context, snapshot *document.Document, pages []int) (printing.Job, error) {
	data, err := snapshot.PrintPDF(ctx, pages)
	if err != nil {
		return printing.Job{}, err
	}
	if len(pages) == 0 {
		pages = make([]int, snapshot.PageCount())
		for page := range pages {
			pages[page] = page
		}
	}
	job := printing.Job{Title: snapshot.Name(), PDF: data}
	for _, page := range pages {
		size, err := snapshot.PageSize(page)
		if err != nil {
			return printing.Job{}, err
		}
		scale := 1.
		if snapshot.Format() == document.FormatPDF {
			scale = .5
		} // 144 DPI preview to PDF points
		job.Pages = append(job.Pages, printing.PageSize{Width: float64(size.X) * scale, Height: float64(size.Y) * scale})
	}
	job.RenderPage = func(ctx context.Context, page int, dpi float64) (image.Image, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if snapshot.Format() != document.FormatPDF {
			return snapshot.RenderPage(0)
		}
		return renderPreparedPrintPage(ctx, data, page, dpi)
	}
	return job, nil
}

func prepareMarkdownPrintJob(ctx context.Context, source *markdown.Document) (printing.Job, error) {
	printed, err := source.PrintPDF(ctx)
	if err != nil {
		return printing.Job{}, err
	}
	job := printing.Job{Title: source.Name, PDF: printed.Data}
	for page := 0; page < printed.PageCount; page++ {
		job.Pages = append(job.Pages, printing.PageSize{Width: printed.Width, Height: printed.Height})
	}
	job.RenderPage = func(ctx context.Context, page int, dpi float64) (image.Image, error) {
		return renderPreparedPrintPage(ctx, printed.Data, page, dpi)
	}
	return job, nil
}

func renderPreparedPrintPage(ctx context.Context, data []byte, page int, dpi float64) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pixels, err := pdfengine.RenderPrintPage(data, page, dpi)
	if err == nil {
		err = ctx.Err()
	}
	return pixels, err
}

func (g *Gui) finishPrint(state *printState, err error) {
	if g.printJob != state {
		return
	}
	state.cancel()
	g.printJob, g.working = nil, false
	if printOwner == g {
		printOwner = nil
	}
	if g.closed {
		return
	}
	if state.progress != nil {
		state.progress.Hide()
	}
	switch {
	case errors.Is(err, printing.ErrCanceled), errors.Is(err, context.Canceled):
		g.logEvent("print_canceled", "")
	case err != nil:
		g.showOperationError("print", err)
	default:
		g.logEvent("print_completed", "elapsed=%s", time.Since(state.started).Round(time.Millisecond))
	}
	g.processPendingFiles()
}
