package gui

import (
	"emanuelelongo/proview/pkg/signatures"
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

type drawingRequest struct {
	drawing    signatures.Drawing
	generation uint64
	revision   uint64
}

type signaturePad struct {
	widget.BaseWidget
	drawing                      signatures.Drawing
	picture                      *canvas.Image
	active, editable, closed     bool
	generation                   uint64
	revision, renderedGeneration uint64
	timer                        *time.Timer
	requests                     chan drawingRequest
	stop                         chan struct{}
}

func newSignaturePad(drawing signatures.Drawing, editable bool) *signaturePad {
	p := &signaturePad{drawing: drawing.Clone(), editable: editable, picture: canvas.NewImageFromImage(nil), requests: make(chan drawingRequest, 1), stop: make(chan struct{})}
	p.picture.FillMode = canvas.ImageFillStretch
	p.ExtendBaseWidget(p)
	go p.renderWorker()
	p.schedule()
	return p
}

func (p *signaturePad) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(color.White)
	background.StrokeColor = color.NRGBA{R: 170, G: 170, B: 170, A: 255}
	background.StrokeWidth = 1
	return &signaturePadRenderer{pad: p, background: background}
}

type signaturePadRenderer struct {
	pad        *signaturePad
	background *canvas.Rectangle
}

func (r *signaturePadRenderer) Destroy() {}
func (r *signaturePadRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	r.pad.picture.Resize(size)
}
func (r *signaturePadRenderer) MinSize() fyne.Size { return fyne.NewSize(600, 270) }
func (r *signaturePadRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.background, r.pad.picture}
}
func (r *signaturePadRenderer) Refresh() { r.background.Refresh(); r.pad.picture.Refresh() }

func (p *signaturePad) logicalPoint(position fyne.Position) (signatures.Point, bool) {
	size := p.Size()
	if size.Width <= 0 || size.Height <= 0 || position.X < 0 || position.Y < 0 || position.X > size.Width || position.Y > size.Height {
		return signatures.Point{}, false
	}
	return signatures.Point{X: float64(position.X) / float64(size.Width) * 1000, Y: float64(position.Y) / float64(size.Height) * 450}, true
}

func (p *signaturePad) MouseDown(event *desktop.MouseEvent) {
	if !p.editable || p.closed || event.Button != desktop.MouseButtonPrimary {
		return
	}
	point, ok := p.logicalPoint(event.Position)
	if !ok {
		return
	}
	p.drawing.Strokes = append(p.drawing.Strokes, []signatures.Point{point})
	p.active = true
	p.schedule()
}

func (p *signaturePad) appendPoint(position fyne.Position) {
	if !p.active || p.closed {
		return
	}
	point, ok := p.logicalPoint(position)
	if !ok {
		p.active = false
		return
	}
	stroke := p.drawing.Strokes[len(p.drawing.Strokes)-1]
	last := stroke[len(stroke)-1]
	if math.Hypot(point.X-last.X, point.Y-last.Y) < 0.5 {
		return
	}
	p.drawing.Strokes[len(p.drawing.Strokes)-1] = append(stroke, point)
	p.schedule()
}
func (p *signaturePad) MouseUp(event *desktop.MouseEvent) {
	if event.Button == desktop.MouseButtonPrimary {
		p.appendPoint(event.Position)
		p.active = false
	}
}
func (p *signaturePad) MouseIn(*desktop.MouseEvent)          {}
func (p *signaturePad) MouseMoved(event *desktop.MouseEvent) { p.appendPoint(event.Position) }
func (p *signaturePad) MouseOut()                            { p.active = false }
func (p *signaturePad) Dragged(event *fyne.DragEvent)        { p.appendPoint(event.Position) }
func (p *signaturePad) DragEnd()                             { p.active = false }

