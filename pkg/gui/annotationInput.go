package gui

import (
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/document"
	"image"
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

type annotationInput struct {
	widget.BaseWidget
	layer            *signatureLayer
	active           bool
	data             annotations.Data
	frame            *image.NRGBA
	live             *canvas.Image
	pending          []*canvas.Image
	liveRect         *document.NormalizedRect
	pendingRects     map[*canvas.Image]document.NormalizedRect
	lastShapePreview time.Time
}

func newAnnotationInput(layer *signatureLayer) *annotationInput {
	p := &annotationInput{layer: layer}
	p.ExtendBaseWidget(p)
	p.Hide()
	return p
}
func (p *annotationInput) Cursor() desktop.Cursor {
	if p.layer.editor.effectiveAnnotationTool() == toolText {
		return desktop.TextCursor
	}
	return desktop.CrosshairCursor
}
func (p *annotationInput) SetVisible(v bool) {
	if v {
		p.Show()
	} else {
		p.Hide()
	}
}
func (p *annotationInput) point(position fyne.Position, clamp bool) (annotations.Point, bool) {
	origin, size := p.layer.displayRect()
	if size.Width <= 0 || size.Height <= 0 {
		return annotations.Point{}, false
	}
	x, y := float64(position.X-origin.X)/float64(size.Width), float64(position.Y-origin.Y)/float64(size.Height)
	inside := x >= 0 && x <= 1 && y >= 0 && y <= 1
	if !inside && !clamp {
		return annotations.Point{}, false
	}
	w, h := p.layer.editor.annotationPageSize(p.layer)
	if !inside && clamp && p.active && len(p.data.Points) > 0 {
		last := p.data.Points[len(p.data.Points)-1]
		lx, ly := last.X/w, last.Y/h
		dx, dy := x-lx, y-ly
		t := 1.
		if x < 0 {
			t = math.Min(t, -lx/dx)
		} else if x > 1 {
			t = math.Min(t, (1-lx)/dx)
		}
		if y < 0 {
			t = math.Min(t, -ly/dy)
		} else if y > 1 {
			t = math.Min(t, (1-ly)/dy)
		}
		x, y = lx+t*dx, ly+t*dy
	}
	return annotations.Point{X: math.Min(math.Max(x, 0), 1) * w, Y: math.Min(math.Max(y, 0), 1) * h}, inside
}
func (p *annotationInput) Tapped(event *fyne.PointEvent) {
	g := p.layer.editor
	if g.effectiveAnnotationTool() != toolText || g.document == nil || g.working && g.annotationPending == 0 {
		return
	}
	point, ok := p.point(event.Position, false)
	if !ok {
		return
	}
	g.beginTextPlacement(p.layer, point)
}
func (p *annotationInput) MouseDown(event *desktop.MouseEvent) {
	g := p.layer.editor
	tool := g.effectiveAnnotationTool()
	if event.Button != desktop.MouseButtonPrimary || g.document == nil || tool == toolText || tool == toolSelect || tool == toolHighlight || g.working && g.annotationPending == 0 {
		return
	}
	point, ok := p.point(event.Position, false)
	if !ok {
		return
	}
	if p.active {
		p.finish()
	}
	g.ensureAnnotationTools()
	g.activatePDFPage(p.layer.page)
	p.data = g.annotationDefaults[g.annotationTool.kind()].Clone()
	p.data.PageWidth, p.data.PageHeight = g.annotationPageSize(p.layer)
	p.data.Points = []annotations.Point{point}
	if p.data.Kind == annotations.Shape {
		p.data.Points = append(p.data.Points, point)
	}
	_, size := p.layer.displayRect()
	scale := math.Min(2, 2048/float64(math.Max(float64(size.Width), float64(size.Height))))
	p.frame = image.NewNRGBA(image.Rect(0, 0, max(1, int(float64(size.Width)*scale)), max(1, int(float64(size.Height)*scale))))
	p.live = canvas.NewImageFromImage(p.frame)
	// Let the GPU scale the preview instead of resampling pixels on the UI thread.
	p.live.ScaleMode = canvas.ImageScaleFastest
	p.live.FillMode = canvas.ImageFillStretch
	p.active = true
	p.liveRect = nil
	if p.data.Kind != annotations.Shape {
		p.paint(point, point)
	}
	p.Refresh()
}
func (p *annotationInput) append(position fyne.Position) {
	if !p.active {
		return
	}
	point, inside := p.point(position, true)
	if p.data.Kind == annotations.Shape {
		a := p.data.Points[0]
		if p.data.Shape == annotations.Magnifier || p.data.Shape == annotations.Oval && constrainShapeProportions(p.layer.editor.modifiers()) {
			delta := math.Min(math.Abs(point.X-a.X), math.Abs(point.Y-a.Y))
			point.X = a.X + math.Copysign(delta, point.X-a.X)
			point.Y = a.Y + math.Copysign(delta, point.Y-a.Y)
		}
		p.data.Points[1] = point
		if time.Since(p.lastShapePreview) > 16*time.Millisecond {
			p.previewShape()
			p.lastShapePreview = time.Now()
		}
		return
	}
	last := p.data.Points[len(p.data.Points)-1]
	if math.Hypot(point.X-last.X, point.Y-last.Y) >= .15 {
		p.data.Points = append(p.data.Points, point)
		p.paint(last, point)
		p.live.Refresh()
	}
	if !inside {
		p.finish()
	}
}
func (p *annotationInput) MouseUp(event *desktop.MouseEvent) {
	if event.Button == desktop.MouseButtonPrimary {
		p.append(event.Position)
		p.finish()
	}
}
func (p *annotationInput) MouseIn(event *desktop.MouseEvent) { p.MouseMoved(event) }
func (p *annotationInput) MouseMoved(event *desktop.MouseEvent) {
	if !p.active {
		p.layer.revealRedactions(event.Position)
	} else {
		p.layer.hideRedactionReveals()
	}
	p.append(event.Position)
}

// Dragged delivers out-of-bounds coordinates even when another page is hovered.
// It clips the final segment to this page before committing the stroke.
func (p *annotationInput) MouseOut()                     { p.layer.hideRedactionReveals() }
func (p *annotationInput) Dragged(event *fyne.DragEvent) { p.append(event.Position) }
func (p *annotationInput) DragEnd()                      { p.finish() }
func (p *annotationInput) cancel() bool {
	if !p.active {
		return false
	}
	p.active = false
	p.live = nil
	p.frame = nil
	p.Refresh()
	return true
}
func (p *annotationInput) finish() {
	if !p.active {
		return
	}
	if p.data.Kind == annotations.Shape {
		a, b := p.data.Points[0], p.data.Points[1]
		if math.Hypot(a.X-b.X, a.Y-b.Y) < 3 {
			p.cancel()
			return
		}
		p.previewShape()
	}
	p.active = false
	live := p.live
	p.pending = append(p.pending, live)
	if p.liveRect != nil {
		if p.pendingRects == nil {
			p.pendingRects = map[*canvas.Image]document.NormalizedRect{}
		}
		p.pendingRects[live] = *p.liveRect
	}
	p.liveRect = nil
	p.live = nil
	p.frame = nil
	p.layer.editor.addPageAnnotation(p.layer, p.data, 0, 0, func() {
		for i, v := range p.pending {
			if v == live {
				p.pending = append(p.pending[:i], p.pending[i+1:]...)
				delete(p.pendingRects, live)
				break
			}
		}
		p.Refresh()
	})
	p.Refresh()
	p.layer.editor.updateTemporarySelection()
}

// Paint an antialiased capsule into a mask, using maximum alpha rather than
// repeated blending. Each stroke has uniform opacity, including intersections.
func (p *annotationInput) paint(a, b annotations.Point) {
	frame := p.frame
	sx := float64(frame.Bounds().Dx()) / p.data.PageWidth
	sy := float64(frame.Bounds().Dy()) / p.data.PageHeight
	ax, ay, bx, by := a.X*sx, a.Y*sy, b.X*sx, b.Y*sy
	radius := p.data.Width * sx / 2
	minX := max(0, int(math.Floor(math.Min(ax, bx)-radius-1)))
	maxX := min(frame.Bounds().Dx()-1, int(math.Ceil(math.Max(ax, bx)+radius+1)))
	minY := max(0, int(math.Floor(math.Min(ay, by)-radius-1)))
	maxY := min(frame.Bounds().Dy()-1, int(math.Ceil(math.Max(ay, by)+radius+1)))
	dx, dy := bx-ax, by-ay
	length := dx*dx + dy*dy
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px, py := float64(x)+.5, float64(y)+.5
			t := 0.
			if length > 0 {
				t = math.Min(math.Max(((px-ax)*dx+(py-ay)*dy)/length, 0), 1)
			}
			distance := math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
			alpha := uint8(math.Min(math.Max(radius+.5-distance, 0), 1) * p.data.Opacity * 255)
			if alpha > frame.NRGBAAt(x, y).A {
				c := p.data.Color
				c.A = alpha
				frame.SetNRGBA(x, y, c)
			}
		}
	}
}
func (p *annotationInput) CreateRenderer() fyne.WidgetRenderer {
	return &annotationInputRenderer{pad: p, background: canvas.NewRectangle(color.Transparent)}
}

