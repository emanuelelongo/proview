package gui

import (
	"emanuelelongo/proview/pkg/document"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func (g *Gui) hideObjectOrderMenu() {
	if g.objectOrderMenu != nil {
		g.objectOrderMenu.Hide()
		g.objectOrderMenu = nil
	}
}

func (l *signatureLayer) showObjectOrderMenuAt(event *fyne.PointEvent) bool {
	for i := len(l.widgets) - 1; i >= 0; i-- {
		w := l.widgets[i]
		position, size := l.widgetRect(w.rect)
		if event.Position.X >= position.X && event.Position.X <= position.X+size.Width &&
			event.Position.Y >= position.Y && event.Position.Y <= position.Y+size.Height {
			l.showObjectOrderMenu(w.id, event.AbsolutePosition)
			return true
		}
	}
	return false
}

func (l *signatureLayer) showObjectOrderMenu(id document.OverlayID, position fyne.Position) {
	g := l.editor
	if g == nil || g.closed || g.document == nil || g.document != l.document || g.working {
		return
	}
	g.hideObjectOrderMenu()
	g.clearAreaSelection()
	l.selectFromWidget(id)
	overlays, err := g.document.OverlaysForPage(l.page)
	if err != nil {
		return
	}
	index := -1
	for i, overlay := range overlays {
		if overlay.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	doc := g.document
	action := func(order document.OverlayOrder) func() {
		return func() {
			g.hideObjectOrderMenu()
			g.reorderObject(doc, l.page, id, order)
		}
	}
	front := fyne.NewMenuItem("Bring to Front", action(document.BringToFront))
	forward := fyne.NewMenuItem("Bring Forward", action(document.BringForward))
	backward := fyne.NewMenuItem("Send Backward", action(document.SendBackward))
	back := fyne.NewMenuItem("Send to Back", action(document.SendToBack))
	front.Disabled, forward.Disabled = index == len(overlays)-1, index == len(overlays)-1
	backward.Disabled, back.Disabled = index == 0, index == 0
	g.objectOrderMenu = widget.NewPopUpMenu(fyne.NewMenu("", front, forward, backward, back), g.window.Canvas())
	g.objectOrderMenu.ShowAtPosition(position)
}

func (g *Gui) reorderObject(doc *document.Document, page int, id document.OverlayID, order document.OverlayOrder) {
	if g.closed || g.document != doc {
		return
	}
	if g.waitAnnotationChanges(func() { g.reorderObject(doc, page, id, order) }) || g.working {
		return
	}
	if err := doc.ReorderOverlay(id, order); err != nil {
		dialog.ShowError(err, g.window)
		return
	}
	layer := g.signatureLayers[page]
	if doc.Format() != document.FormatPDF {
		layer = g.imageSignatureLayer
	}
	if layer != nil {
		layer.reload()
		g.selectSignature(layer, id)
	}
	g.documentChanged(page)
}

func (w *signatureWidget) TappedSecondary(event *fyne.PointEvent) {
	w.layer.showObjectOrderMenu(w.id, event.AbsolutePosition)
}

func (p *annotationInput) TappedSecondary(event *fyne.PointEvent) {
	p.layer.TappedSecondary(event)
}
