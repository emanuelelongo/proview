package gui

import (
	"bytes"
	"context"
	"emanuelelongo/proview/internal/mermaid"
	"image"
	"image/color"
	"image/png"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const maxMarkdownDiagrams = 16

func (v *markdownView) mermaidBlock(source string) fyne.CanvasObject {
	copyCode := newIconButton(theme.ContentCopyIcon(), "Copy Mermaid code", func() { v.editor.app.Clipboard().SetContent(source) })
	copyCode.Importance = widget.LowImportance
	header := container.NewBorder(nil, nil, nil, copyCode)
	original := v.mermaidSourceFallback(source)
	if len(v.diagrams) >= maxMarkdownDiagrams {
		label := widget.NewLabel("The preview limit for this document has been reached. You can copy the code or read it below.")
		label.Wrapping = fyne.TextWrapWord
		return container.NewBorder(header, nil, nil, nil, container.NewVBox(label, original))
	}
	original.Hide()
	preview := &markdownDiagram{view: v, source: source, image: canvas.NewImageFromImage(nil), status: widget.NewLabel("Rendering diagram…"), fallback: original, copyCode: copyCode}
	preview.image.FillMode = canvas.ImageFillContain
	preview.image.ScaleMode = canvas.ImageScaleSmooth
	preview.image.Hide()
	preview.status.Wrapping = fyne.TextWrapWord
	preview.ExtendBaseWidget(preview)
	preview.box = container.NewVBox(preview, original)
	v.diagrams = append(v.diagrams, preview)
	return container.NewBorder(header, nil, nil, nil, preview.box)
}
func (v *markdownView) mermaidSourceFallback(source string) fyne.CanvasObject {
	text := v.styleText(widget.NewRichText(&widget.TextSegment{Text: source, Style: widget.RichTextStyleCodeInline}))
	scroll := container.NewHScroll(container.NewPadded(text))
	return container.NewStack(scroll, newMarkdownCodeScrollSurface(v, scroll, text))
}

// Each widget coalesces size/theme changes into one active job and one latest
// request. Results belong to their original view and never publish after closing.
type markdownDiagram struct {
	widget.BaseWidget
	view                *markdownView
	source              string
	box                 *fyne.Container
	image               *canvas.Image
	status              *widget.Label
	fallback            fyne.CanvasObject
	copyCode            *iconButton
	result              mermaid.Result
	requested           mermaid.Options
	hasRequest, running bool
	generation          uint64
	err                 error
}

func (d *markdownDiagram) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(d.image, d.status))
}
func (d *markdownDiagram) MinSize() fyne.Size {
	height := float32(80)
	if d.result.Image != nil {
		width := min(float64(max(1, d.Size().Width)), d.result.Width)
		height = float32(d.result.Height * width / d.result.Width)
	} else if d.err != nil {
		height = max(40, d.status.MinSize().Height)
	}
	return fyne.NewSize(100, height)
}
func (d *markdownDiagram) Resize(size fyne.Size) {
	d.BaseWidget.Resize(size)
	d.request()
}
func (d *markdownDiagram) Refresh() {
	d.request()
	d.BaseWidget.Refresh()
}
func (d *markdownDiagram) options() (mermaid.Options, bool) {
	if d.Size().Width < 80 || d.view.diagramContext.Err() != nil {
		return mermaid.Options{}, false
	}
	scale := float64(1)
	if d.view.editor.window != nil {
		scale = float64(d.view.editor.window.Canvas().Scale())
	}
	background := theme.ColorForWidget(theme.ColorNameBackground, d)
	r, g, b, _ := background.RGBA()
	return mermaid.Options{
		Width: int(d.Size().Width) / 16 * 16, FontScale: float64(d.view.scale),
		PixelScale: math.Max(1, scale), Dark: r+g+b < 3*32768,
		Background: color.NRGBAModel.Convert(background).(color.NRGBA),
	}, true
}
func (d *markdownDiagram) request() {
	options, ready := d.options()
	if !ready {
		return
	}
	if d.hasRequest && options == d.requested {
		return
	}
	if d.hasRequest && options.Dark != d.requested.Dark {
		d.image.Hide()
		d.status.SetText("Rendering diagram…")
		d.status.Show()
	}
	d.hasRequest = true
	d.requested = options
	d.generation++
	if !d.running {
		d.start()
	}
}
func (d *markdownDiagram) start() {
	ctx := d.view.diagramContext
	if ctx.Err() != nil {
		return
	}
	options, generation := d.requested, d.generation
	d.running = true
	renderer := d.view.doc.Diagrams
	go func() {
		result, err := renderer.Render(ctx, d.source, options)
		if ctx.Err() != nil {
			return
		}
		fyne.Do(func() {
			if ctx.Err() != nil || d.view.editor.closed || d.view.editor.markdownView != d.view {
				return
			}
			d.running = false
			if generation != d.generation {
				d.start()
				return
			}
			d.view.updateDiagramLayout(func() {
				d.err = err
				if err != nil {
					d.result = mermaid.Result{}
					d.image.Hide()
					d.image.Image = nil
					d.status.SetText(mermaid.Message(err))
					d.status.Show()
					d.fallback.Show()
				} else {
					d.result = result
					d.fallback.Hide()
					d.image.Image = result.Image
					d.image.Show()
					d.status.Hide()
				}
				d.BaseWidget.Refresh()
			})
		})
	}()
}

