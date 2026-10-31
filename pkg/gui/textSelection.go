package gui

import (
	"image"
	"image/color"
	"math"

	"emanuelelongo/proview/internal/textselect"
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"golang.org/x/image/vector"
)

type textSelection struct {
	doc        *document.Document
	key        string
	layer      *signatureLayer
	layout     textselect.Layout
	start, end int
	active     bool
	qrCode     bool
}

func (g *Gui) validTextSelection() bool {
	s := g.textSelection
	return s != nil && g.document == s.doc && s.key == g.document.TextSelectionKey(s.layer.page) && s.layer.document == g.document
}
func (g *Gui) clearTextSelection() {
	if s := g.textSelection; s != nil {
		g.hideSelectionCopyMenu()
		g.textSelection = nil
		g.clearTextSelectionStatus()
		s.layer.textGesture = false
		if s.layer.textHighlight != nil {
			s.layer.textHighlight.Hide()
			s.layer.Refresh()
		}
	}
}
func (l *signatureLayer) textPoint(pos fyne.Position) textselect.Point {
	origin, size := l.displayRect()
	if size.Width <= 0 || size.Height <= 0 {
		return textselect.Point{X: -1, Y: -1}
	}
	return textselect.Point{X: float64((pos.X - origin.X) / size.Width), Y: float64((pos.Y - origin.Y) / size.Height)}
}
func (l *signatureLayer) startTextSelection(pos fyne.Position, mods fyne.KeyModifier) bool {
	return l.startSourceSelection(pos, mods, l.selectableLayout(), false, true)
}
func (l *signatureLayer) startQRCodeSelection(pos fyne.Position, mods fyne.KeyModifier) bool {
	return l.startSourceSelection(pos, mods, l.qrLayout(), true, false)
}
func (l *signatureLayer) startSourceSelection(pos fyne.Position, mods fyne.KeyModifier, layout *textselect.Layout, qrCode, characters bool) bool {
	g := l.editor
	if g == nil || g.working || g.effectiveAnnotationTool() != toolSelect || mods&fyne.KeyModifierAlt != 0 {
		return false
	}
	if layout == nil {
		if qrCode {
			g.requestQRCodePage(l.page)
		} else {
			g.requestOCRPage(l.page)
		}
		return false
	}
	word := layout.WordAt(l.textPoint(pos), false)
	if word < 0 {
		return false
	}
	if characters {
		units := layout.SelectionLayout()
		layout = &units
		word = layout.WordAt(l.textPoint(pos), true)
	}
	g.clearAreaSelection()
	g.clearTextSelection()
	g.clearSignatureSelection()
	g.clearRedactionSelection()
	g.activatePDFPage(l.page)
	if g.window != nil {
		g.window.Canvas().Focus(nil)
	}
	g.textSelection = &textSelection{doc: g.document, key: g.document.TextSelectionKey(l.page), layer: l, layout: *layout, start: word, end: word, active: true, qrCode: qrCode}
	l.textGesture = true
	l.refreshTextHighlight()
	return true
}
func (l *signatureLayer) updateTextSelection(pos fyne.Position) {
	g := l.editor
	if g == nil || !g.validTextSelection() {
		return
	}
	s := g.textSelection
	if s.layer != l || !s.active || s.qrCode {
		return
	}
	word := s.layout.WordAt(l.textPoint(pos), true)
	if word >= 0 && word != s.end {
		g.hideSelectionCopyMenu()
		s.end = word
		l.refreshTextHighlight()
	}
}
func (l *signatureLayer) finishTextSelection() {
	if l.editor != nil && l.editor.textSelection != nil && l.editor.textSelection.layer == l {
		l.editor.textSelection.active = false
	}
	l.textGesture = false
	if l.editor != nil {
		l.editor.updateTemporarySelection()
	}
}
func (l *signatureLayer) refreshTextHighlight() {
	s := l.editor.textSelection
	if s == nil {
		return
	}
	l.editor.showTextSelectionStatus()
	words := s.layout.Range(s.start, s.end)
	// Capture immutable geometry: rendering can occur outside the event callback.
	quads := make([]textselect.Quad, len(words))
	for i, w := range words {
		quads[i] = w.Quad
	}
	generator := func(w, h int) image.Image {
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		r := vector.NewRasterizer(w, h)
		for _, q := range quads {
			for i, p := range q {
				x, y := float32(p.X*float64(w)), float32(p.Y*float64(h))
				if i == 0 {
					r.MoveTo(x, y)
				} else {
					r.LineTo(x, y)
				}
			}
			r.ClosePath()
		}
		r.Draw(out, out.Bounds(), image.NewUniform(color.NRGBA{R: 40, G: 130, B: 255, A: 80}), image.Point{})
		return out
	}
	if l.textHighlight == nil {
		l.textHighlight = canvas.NewRaster(generator)
	} else {
		l.textHighlight.Generator = generator
	}
	l.textHighlight.Show()
	l.textHighlight.Refresh()
	l.Refresh()
}
func (l *signatureLayer) DoubleTapped(e *fyne.PointEvent) {
	if l.startQRCodeSelection(e.Position, l.editorModifiers()) || l.startSourceSelection(e.Position, l.editorModifiers(), l.selectableLayout(), false, false) {
		l.finishTextSelection()
	}
}
func (l *signatureLayer) editorModifiers() fyne.KeyModifier {
	if l.editor == nil {
		return 0
	}
	return l.editor.modifiers()
}
func (l *signatureLayer) Cursor() desktop.Cursor {
	if l.editor != nil && l.editor.effectiveAnnotationTool() == toolSelect && l.editorModifiers()&fyne.KeyModifierAlt == 0 {
		if layout := l.qrLayout(); layout != nil && layout.WordAt(l.textPoint(l.textMouse), false) >= 0 {
			return desktop.PointerCursor
		}
		if layout := l.selectableLayout(); layout != nil && layout.WordAt(l.textPoint(l.textMouse), false) >= 0 {
			return desktop.TextCursor
		}
	}
	return desktop.DefaultCursor
}
func (l *signatureLayer) MouseIn(e *desktop.MouseEvent)    { l.textMouse = e.Position }
func (l *signatureLayer) MouseMoved(e *desktop.MouseEvent) { l.textMouse = e.Position }
func (l *signatureLayer) MouseOut()                        { l.textMouse = fyne.NewPos(-math.MaxFloat32, -math.MaxFloat32) }
