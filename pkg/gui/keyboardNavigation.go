package gui

import (
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
)

// These are viewer keys, not global shortcuts: focused widgets, modifiers,
// dialogs and unfinished editing gestures retain ownership of their input.
func (g *Gui) handleDocumentNavigationKey(key fyne.KeyName) bool {
	if g.showingMarkdown() {
		return g.handleMarkdownKey(key)
	}
	switch key {
	case fyne.KeyLeft, fyne.KeyRight, fyne.KeyUp, fyne.KeyDown, fyne.KeyPageUp, fyne.KeyPageDown:
	default:
		return false
	}
	if g.closed || g.working || g.document == nil || g.window == nil || g.modifiers() != 0 || g.textPlacement != nil || g.textMove != nil || g.thumbnailDrag != nil || g.thumbnailPress != nil {
		return false
	}
	if g.window.Canvas().Focused() != nil || g.window.Canvas().Overlays().Top() != nil {
		return false
	}
	if g.areaSelection != nil && g.areaSelection.active {
		return false
	}
	if g.cropOverlay != nil && g.cropOverlay.selecting {
		return false
	}
	for _, crop := range g.pdfCrops {
		if crop.selecting {
			return false
		}
	}
	for _, layer := range g.signatureLayers {
		if layer.textGesture || (layer.input != nil && layer.input.active) || (layer.highlightInput != nil && (layer.highlightInput.active || layer.highlightInput.pending)) || (layer.redactInput != nil && (layer.redactInput.active || layer.redactInput.pending)) {
			return false
		}
		for _, object := range layer.widgets {
			if object.editActive {
				return false
			}
		}
	}
	if key == fyne.KeyPageUp || key == fyne.KeyPageDown {
		if g.document.Format() != document.FormatPDF || g.pdfScroll == nil {
			return false
		}
		delta := 1
		if key == fyne.KeyPageUp {
			delta = -1
		}
		page := max(0, min(g.document.PageCount()-1, g.activePDFPage+delta))
		if page != g.activePDFPage {
			g.loadPDFPage(page)
			g.pdfScroll.ScrollToOffset(fyne.NewPos(g.pdfScroll.Offset.X, g.pdfPageTop(page)))
			g.loadVisiblePDFPages()
			// Near the document's end, clamping the viewport can leave another page
			// more visible. Keep the explicitly requested page active nonetheless.
			g.activatePDFPage(page)
			g.revealThumbnail(page)
		}
		return true
	}
	scroll := g.imageScroll
	if g.document.Format() == document.FormatPDF {
		scroll = g.pdfScroll
	}
	if scroll == nil {
		return false
	}
	const step float32 = 40
	offset := scroll.Offset
	switch key {
	case fyne.KeyLeft:
		offset.X -= step
	case fyne.KeyRight:
		offset.X += step
	case fyne.KeyUp:
		offset.Y -= step
	case fyne.KeyDown:
		offset.Y += step
	}
	scroll.ScrollToOffset(offset)
	if g.document.Format() == document.FormatPDF {
		g.loadVisiblePDFPages()
	}
	return true
}