// Restoring a retained view can precede its image heights. Reapply the intended
// offset as those heights arrive, until work settles or the reader scrolls.
func (v *markdownView) restoreReadingOffset(offset fyne.Position) {
	v.pendingReadingOffset = nil
	if len(v.diagrams) > 0 && offset.Y > 0 {
		target := offset
		v.pendingReadingOffset = &target
	}
	v.restoringReadingOffset = true
	v.scroll.ScrollToOffset(offset)
	v.restoringReadingOffset = false
}
func (v *markdownView) updateDiagramLayout(update func()) {
	if v.cropCapturing || v.crop != nil && v.crop.active {
		v.pendingDiagramUpdates = append(v.pendingDiagramUpdates, update)
		return
	}
	v.restoringReadingOffset = true
	update()
	v.relayout()
	v.scroll.Refresh()
	if v.pendingReadingOffset != nil {
		v.scroll.ScrollToOffset(*v.pendingReadingOffset)
	}
	v.restoringReadingOffset = false
	for _, diagram := range v.diagrams {
		if diagram.running {
			return
		}
	}
	v.pendingReadingOffset = nil
	v.scheduleThumbnail()
}

func (d *markdownDiagram) TappedSecondary(event *fyne.PointEvent) {
	if d.result.Image == nil || !d.image.Visible() || d.view.editor.closed || d.view.editor.markdownView != d.view {
		return
	}
	frame := d.result.Image
	menu := fyne.NewMenu("", fyne.NewMenuItem("Copy image", func() { d.view.copyDiagramImage(frame) }))
	widget.ShowPopUpMenuAtPosition(menu, d.view.editor.window.Canvas(), event.AbsolutePosition)
}
func (v *markdownView) copyDiagramImage(frame image.Image) {
	if frame == nil || v.diagramContext.Err() != nil {
		return
	}
	clipboard := desktopImageClipboard
	ctx, cancel := context.WithTimeout(v.diagramContext, 5*time.Second)
	go func() {
		defer cancel()
		var data bytes.Buffer
		err := png.Encode(&data, frame)
		if err == nil {
			err = ctx.Err()
		}
		if err == nil {
			err = clipboard.Write(ctx, data.Bytes())
		}
		if err != nil && ctx.Err() == nil {
			fyne.Do(func() {
				if !v.editor.closed && v.editor.markdownView == v {
					v.editor.showOperationError("clipboard_copy", err)
				}
			})
		}
	}()
}
