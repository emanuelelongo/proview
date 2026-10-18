package gui

import (
	"context"
	"image"
	"image/color"
	"math"
	"time"

	"emanuelelongo/proview/internal/ocr"
	"emanuelelongo/proview/internal/textselect"
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/vector"
)

type highlightInput struct {
	widget.BaseWidget
	layer                     *signatureLayer
	geometry                  *textselect.Layout
	key                       string
	data                      annotations.Data
	start, end                int
	press, last               fyne.Position
	active, pending, released bool
	serial                    uint64
	cancelLoad                context.CancelFunc
	draft                     []annotations.Quad
	preview                   *canvas.Raster
	backdrop                  image.Image
}

func newHighlightInput(layer *signatureLayer) *highlightInput {
	p := &highlightInput{layer: layer}
	p.ExtendBaseWidget(p)
	p.Hide()
	return p
}
func (p *highlightInput) Cursor() desktop.Cursor { return desktop.TextCursor }
func (p *highlightInput) MouseDown(event *desktop.MouseEvent) {
	g := p.layer.editor
	if event.Button != desktop.MouseButtonPrimary || g.document == nil || g.effectiveAnnotationTool() != toolHighlight || g.working && g.annotationPending == 0 {
		return
	}
	point := p.layer.textPoint(event.Position)
	if point.X < 0 || point.Y < 0 || point.X > 1 || point.Y > 1 {
		return
	}
	p.cancel()
	g.activatePDFPage(p.layer.page)
	g.clearSignatureSelection()
	g.clearTextSelection()
	g.ensureAnnotationTools()
	p.data = g.annotationDefaults[annotations.Highlight].Clone()
	p.data.Opacity = 1
	p.data.PageWidth, p.data.PageHeight = g.annotationPageSize(p.layer)
	if g.image != nil {
		p.backdrop = g.image.Image
	}
	if g.document.Format() == document.FormatPDF && p.layer.page < len(g.pdfPageImages) && g.pdfPageImages[p.layer.page] != nil {
		p.backdrop = g.pdfPageImages[p.layer.page].Image
	}
	p.data.Points = nil
	p.data.Quads = nil
	p.press, p.last, p.released = event.Position, event.Position, false
	key := g.document.TextSelectionKey(p.layer.page)
	if p.geometry != nil && p.key == key {
		p.begin()
		return
	}
	if cached := p.layer.selectableLayout(); cached != nil {
		copy := *cached
		p.geometry = &copy
		p.key = key
		p.begin()
		return
	}
	p.pending = true
	doc, page, serial := g.document, p.layer.page, p.serial
	snapshot := doc.RenderingSnapshot()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	p.cancelLoad = cancel
	var engine ocr.Engine
	if g.ocr != nil {
		engine = g.ocr.engine
	}
	// Cancellation may not interrupt native PDF extraction immediately. Bound
	// actual recognition work across all pages in this editor regardless.
	if g.highlightWork == nil {
		g.highlightWork = make(chan struct{}, 1)
	}
	work := g.highlightWork
	go func() {
		var geometry textselect.Layout
		var err error
		select {
		case work <- struct{}{}:
			if err = ctx.Err(); err == nil {
				geometry, err = snapshot.SelectableText(ctx, page, engine)
			}
			<-work
		case <-ctx.Done():
			err = ctx.Err()
		}
		cancel()
		fyne.Do(func() {
			if p.serial != serial {
				return
			}
			p.cancelLoad = nil
			if g.closed || g.document != doc || p.layer.document != doc || doc.TextSelectionKey(page) != key || g.effectiveAnnotationTool() != toolHighlight {
				p.cancel()
				return
			}
			p.pending = false
			if err != nil {
				g.logEvent("highlight_text_failed", "level=WARN page=%d error=%q", page+1, err)
				p.cancel()
				return
			}
			p.geometry = &geometry
			p.key = key
			p.begin()
			p.update(p.last)
			if p.released {
				p.finish()
			}
		})
	}()
}
func (p *highlightInput) begin() {
	if p.geometry == nil {
		return
	}
	p.start = p.geometry.WordAt(p.layer.textPoint(p.press), false)
	if p.start < 0 {
		return
	}
	p.active = true
	p.end = -1
	p.update(p.last)
}
func (p *highlightInput) update(position fyne.Position) {
	p.last = position
	if !p.active || p.geometry == nil {
		return
	}
	end := p.geometry.WordAt(p.layer.textPoint(position), true)
	if end < 0 || end == p.end {
		return
	}
	p.end = end
	p.draft = highlightQuads(*p.geometry, p.start, p.end, p.data.PageWidth, p.data.PageHeight)
	// Capture immutable geometry; Fyne may rasterize from another goroutine.
	quads := append([]annotations.Quad(nil), p.draft...)
	data := p.data
	backdrop := p.backdrop
	generator := func(w, h int) image.Image {
		frame := image.NewNRGBA(image.Rect(0, 0, w, h))
		r := vector.NewRasterizer(w, h)
		for _, q := range quads {
			for i, p := range q {
				x, y := float32(p.X/data.PageWidth*float64(w)), float32(p.Y/data.PageHeight*float64(h))
				if i == 0 {
					r.MoveTo(x, y)
				} else {
					r.LineTo(x, y)
				}
			}
			r.ClosePath()
		}
		c := data.Color
		c.A = 255
		r.Draw(frame, frame.Bounds(), image.NewUniform(c), image.Point{})
		if backdrop != nil {
			base := image.NewNRGBA(frame.Bounds())
			xdraw.BiLinear.Scale(base, base.Bounds(), backdrop, backdrop.Bounds(), xdraw.Src, nil)
			return document.MultiplyHighlight(base, frame, frame.Bounds())
		}
		// Geometry can arrive before the page image; keep the gesture visible
		// without covering text while the page loads.
		for i := 3; i < len(frame.Pix); i += 4 {
			frame.Pix[i] = uint8(uint16(frame.Pix[i]) * 77 / 255)
		}
		return frame
	}
	if p.preview == nil {
		p.preview = canvas.NewRaster(generator)
	} else {
		p.preview.Generator = generator
	}
	p.preview.Refresh()
	p.Refresh()
}
func (p *highlightInput) MouseUp(e *desktop.MouseEvent) {
	if e.Button == desktop.MouseButtonPrimary {
		p.released = true
		p.update(e.Position)
		p.finish()
	}
}
func (p *highlightInput) MouseIn(e *desktop.MouseEvent)    { p.MouseMoved(e) }
func (p *highlightInput) MouseMoved(e *desktop.MouseEvent) { p.update(e.Position) }
func (p *highlightInput) MouseOut()                        {}
func (p *highlightInput) Dragged(e *fyne.DragEvent)        { p.update(e.Position) }
func (p *highlightInput) DragEnd()                         { p.released = true; p.finish() }
func (p *highlightInput) Tapped(*fyne.PointEvent)          {}
func (p *highlightInput) cancel() bool {
	was := p.active || p.pending
	p.serial++
	if p.cancelLoad != nil {
		p.cancelLoad()
		p.cancelLoad = nil
	}
	p.active, p.pending = false, false
	p.draft = nil
	p.preview = nil
	p.backdrop = nil
	p.Refresh()
	return was
}
func (p *highlightInput) finish() {
	if !p.active {
		return
	}
	data := p.data.Clone()
	data.Quads = append([]annotations.Quad(nil), p.draft...)
	p.cancel()
	if len(data.Quads) == 0 {
		return
	}
	p.layer.editor.addPageAnnotation(p.layer, data, 0, 0, nil)
	p.layer.editor.updateTemporarySelection()
}
func (p *highlightInput) CreateRenderer() fyne.WidgetRenderer {
	return &highlightInputRenderer{pad: p, background: canvas.NewRectangle(color.Transparent)}
}

