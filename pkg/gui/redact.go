package gui

import (
	"image"
	"image/color"
	"math"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

func quadPresentation(q document.TextQuad, size image.Point) (image.Image, document.NormalizedRect) {
	x0, y0, x1, y1 := q.Bounds()
	rect := document.NormalizedRect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
	local := q
	for _, p := range []*document.TextPoint{&local.UL, &local.UR, &local.LL, &local.LR} {
		p.X = (p.X - x0) / (x1 - x0)
		p.Y = (p.Y - y0) / (y1 - y0)
	}
	return document.PaintRedactions(image.Pt(max(1, int(rect.Width*float64(size.X))), max(1, int(rect.Height*float64(size.Y)))), []document.TextQuad{local}), rect
}

type redactionWidget struct {
	widget.BaseWidget
	layer   *signatureLayer
	id      document.RedactionID
	rect    document.NormalizedRect
	preview *canvas.Image
	border  *canvas.Rectangle
}

func newRedactionWidget(layer *signatureLayer, id document.RedactionID, q document.TextQuad) *redactionWidget {
	img, rect := quadPresentation(q, layer.pageSize)
	w := &redactionWidget{layer: layer, id: id, rect: rect, preview: canvas.NewImageFromImage(img), border: canvas.NewRectangle(color.Transparent)}
	w.preview.FillMode = canvas.ImageFillStretch
	w.border.StrokeColor = color.NRGBA{R: 0, G: 122, B: 255, A: 255}
	w.ExtendBaseWidget(w)
	return w
}
func (w *redactionWidget) Tapped(*fyne.PointEvent) {
	if w.layer.editor != nil {
		w.layer.editor.selectRedaction(w.layer.page, w.id)
	}
}

// Reveal is presentation-only: source bytes, marks and exports stay unchanged.
func (w *redactionWidget) MouseIn(event *desktop.MouseEvent) { w.MouseMoved(event) }
func (w *redactionWidget) MouseMoved(event *desktop.MouseEvent) {
	w.layer.revealRedactions(event.Position.Add(w.Position()))
}
func (w *redactionWidget) MouseOut() { w.layer.hideRedactionReveals() }
func (w *redactionWidget) setRevealed(revealed bool) {
	opacity := 0.
	if revealed {
		opacity = .85
	}
	if w.preview.Translucency != opacity {
		w.preview.Translucency = opacity
		w.preview.Refresh()
	}
}
func (l *signatureLayer) hideRedactionReveals() {
	for _, mark := range l.redactionWidgets {
		mark.setRevealed(false)
	}
}
func (l *signatureLayer) revealRedactions(position fyne.Position) {
	origin, size := l.displayRect()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	x, y := float64(position.X-origin.X)/float64(size.Width), float64(position.Y-origin.Y)/float64(size.Height)
	for _, mark := range l.redactionWidgets {
		r := mark.rect
		mark.setRevealed(x >= r.X && x <= r.X+r.Width && y >= r.Y && y <= r.Y+r.Height)
	}
}
func (w *redactionWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(w.preview, w.border))
}
func (w *redactionWidget) setSelected(selected bool) {
	w.border.StrokeWidth = 0
	if selected {
		w.border.StrokeWidth = 2
	}
	w.border.Refresh()
}

func (g *Gui) clearRedactionSelection() {
	g.selectedRedaction = 0
	for _, layer := range g.signatureLayers {
		for _, mark := range layer.redactionWidgets {
			mark.setSelected(false)
		}
	}
}
func (g *Gui) selectRedaction(page int, id document.RedactionID) {
	if g.working {
		return
	}
	g.activatePDFPage(page)
	g.clearSignatureSelection()
	g.clearRedactionSelection()
	g.selectedRedaction = id
	g.selectedRedactionPage = page
	for _, layer := range g.signatureLayers {
		for _, mark := range layer.redactionWidgets {
			mark.setSelected(mark.id == id)
		}
	}
	g.refreshAnnotationControls()
}
func (g *Gui) deleteSelectedRedaction() {
	if g.document == nil || g.selectedRedaction == 0 || g.working {
		return
	}
	page := g.selectedRedactionPage
	if err := g.document.RemoveRedaction(g.selectedRedaction); err != nil {
		g.showOperationError("pdf_redact", err)
		return
	}
	g.clearRedactionSelection()
	if layer := g.signatureLayers[page]; layer != nil {
		layer.reload()
	}
	g.refreshAnnotationControls()
	g.documentChanged(page)
}

