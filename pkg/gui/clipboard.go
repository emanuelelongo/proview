package gui

import (
	"bytes"
	"context"
	"emanuelelongo/proview/pkg/document"
	img "emanuelelongo/proview/pkg/images"
	"emanuelelongo/proview/pkg/signatures"
	"errors"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"golang.design/x/clipboard"
	"image"
	"image/png"
	"math"
	"sync"
	"time"
)

type imageClipboard interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
}
type systemImageClipboard struct{}

var clipboardInit sync.Once
var clipboardInitError error

func initImageClipboard() error {
	clipboardInit.Do(func() { clipboardInitError = clipboard.Init() })
	return clipboardInitError
}
func (systemImageClipboard) Read(ctx context.Context) ([]byte, error) {
	if e := initImageClipboard(); e != nil {
		return nil, e
	}
	return clipboard.Read(ctx, clipboard.FmtImage)
}
func (systemImageClipboard) Write(ctx context.Context, data []byte) error {
	if e := initImageClipboard(); e != nil {
		return e
	}
	_, e := clipboard.Write(ctx, clipboard.FmtImage, data)
	return e
}

var desktopImageClipboard imageClipboard = systemImageClipboard{}

func (g *Gui) textClipboardShortcut(copy bool) bool {
	if g.window == nil {
		return false
	}
	focused := g.window.Canvas().Focused()
	if focused == nil {
		return false
	}
	var shortcut fyne.Shortcut = &fyne.ShortcutPaste{Clipboard: g.window.Clipboard()}
	if copy {
		shortcut = &fyne.ShortcutCopy{Clipboard: g.window.Clipboard()}
	}
	switch e := focused.(type) {
	case *annotationEntry:
		e.TypedShortcut(shortcut)
		return true
	case *widget.Entry:
		e.TypedShortcut(shortcut)
		return true
	}
	return false
}
func (g *Gui) copyArea() {
	if g.textClipboardShortcut(true) {
		return
	}
	if g.waitAnnotationChanges(g.copyArea) || g.working || g.document == nil {
		return
	}
	if o, ok := g.selectedOverlay(); ok {
		g.copySelectedObject(o)
		return
	}
	if g.validTextSelection() {
		s := g.textSelection
		g.window.Clipboard().SetContent(s.layout.Text(s.start, s.end))
		return
	}
	if g.areaSelection == nil {
		return
	}
	g.copyPageArea(g.areaSelection.page, g.areaSelection.rect)
}
func (g *Gui) copyPageArea(page int, rect document.NormalizedRect) {
	selected := areaSelection{page: page, rect: rect}
	selection := g.areaSelection
	if selection != nil && (selection.page != page || selection.rect != rect || selection.active) {
		selection = nil
	}
	doc := g.document
	snapshot := doc.RenderingSnapshot()
	g.queueAnnotation(func() func() {
		p, e := snapshot.SafeRenderPage(selected.page, 144)
		var data []byte
		if e == nil {
			rect := image.Rect(int(math.Round(selected.rect.X*float64(p.Bounds().Dx()))), int(math.Round(selected.rect.Y*float64(p.Bounds().Dy()))), int(math.Round((selected.rect.X+selected.rect.Width)*float64(p.Bounds().Dx()))), int(math.Round((selected.rect.Y+selected.rect.Height)*float64(p.Bounds().Dy())))).Intersect(p.Bounds())
			if rect.Empty() {
				e = fmt.Errorf("Select an area to copy")
			} else {
				var b bytes.Buffer
				e = png.Encode(&b, img.Crop(p, rect))
				data = b.Bytes()
			}
		}
		if e == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			e = desktopImageClipboard.Write(ctx, data)
		}
		return func() {
			if g.document != doc || g.closed {
				return
			}
			if e != nil {
				g.showOperationError("clipboard_copy", fmt.Errorf("Could not copy image: %w", e))
				return
			}
			g.showAreaCopied(doc, selection)
		}
	})
}
func (g *Gui) pasteImage() {
	if g.textClipboardShortcut(false) {
		return
	}
	if g.waitAnnotationChanges(g.pasteImage) || g.working || g.document == nil {
		return
	}
	doc := g.document
	page := g.activePDFPage
	layer := g.signatureLayers[page]
	if doc.Format() != document.FormatPDF {
		page = 0
		layer = g.imageSignatureLayer
	}
	if layer == nil {
		return
	}
	_, display := layer.displayRect()
	centerX, centerY := .5, .5
	if doc.Format() == document.FormatPDF && g.pdfScroll != nil && display.Height > 0 {
		top := g.pdfPageTop(page)
		a := math.Max(0, float64(g.pdfScroll.Offset.Y-top))
		b := math.Min(float64(display.Height), float64(g.pdfScroll.Offset.Y+g.pdfScroll.Size().Height-top))
		if b > a {
			centerY = (a + b) / 2 / float64(display.Height)
		}
	}
	pageSize := layer.pageSize
	g.queueAnnotation(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var payload []byte
		var copied document.CopiedObject
		var prepared signatures.Prepared
		if backend, ok := desktopImageClipboard.(objectClipboard); ok {
			var err error
			payload, err = backend.ReadObject(ctx)
			if err != nil && !errors.Is(err, clipboard.ErrNoData) {
				return func() {
					if g.document == doc && !g.closed {
						g.showOperationError("clipboard", err)
					}
				}
			}
			if len(payload) > 0 {
				copied, err = document.DecodeCopiedObject(payload)
				if err == nil {
					prepared, err = copied.Asset.Prepare()
				}
				return func() {
					if g.document != doc || g.closed {
						return
					}
					if err != nil {
						g.showOperationError("clipboard_paste", fmt.Errorf("Could not paste object: %w", err))
						return
					}
					g.insertCopiedObject(doc, layer, page, copied, prepared, payload)
				}
			}
		}
		data, e := desktopImageClipboard.Read(ctx)
		var config image.Config
		if errors.Is(e, clipboard.ErrNoData) || (e == nil && len(data) == 0) {
			e = fmt.Errorf("The clipboard does not contain a supported image")
		}
		if e == nil {
			config, e = png.DecodeConfig(bytes.NewReader(data))
			if e == nil && (int64(config.Width)*int64(config.Height) > 16_000_000 || len(data) > 16<<20) {
				e = fmt.Errorf("Clipboard image exceeds 16 MB or 16 million pixels")
			}
		}
		return func() {
			if g.document != doc || g.closed {
				return
			}
			if e != nil {
				g.showOperationError("clipboard_paste", e)
				return
			}
			scale := math.Min(1, math.Min(float64(pageSize.X)*.8/float64(config.Width), float64(pageSize.Y)*.8/float64(config.Height)))
			w, h := float64(config.Width)*scale/float64(pageSize.X), float64(config.Height)*scale/float64(pageSize.Y)
			r := document.NormalizedRect{X: math.Min(math.Max(centerX-w/2, 0), 1-w), Y: math.Min(math.Max(centerY-h/2, 0), 1-h), Width: w, Height: h}
			o, e := doc.AddImage(page, r, data)
			if e != nil {
				g.showOperationError("clipboard_paste", e)
				return
			}
			g.setAnnotationTool(toolSelect)
			g.clearAreaSelection()
			layer.reload()
			g.selectSignature(layer, o.ID)
			g.documentChanged(page)
		}
	})
}

// newFromClipboard creates an independent image rather than an annotation.
func (g *Gui) newFromClipboard() {
	if g.waitAnnotationChanges(g.newFromClipboard) || g.working {
		return
	}
	g.runFileWork("Creating image from clipboard", func() (*document.Document, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		data, err := desktopImageClipboard.Read(ctx)
		if errors.Is(err, clipboard.ErrNoData) || (err == nil && len(data) == 0) {
			return nil, fmt.Errorf("The clipboard does not contain a supported image")
		}
		if err != nil {
			return nil, err
		}
		config, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if int64(config.Width)*int64(config.Height) > 16_000_000 || len(data) > 16<<20 {
			return nil, fmt.Errorf("Clipboard image exceeds 16 MB or 16 million pixels")
		}
		source, err := document.LoadFrom("Untitled.png", "", bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		doc, err := document.New(source)
		if err != nil {
			source.Close()
			return nil, err
		}
		doc.MarkUnsaved()
		return doc, nil
	}, g.openNewImage)
}