type highlightInputRenderer struct {
	pad        *highlightInput
	background *canvas.Rectangle
}

func (r *highlightInputRenderer) MinSize() fyne.Size { return fyne.Size{} }
func (r *highlightInputRenderer) Destroy()           {}
func (r *highlightInputRenderer) Objects() []fyne.CanvasObject {
	if r.pad.preview != nil {
		return []fyne.CanvasObject{r.background, r.pad.preview}
	}
	return []fyne.CanvasObject{r.background}
}
func (r *highlightInputRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	if r.pad.preview != nil {
		origin, size := r.pad.layer.displayRect()
		r.pad.preview.Move(origin)
		r.pad.preview.Resize(size)
	}
}
func (r *highlightInputRenderer) Refresh() { r.Layout(r.pad.Size()); canvas.Refresh(r.pad) }

// Merge words along each line's own axes, including gaps and rotated text.
func highlightQuads(layout textselect.Layout, start, end int, width, height float64) []annotations.Quad {
	words := layout.Range(start, end)
	var out []annotations.Quad
	for i := 0; i < len(words); {
		first := words[i]
		j := i + 1
		for j < len(words) && words[j].Block == first.Block && words[j].Line == first.Line {
			j++
		}
		a, b := first.Quad[0], first.Quad[1]
		ux, uy := (b.X-a.X)*width, (b.Y-a.Y)*height
		length := math.Hypot(ux, uy)
		if length <= 0 {
			i = j
			continue
		}
		ux /= length
		uy /= length
		vx, vy := -uy, ux
		lo, hi, top, bottom := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
		for _, word := range words[i:j] {
			for _, p := range word.Quad {
				x, y := p.X*width, p.Y*height
				u, v := x*ux+y*uy, x*vx+y*vy
				lo = math.Min(lo, u)
				hi = math.Max(hi, u)
				top = math.Min(top, v)
				bottom = math.Max(bottom, v)
			}
		}
		point := func(u, v float64) annotations.Point {
			return annotations.Point{X: clampFloat64(u*ux+v*vx, 0, width), Y: clampFloat64(u*uy+v*vy, 0, height)}
		}
		out = append(out, annotations.Quad{point(lo, top), point(hi, top), point(hi, bottom), point(lo, bottom)})
		i = j
	}
	return out
}