type redactionInput struct {
	widget.BaseWidget
	layer                     *signatureLayer
	geometry                  *document.TextLayout
	loading                   bool
	pending, released, active bool
	press, last               fyne.Position
	start, end                int
	rectangular               bool
	anchor                    document.TextPoint
	draft                     []document.TextQuad
	draftImages               []*canvas.Image
	draftRects                []document.NormalizedRect
}

func newRedactionInput(layer *signatureLayer) *redactionInput {
	p := &redactionInput{layer: layer}
	p.ExtendBaseWidget(p)
	p.Hide()
	return p
}
func (p *redactionInput) Cursor() desktop.Cursor {
	if p.rectangular || p.geometry != nil && len(p.geometry.Glyphs) == 0 {
		return desktop.CrosshairCursor
	}
	return desktop.TextCursor
}
func (p *redactionInput) point(position fyne.Position) document.TextPoint {
	origin, size := p.layer.displayRect()
	if size.Width <= 0 || size.Height <= 0 {
		return document.TextPoint{}
	}
	return document.TextPoint{X: math.Max(0, math.Min(1, float64(position.X-origin.X)/float64(size.Width))), Y: math.Max(0, math.Min(1, float64(position.Y-origin.Y)/float64(size.Height)))}
}
func (p *redactionInput) load() {
	if p.geometry != nil || p.loading || p.layer.document == nil {
		return
	}
	p.loading = true
	g := p.layer.editor
	doc := p.layer.document
	page := p.layer.page
	snapshot := doc.RenderingSnapshot()
	pageID := doc.PageIDs()[page]
	g.queueAnnotation(func() func() {
		geometry, err := snapshot.TextGeometry(page)
		return func() {
			p.loading = false
			if g.document != doc || p.layer.document != doc || p.layer.page != page || page >= doc.PageCount() || doc.PageIDs()[page] != pageID {
				return
			}
			if err != nil {
				p.pending = false
				g.showOperationError("pdf_redact", err)
				return
			}
			p.geometry = &geometry
			if p.pending && g.annotationTool == toolRedact {
				p.pending = false
				last := p.last
				p.begin(p.press)
				p.update(last)
				if p.released {
					p.finish()
				}
			}
		}
	})
}
func (p *redactionInput) begin(position fyne.Position) {
	if p.geometry == nil {
		return
	}
	p.anchor = p.point(position)
	start := p.geometry.GlyphAt(p.anchor, false)
	p.rectangular = start < 0
	p.start, p.end, p.active = start, -1, true
	p.update(position)
}
func (p *redactionInput) MouseDown(event *desktop.MouseEvent) {
	g := p.layer.editor
	if event.Button != desktop.MouseButtonPrimary || g.annotationTool != toolRedact || g.document == nil || (g.working && g.annotationPending == 0) {
		return
	}
	origin, size := p.layer.displayRect()
	if event.Position.X < origin.X || event.Position.Y < origin.Y || event.Position.X > origin.X+size.Width || event.Position.Y > origin.Y+size.Height {
		return
	}
	p.cancel()
	p.layer.hideRedactionReveals()
	g.activatePDFPage(p.layer.page)
	g.clearSignatureSelection()
	g.clearRedactionSelection()
	p.press, p.last, p.released = event.Position, event.Position, false
	if p.geometry == nil {
		p.pending = true
		p.load()
		return
	}
	p.begin(event.Position)
}
func (p *redactionInput) update(position fyne.Position) {
	p.last = position
	if !p.active || p.geometry == nil {
		return
	}
	if p.rectangular {
		point := p.point(position)
		x0, y0 := math.Min(p.anchor.X, point.X), math.Min(p.anchor.Y, point.Y)
		x1, y1 := math.Max(p.anchor.X, point.X), math.Max(p.anchor.Y, point.Y)
		_, size := p.layer.displayRect()
		p.draft = nil
		// A click or a nearly flat drag must not create an accidental mark.
		if (x1-x0)*float64(size.Width) >= 3 && (y1-y0)*float64(size.Height) >= 3 {
			p.draft = []document.TextQuad{{UL: document.TextPoint{X: x0, Y: y0}, UR: document.TextPoint{X: x1, Y: y0}, LL: document.TextPoint{X: x0, Y: y1}, LR: document.TextPoint{X: x1, Y: y1}}}
		}
	} else {
		end := p.geometry.GlyphAt(p.point(position), true)
		if end == p.end {
			return
		}
		p.end = end
		p.draft = p.geometry.Selection(p.start, end)
	}
	p.draftImages = nil
	p.draftRects = nil
	for _, q := range p.draft {
		img, rect := quadPresentation(q, p.layer.pageSize)
		preview := canvas.NewImageFromImage(img)
		preview.FillMode = canvas.ImageFillStretch
		p.draftImages = append(p.draftImages, preview)
		p.draftRects = append(p.draftRects, rect)
	}
	p.Refresh()
}
func (p *redactionInput) MouseUp(event *desktop.MouseEvent) {
	if event.Button == desktop.MouseButtonPrimary {
		p.released = true
		p.update(event.Position)
		p.finish()
	}
}
func (p *redactionInput) MouseIn(event *desktop.MouseEvent) { p.MouseMoved(event) }
func (p *redactionInput) MouseOut()                         { p.layer.hideRedactionReveals() }
func (p *redactionInput) MouseMoved(event *desktop.MouseEvent) {
	if !p.active {
		p.layer.revealRedactions(event.Position)
	}
	p.update(event.Position)
}
func (p *redactionInput) Dragged(event *fyne.DragEvent) { p.update(event.Position) }
func (p *redactionInput) DragEnd()                      { p.released = true; p.finish() }
func (p *redactionInput) Tapped(*fyne.PointEvent)       {}
func (p *redactionInput) cancel() bool {
	active := p.active || p.pending
	p.active = false
	p.rectangular = false
	p.pending = false
	p.draft = nil
	p.draftImages = nil
	p.draftRects = nil
	p.Refresh()
	return active
}
func (p *redactionInput) finish() {
	if !p.active {
		return
	}
	quads := append([]document.TextQuad(nil), p.draft...)
	p.cancel()
	if len(quads) == 0 {
		return
	}
	if _, err := p.layer.document.AddRedaction(p.layer.page, quads); err != nil {
		p.layer.editor.showOperationError("pdf_redact", err)
		return
	}
	p.layer.reload()
	p.layer.editor.documentChanged(p.layer.page)
}
func (p *redactionInput) CreateRenderer() fyne.WidgetRenderer {
	return &redactionInputRenderer{pad: p, background: canvas.NewRectangle(color.Transparent)}
}

type redactionInputRenderer struct {
	pad        *redactionInput
	background *canvas.Rectangle
}

func (r *redactionInputRenderer) Destroy()           {}
func (r *redactionInputRenderer) MinSize() fyne.Size { return fyne.NewSize(0, 0) }
func (r *redactionInputRenderer) Objects() []fyne.CanvasObject {
	objects := []fyne.CanvasObject{r.background}
	for _, img := range r.pad.draftImages {
		objects = append(objects, img)
	}
	return objects
}
func (r *redactionInputRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	for i, img := range r.pad.draftImages {
		position, display := r.pad.layer.widgetRect(r.pad.draftRects[i])
		img.Move(position)
		img.Resize(display)
	}
}
func (r *redactionInputRenderer) Refresh() { r.Layout(r.pad.Size()); canvas.Refresh(r.pad) }
