package gui

import (
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"image/color"
	"math"
	"time"
)

type areaSelection struct {
	page     int
	rect     document.NormalizedRect
	start    fyne.Position
	active   bool
	layer    *signatureLayer
	feedback *fyne.Animation
}

func (g *Gui) clearAreaSelection() {
	g.clearTextSelection()
	if g.areaSelection != nil {
		if g.areaSelection.feedback != nil {
			g.areaSelection.feedback.Stop()
		}
		l := g.areaSelection.layer
		g.areaSelection = nil
		if l != nil && l.selectionBox != nil {
			l.selectionBox.Hide()
			l.selectionBox.FillColor = color.NRGBA{R: 40, G: 130, B: 255, A: 35}
			l.selectionBox.StrokeColor = color.NRGBA{R: 40, G: 130, B: 255, A: 255}
			l.Refresh()
		}
	}
}
func (g *Gui) showAreaCopied(doc *document.Document, selection *areaSelection) {
	if selection == nil || g.areaSelection != selection || g.document != doc || g.closed {
		return
	}
	l := selection.layer
	if l == nil || l.selectionBox == nil {
		g.clearAreaSelection()
		return
	}
	if selection.feedback != nil {
		selection.feedback.Stop()
	}
	l.selectionBox.FillColor = color.NRGBA{R: 40, G: 130, B: 255, A: 110}
	l.selectionBox.Refresh()
	selection.feedback = fyne.NewAnimation(250*time.Millisecond, func(progress float32) {
		if g.closed || g.document != doc || g.areaSelection != selection {
			selection.feedback.Stop()
			return
		}
		if progress >= 1 {
			selection.feedback = nil
			g.clearAreaSelection()
			return
		}
		l.selectionBox.FillColor = color.NRGBA{R: 40, G: 130, B: 255, A: uint8(110 * (1 - progress))}
		l.selectionBox.StrokeColor = color.NRGBA{R: 40, G: 130, B: 255, A: uint8(255 * (1 - progress))}
		l.selectionBox.Refresh()
	})
	selection.feedback.Start()
}
func (l *signatureLayer) startArea(pos fyne.Position) bool {
	g := l.editor
	if g == nil || g.document == nil || g.effectiveAnnotationTool() != toolSelect || g.working {
		return false
	}
	origin, size := l.displayRect()
	x, y := pos.X-origin.X, pos.Y-origin.Y
	if x < 0 || y < 0 || x > size.Width || y > size.Height {
		return false
	}
	g.clearAreaSelection()
	g.clearSignatureSelection()
	g.clearRedactionSelection()
	g.activatePDFPage(l.page)
	g.areaSelection = &areaSelection{page: l.page, start: pos, active: true, layer: l}
	return true
}
func (l *signatureLayer) updateArea(pos fyne.Position) {
	g := l.editor
	if g == nil || g.areaSelection == nil || g.areaSelection.layer != l || !g.areaSelection.active {
		return
	}
	s := g.areaSelection
	origin, size := l.displayRect()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	x0, y0 := float64((s.start.X-origin.X)/size.Width), float64((s.start.Y-origin.Y)/size.Height)
	x1, y1 := clampFloat64(float64((pos.X-origin.X)/size.Width), 0, 1), clampFloat64(float64((pos.Y-origin.Y)/size.Height), 0, 1)
	s.rect = document.NormalizedRect{X: math.Min(x0, x1), Y: math.Min(y0, y1), Width: math.Abs(x1 - x0), Height: math.Abs(y1 - y0)}
	if !s.rect.Valid() {
		return
	}
	if l.selectionBox == nil {
		l.selectionBox = canvas.NewRectangle(color.NRGBA{R: 40, G: 130, B: 255, A: 35})
		l.selectionBox.StrokeColor = color.NRGBA{R: 40, G: 130, B: 255, A: 255}
		l.selectionBox.StrokeWidth = 1
	}
	l.selectionBox.Show()
	l.Refresh()
}
func (l *signatureLayer) finishArea() {
	if l.editor == nil || l.editor.areaSelection == nil || l.editor.areaSelection.layer != l {
		return
	}
	s := l.editor.areaSelection
	s.active = false
	_, size := l.displayRect()
	if !s.rect.Valid() || s.rect.Width*float64(size.Width) < 3 || s.rect.Height*float64(size.Height) < 3 {
		l.editor.clearAreaSelection()
	}
}
func (l *signatureLayer) MouseDown(e *desktop.MouseEvent) {
	if e.Button == desktop.MouseButtonPrimary {
		if !l.startQRCodeSelection(e.Position, e.Modifier) && !l.startTextSelection(e.Position, e.Modifier) {
			l.startArea(e.Position)
		}
	}
}
func (l *signatureLayer) MouseUp(e *desktop.MouseEvent) {
	if e.Button == desktop.MouseButtonPrimary {
		if l.textGesture {
			l.updateTextSelection(e.Position)
			l.finishTextSelection()
			return
		}
		l.updateArea(e.Position)
		l.finishArea()
	}
}
func (l *signatureLayer) Dragged(e *fyne.DragEvent) {
	if l.textGesture {
		l.updateTextSelection(e.Position)
		return
	}
	if l.editor == nil {
		return
	}
	if l.editor.areaSelection == nil {
		l.startArea(fyne.NewPos(e.Position.X-e.Dragged.DX, e.Position.Y-e.Dragged.DY))
	}
	l.updateArea(e.Position)
}
func (l *signatureLayer) DragEnd() {
	if l.textGesture {
		l.finishTextSelection()
		return
	}
	l.finishArea()
}
func (g *Gui) modifiers() fyne.KeyModifier {
	if g.app != nil {
		if d, ok := g.app.Driver().(desktop.Driver); ok {
			return d.CurrentKeyModifiers()
		}
	}
	return 0
}