// Only one renderer runs per pad; pending snapshots are replaced by the latest.
func (p *signaturePad) schedule() {
	if p.closed {
		return
	}
	p.generation++
	if p.timer != nil {
		return
	}
	p.timer = time.AfterFunc(50*time.Millisecond, func() {
		fyne.Do(func() {
			p.timer = nil
			if p.closed {
				return
			}
			request := drawingRequest{drawing: p.drawing.Clone(), generation: p.generation, revision: p.revision}
			select {
			case <-p.requests:
			default:
			}
			select {
			case p.requests <- request:
			default:
			}
		})
	})
}

func (p *signaturePad) renderWorker() {
	for {
		select {
		case <-p.stop:
			return
		case request := <-p.requests:
			picture, err := signatures.RenderDrawing(request.drawing, 1000)
			fyne.Do(func() {
				p.applyPreview(request, picture, err)
			})
		}
	}
}

// Mouse input may advance while a snapshot renders. Display those frames rather
// than waiting for input to stop, but reject frames from before a settings edit,
// undo or clear, and never replace a newer frame with an older one.
func (p *signaturePad) applyPreview(request drawingRequest, picture image.Image, err error) {
	if p.closed || request.revision != p.revision || request.generation <= p.renderedGeneration {
		return
	}
	p.renderedGeneration = request.generation
	if err != nil {
		p.picture.Image = nil
	} else {
		p.picture.Image = picture
	}
	p.picture.Refresh()
}

func (p *signaturePad) invalidatePreview() {
	p.revision++
	p.schedule()
}

func (p *signaturePad) close() {
	if p.closed {
		return
	}
	p.closed = true
	if p.timer != nil {
		p.timer.Stop()
	}
	close(p.stop)
}

func (g *Gui) editDrawing(drawing signatures.Drawing, editable bool, onSave func(signatures.Asset, func())) {
	pad := newSignaturePad(drawing, editable)
	smooth := widget.NewSlider(0, 100)
	smooth.Step = 1
	smooth.SetValue(drawing.Smooth)
	smoothValue := widget.NewLabel(fmt.Sprintf("%.0f", drawing.Smooth))
	smooth.OnChanged = func(value float64) {
		pad.drawing.Smooth = value
		smoothValue.SetText(fmt.Sprintf("%.0f", value))
		pad.invalidatePreview()
	}
	width := widget.NewSlider(1, 20)
	width.Step = 0.5
	width.SetValue(drawing.Width)
	widthValue := widget.NewLabel(fmt.Sprintf("%.1f", drawing.Width))
	width.OnChanged = func(value float64) {
		pad.drawing.Width = value
		widthValue.SetText(fmt.Sprintf("%.1f", value))
		pad.invalidatePreview()
	}
	controls := container.NewVBox(
		widget.NewLabel("Smoothness"), container.NewBorder(nil, nil, nil, smoothValue, smooth),
		widget.NewLabel("Width"), container.NewBorder(nil, nil, nil, widthValue, width),
	)
	if editable {
		controls.Add(container.NewHBox(
			widget.NewButton("Undo last stroke", func() {
				if len(pad.drawing.Strokes) > 0 {
					pad.active = false
					pad.drawing.Strokes = pad.drawing.Strokes[:len(pad.drawing.Strokes)-1]
					pad.invalidatePreview()
				}
			}),
			widget.NewButton("Clear", func() { pad.active = false; pad.drawing.Strokes = nil; pad.invalidatePreview() }),
		))
	}
	var editor *dialog.CustomDialog
	var save *widget.Button
	save = widget.NewButton("Save", func() {
		save.Disable()
		snapshot := pad.drawing.Clone()
		go func() {
			asset, err := signatures.FromDrawing(snapshot)
			fyne.Do(func() {
				if pad.closed {
					return
				}
				save.Enable()
				if err != nil {
					g.showOperationError("signature_drawing", err)
					return
				}
				onSave(asset, editor.Hide)
			})
		}()
	})
	content := container.NewScroll(container.NewVBox(container.NewCenter(pad), controls))
	content.SetMinSize(g.signatureViewport())
	editor = dialog.NewCustomWithoutButtons("Drawn signature", content, g.window)
	editor.SetButtons([]fyne.CanvasObject{save, widget.NewButton("Cancel", editor.Hide)})
	g.showSignatureDialog(editor, pad.close)
}