type annotationInputRenderer struct {
	pad        *annotationInput
	background *canvas.Rectangle
}

func (r *annotationInputRenderer) Destroy()           {}
func (r *annotationInputRenderer) MinSize() fyne.Size { return fyne.NewSize(0, 0) }
func (r *annotationInputRenderer) Objects() []fyne.CanvasObject {
	objects := []fyne.CanvasObject{r.background}
	for _, v := range r.pad.pending {
		objects = append(objects, v)
	}
	if r.pad.live != nil {
		objects = append(objects, r.pad.live)
	}
	return objects
}
func (r *annotationInputRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	origin, display := r.pad.layer.displayRect()
	for _, obj := range r.Objects()[1:] {
		v := obj.(*canvas.Image)
		pos, sz := origin, display
		if rect, ok := r.pad.pendingRects[v]; ok {
			pos, sz = r.pad.layer.widgetRect(rect)
		} else if v == r.pad.live && r.pad.liveRect != nil {
			pos, sz = r.pad.layer.widgetRect(*r.pad.liveRect)
		}
		v.Move(pos)
		v.Resize(sz)
	}
}
func (r *annotationInputRenderer) Refresh() { r.Layout(r.pad.Size()); canvas.Refresh(r.pad) }

func (p *annotationInput) previewShape() {
	a, b, e := p.data.Artwork()
	if e != nil {
		return
	}
	picture, e := a.Render(512)
	if e != nil {
		return
	}
	w, h := p.layer.editor.annotationPageSize(p.layer)
	rect := document.NormalizedRect{X: b.X0 / w, Y: b.Y0 / h, Width: b.W() / w, Height: b.H() / h}
	// Keep the pending artwork aligned with the eventual page-clamped overlay.
	rect.X = clampFloat64(rect.X, 0, 1-rect.Width)
	rect.Y = clampFloat64(rect.Y, 0, 1-rect.Height)
	p.liveRect = &rect
	p.live.Image = picture
	p.live.Refresh()
	p.Refresh()
}
