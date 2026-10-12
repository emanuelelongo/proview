package gui

import (
	"emanuelelongo/proview/pkg/document"
	"slices"

	"fyne.io/fyne/v2/dialog"
)

func (g *Gui) undo() {
	if g.waitAnnotationChanges(g.undo) {
		return
	}
	if g.working || g.document == nil || !g.document.Undo() {
		return
	}
	g.syncDocumentView()
}

func (g *Gui) redo() {
	if g.waitAnnotationChanges(g.redo) {
		return
	}
	if g.working || g.document == nil || !g.document.Redo() {
		return
	}
	g.syncDocumentView()
}

func (g *Gui) syncDocumentView() {
	g.clearAreaSelection()
	if g.document == nil {
		g.updateHistoryActions()
		return
	}

	g.clearSignatureSelection()
	g.clearRedactionSelection()
	g.cropOverlay.Deactivate()
	if g.document.Format() == document.FormatPDF {
		if !slices.Equal(g.pdfPageIDs, g.document.PageIDs()) {
			g.rebuildPDF(g.activePDFPage)
			return
		}
		for page, layer := range g.signatureLayers {
			layer.reload()
			g.scheduleLensPreviews(layer)
			g.refreshPagePreview(page)
		}
		g.updateHistoryActions()
		return
	}

	preview, err := g.document.RenderBasePage(0)
	if err != nil {
		dialog.ShowError(err, g.window)
		return
	}
	g.image.Image = preview
	g.image.Refresh()
	g.imageSignatureLayer.SetDocument(g.document, 0, preview.Bounds().Size())
	g.scheduleLensPreviews(g.imageSignatureLayer)
	g.applyZoom()
	g.updateHistoryActions()
}

func (g *Gui) updateHistoryActions() {
	g.syncOCR()
	g.updateFileInfo()
	hasDocument := g.document != nil
	if g.openAction != nil {
		g.openAction.Enable()
	}
	if g.blankAction != nil {
		g.blankAction.Enable()
	}
	if g.zoomOutAction != nil {
		setToolbarActionEnabled(g.zoomOutAction, g.hasOpenContent())
	}
	if g.zoomInAction != nil {
		setToolbarActionEnabled(g.zoomInAction, g.hasOpenContent())
	}
	if g.signatureAction != nil {
		setToolbarActionEnabled(g.signatureAction, !g.showingModel())
	}
	if g.signatureItem != nil {
		g.signatureItem.Disabled = g.showingModel()
	}
	g.updateTransformActions()
	if g.cropAction != nil {
		setToggleToolbarActionEnabled(g.cropAction, g.hasOpenContent())
	}
	for tool, action := range g.annotationActions {
		setToggleToolbarActionEnabled(action, hasDocument)
		action.SetActive(hasDocument && tool == g.effectiveAnnotationTool())
	}
	if action := g.annotationActions[toolRedact]; action != nil {
		if g.document != nil && g.document.Format() == document.FormatPDF {
			action.button.Enable()
		} else {
			action.button.Disable()
		}
	}
	if g.undoItem != nil {
		g.undoItem.Disabled = !hasDocument || !g.document.CanUndo()
	}
	if g.redoItem != nil {
		g.redoItem.Disabled = !hasDocument || !g.document.CanRedo()
	}
	if g.saveItem != nil {
		g.saveItem.Disabled = g.document == nil
	}
	if g.saveAsItem != nil {
		g.saveAsItem.Disabled = !hasDocument
	}
	for _, item := range g.menuDocumentItems {
		item.Disabled = !hasDocument
		if item.Label == "Crop" || item.Label == "Close" || item.Label == "Zoom In" || item.Label == "Zoom Out" || item.Label == "Fit to Window" {
			item.Disabled = !g.hasOpenContent()
		}
	}
	if g.document == nil || g.document.Format() != document.FormatPDF {
		if action := g.annotationActions[toolRedact]; action != nil {
			action.button.Disable()
		}
		if g.menuDocumentItems != nil {
			for _, item := range g.menuDocumentItems {
				if item.Label == "Redact" {
					item.Disabled = true
				}
			}
		}
	}
	updateModelMenu(g)
	g.updateTitle()
	for _, menu := range g.stateMenus {
		refreshMenuState(menu)
	}
}

func (g *Gui) updateTransformActions() {
	hasDocument := g.document != nil
	for _, action := range g.transformActions {
		setToolbarActionEnabled(action, hasDocument)
	}
	flipEnabled := hasDocument && (g.document.Format() != document.FormatPDF || g.hasSelectedSignature)
	for _, action := range g.flipActions {
		setToolbarActionEnabled(action, flipEnabled)
	}
}

func setToolbarActionEnabled(action *iconToolbarAction, enabled bool) {
	if enabled {
		action.Enable()
	} else {
		action.Disable()
	}
}

func setToggleToolbarActionEnabled(action *toggleToolbarAction, enabled bool) {
	if enabled {
		action.Enable()
	} else {
		action.Disable()
	}
}
