package gui

import (
	"bytes"
	"emanuelelongo/proview/pkg/document"
	"fmt"
	"image"
	"image/color"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func (g *Gui) openImageWindow(doc *document.Document) {
	editor := g.newEditor()
	editor.showDocument(doc)
	editor.saveTarget = nil
	editor.window.Show()
}

// Generated images reuse an empty window and preserve any open document.
func (g *Gui) openNewImage(doc *document.Document) {
	if !g.hasOpenContent() {
		g.showDocument(doc)
		g.saveTarget = nil
		return
	}
	g.openImageWindow(doc)
}

func (g *Gui) newPage() {
	if driver, ok := g.app.Driver().(desktop.Driver); ok && driver.CurrentKeyModifiers()&fyne.KeyModifierShift != 0 {
		g.newFromClipboard()
		return
	}
	g.newBlankPage()
}

func (g *Gui) newBlankPage() {
	if g.waitAnnotationChanges(g.newBlankPage) || g.working {
		return
	}
	if g.document != nil && g.document.Format() == document.FormatPDF {
		doc, page := g.document, g.activePDFPage
		snapshot := doc.RenderingSnapshot()
		g.runFileWork("Adding blank page", func() (*document.Document, error) {
			return snapshot.BlankPageAfter(page)
		}, func(blank *document.Document) {
			if err := doc.InsertBlankPage(page, blank); err != nil {
				blank.Close()
				g.showOperationError("add_blank_page", err)
				return
			}
			g.rebuildPDFWithCompletion(page+1, func() { g.revealThumbnail(page + 1) })
		})
		return
	}
	g.newBlankImage()
}

func (g *Gui) newBlankImage() {
	if g.working {
		return
	}
	width, height := widget.NewEntry(), widget.NewEntry()
	width.SetText("1200")
	height.SetText("800")
	background := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	var swatch *colorSwatch
	swatch = newColorSwatch(background, func() {
		picker := dialog.NewColorPicker("Background", "Choose a background color", func(c color.Color) {
			background = color.NRGBAModel.Convert(c).(color.NRGBA)
			swatch.setColor(background)
		}, g.window)
		picker.Advanced = true
		picker.Show()
		picker.SetColor(background)
	})
	var form *dialog.CustomDialog
	create := widget.NewButton("Create", func() {
		w, err := strconv.Atoi(width.Text)
		if err != nil {
			dialog.ShowError(fmt.Errorf("Enter a whole number for width"), g.window)
			return
		}
		h, err := strconv.Atoi(height.Text)
		if err != nil {
			dialog.ShowError(fmt.Errorf("Enter a whole number for height"), g.window)
			return
		}
		g.runFileWork("Creating image", func() (*document.Document, error) { return document.NewImage("Untitled.png", w, h, background) }, func(doc *document.Document) { form.Hide(); g.openNewImage(doc) })
	})
	dimensions := container.NewGridWithColumns(2,
		container.NewVBox(widget.NewLabel("Width (px)"), width),
		container.NewVBox(widget.NewLabel("Height (px)"), height),
	)
	backgroundRow := container.New(layout.NewCustomPaddedHBoxLayout(16),
		widget.NewLabel("Background"), container.NewGridWrap(fyne.NewSize(36, 36), swatch),
	)
	body := container.New(layout.NewCustomPaddedVBoxLayout(20), dimensions, backgroundRow)
	content := container.New(layout.NewCustomPaddedLayout(12, 16, 12, 12), body)
	form = dialog.NewCustomWithoutButtons("New page", content, g.window)
	create.Importance = widget.HighImportance
	form.SetButtons([]fyne.CanvasObject{widget.NewButton("Cancel", form.Hide), create})
	form.Resize(fyne.NewSize(440, 280))
	form.Show()
}

func (g *Gui) deactivatePDFCrops() {
	for _, crop := range g.pdfCrops {
		crop.Deactivate()
	}
}
func (g *Gui) startPDFCrop() {
	page := g.activePDFPage
	crop := g.pdfCrops[page]
	if crop == nil {
		return
	}
	if crop.active {
		crop.Deactivate()
		return
	}
	g.deactivatePDFCrops()
	g.clearSignatureSelection()
	g.setAnnotationTool(toolSelect)
	size, err := g.document.PageSize(page)
	if err != nil {
		g.showOperationError("file_work", err)
		return
	}
	crop.Activate(image.Rectangle{Max: size}, func(rect image.Rectangle) {
		snapshot := g.document.RenderingSnapshot()
		name := strings.TrimSuffix(g.document.Name(), filepath.Ext(g.document.Name())) + fmt.Sprintf("-page-%d-crop.png", page+1)
		g.runFileWork("Cropping page", func() (*document.Document, error) { return snapshot.CropPage(page, rect, name) }, g.openImageWindow)
	})
}

// Expensive loading, rendering and encoding never run in the event callback.
func (g *Gui) runFileWork(title string, work func() (*document.Document, error), done func(*document.Document)) {
	started := time.Now()
	g.logEvent("operation_started", "operation=%q", title)
	g.working = true
	progress := dialog.NewCustomWithoutButtons(title, widget.NewProgressBarInfinite(), g.window)
	progress.Show()
	go func() {
		doc, err := work()
		fyne.Do(func() {
			progress.Hide()
			g.working = false
			defer g.processPendingFiles()
			if err != nil {
				g.showOperationError(title, err)
				return
			}
			g.logEvent("operation_completed", "operation=%q elapsed=%s", title, time.Since(started).Round(time.Millisecond))
			done(doc)
		})
	}()
}

type pdfInput struct {
	name, location string
	data           []byte
}

func (g *Gui) openPDFCollection(appendPages bool) {
	if g.waitAnnotationChanges(func() { g.openPDFCollection(appendPages) }) || g.working {
		return
	}
	if appendPages && (g.document == nil || g.document.Format() != document.FormatPDF) {
		dialog.ShowInformation("Append PDF", "Open a PDF first.", g.window)
		return
	}
	launch := func() { g.choosePDFCollection(appendPages) }
	if appendPages {
		launch()
	} else {
		g.confirmDiscard(launch)
	}
}
func (g *Gui) choosePDFCollection(appendPages bool) {
	var inputs []pdfInput
	selected := -1
	list := widget.NewList(func() int { return len(inputs) }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(i widget.ListItemID, o fyne.CanvasObject) {
		o.(*widget.Label).SetText(fmt.Sprintf("%d. %s", i+1, inputs[i].name))
	})
	list.OnSelected = func(i widget.ListItemID) { selected = i }
	list.OnUnselected = func(widget.ListItemID) { selected = -1 }
	var chooser *dialog.CustomDialog
	collectionOpen := true
	add := widget.NewButtonWithIcon("Add PDFs", tablerIcon("plus"), func() {
		g.chooseFiles("Add PDFs", []string{".pdf"}, true, func(readers []fyne.URIReadCloser, err error) {
			if !collectionOpen {
				closePickedFiles(readers)
				return
			}
			if err != nil {
				g.showOperationError("file_work", err)
				return
			}
			if len(readers) == 0 {
				return
			}
			g.working = true
			go func() {
				var added []pdfInput
				var readErr error
				for _, reader := range readers {
					data, err := io.ReadAll(reader)
					if err != nil {
						readErr = err
						break
					}
					added = append(added, pdfInput{reader.URI().Name(), reader.URI().String(), data})
				}
				for _, reader := range readers {
					if err := reader.Close(); readErr == nil {
						readErr = err
					}
				}
				fyne.Do(func() {
					g.working = false
					defer g.processPendingFiles()
					if !collectionOpen || g.closed {
						return
					}
					if readErr != nil {
						dialog.ShowError(readErr, g.window)
						return
					}
					inputs = append(inputs, added...)
					list.Refresh()
					list.Select(len(inputs) - 1)
				})
			}()
		})
	})
	move := func(delta int) {
		i := selected
		j := i + delta
		if i < 0 || j < 0 || j >= len(inputs) {
			return
		}
		inputs[i], inputs[j] = inputs[j], inputs[i]
		list.Refresh()
		list.Select(j)
	}
	remove := newIconButton(tablerIcon("trash"), "Delete", func() {
		if selected < 0 || selected >= len(inputs) {
			return
		}
		inputs = append(inputs[:selected], inputs[selected+1:]...)
		list.UnselectAll()
		list.Refresh()
	})
	label, title := "Open", "Open PDFs together"
	if appendPages {
		label, title = "Append", "Append PDFs"
	}
	submit := widget.NewButton(label, func() {
		if g.working || len(inputs) == 0 {
			return
		}
		selectedInputs := append([]pdfInput(nil), inputs...)
		current := g.document
		if appendPages && (current == nil || current.Format() != document.FormatPDF) {
			dialog.ShowInformation("Append PDF", "Open a PDF first.", g.window)
			return
		}
		g.runFileWork("Loading PDFs", func() (*document.Document, error) {
			sources := make([]*document.Source, 0, len(selectedInputs))
			for _, input := range selectedInputs {
				source, err := document.LoadFrom(input.name, input.location, bytes.NewReader(input.data))
				if err != nil {
					for _, s := range sources {
						s.Close()
					}
					return nil, err
				}
				sources = append(sources, source)
			}
			if appendPages { // Mutation belongs to the UI callback; only load in worker.
				prepared, err := document.New(sources[0])
				if err != nil {
					return nil, err
				}
				if err = prepared.AppendPDFs(sources[1:]...); err != nil {
					return nil, err
				}
				return prepared, nil
			}
			combined, err := document.New(sources[0])
			if err != nil {
				return nil, err
			}
			if err = combined.AppendPDFs(sources[1:]...); err != nil {
				return nil, err
			}
			combined.MarkSaved()
			if len(selectedInputs) > 1 {
				combined.MarkUnsaved()
			}
			_, err = combined.Preview(0)
			return combined, err
		}, func(loaded *document.Document) {
			chooser.Hide()
			if appendPages {
				if err := current.AppendDocumentPages(loaded); err != nil {
					g.showOperationError("file_work", err)
					return
				}
				g.rebuildPDF(g.activePDFPage)
			} else {
				g.showDocument(loaded)
				g.saveTarget = nil
			}
		})
	})
	scroll := container.NewGridWrap(fyne.NewSize(520, 280), list)
	controls := container.NewHBox(add, newIconButton(tablerIcon("arrow-up"), "Move up", func() { move(-1) }), newIconButton(tablerIcon("arrow-down"), "Move down", func() { move(1) }), remove)
	chooser = dialog.NewCustom(title, "Cancel", container.NewVBox(widget.NewLabel("Add PDFs, then arrange their order before opening."), scroll, controls, submit), g.window)
	chooser.SetOnClosed(func() { collectionOpen = false })
	chooser.Show()
}

func (g *Gui) deletePDFPage(page int) {
	if g.waitAnnotationChanges(func() { g.deletePDFPage(page) }) || g.working {
		return
	}
	if g.document == nil || g.document.Format() != document.FormatPDF {
		dialog.ShowInformation("Pages", "Open a PDF first.", g.window)
		return
	}
	if g.document.PageCount() == 1 {
		dialog.ShowInformation("Pages", "Keep at least one page.", g.window)
		return
	}
	if page < 0 || page >= g.document.PageCount() {
		return
	}
	g.cancelThumbnailDrag()
	order := make([]int, 0, g.document.PageCount()-1)
	for i := 0; i < g.document.PageCount(); i++ {
		if i != page {
			order = append(order, i)
		}
	}
	active := g.activePDFPage
	if active > page {
		active--
	} else if active == page {
		active = min(page, len(order)-1)
	}
	if err := g.document.SetPageOrder(order); err != nil {
		g.showOperationError("file_work", err)
		return
	}
	g.rebuildPDFWithCompletion(active, func() { g.revealThumbnail(active) })
}

func (g *Gui) rebuildPDF(active int) { g.rebuildPDFWithCompletion(active, nil) }

func (g *Gui) rebuildPDFWithCompletion(active int, done func()) {
	doc := g.document
	zoom := g.zoom
	snapshot := doc.RenderingSnapshot()
	g.runFileWork("Updating pages", func() (*document.Document, error) { _, err := snapshot.Preview(0); return doc, err }, func(doc *document.Document) {
		g.showPDF(doc)
		g.zoom = zoom
		g.applyZoom()
		if active < 0 || active >= doc.PageCount() {
			active = 0
		}
		g.activatePDFPage(active)
		g.pdfScroll.ScrollToOffset(fyne.NewPos(0, g.pdfPageTop(active)))
		g.loadVisiblePDFPages()
		g.loadVisiblePDFThumbnails()
		if done != nil {
			done()
		}
	})
}

func (g *Gui) duplicatePDFPage(page int) {
	if g.waitAnnotationChanges(func() { g.duplicatePDFPage(page) }) || g.working || g.document == nil {
		return
	}
	if err := g.document.DuplicatePage(page); err != nil {
		g.showOperationError("file_work", err)
		return
	}
	g.cancelThumbnailDrag()
	g.rebuildPDFWithCompletion(page+1, func() { g.revealThumbnail(page + 1) })
}
