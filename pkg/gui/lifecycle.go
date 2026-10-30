package gui

import (
	"fmt"
	"image"
	"runtime/debug"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

func (g *Gui) requestOpen() {
	g.activateEditor()
	if g.waitAnnotationChanges(g.requestOpen) {
		return
	}
	if g.working {
		return
	}
	g.chooseFile("Open file", openFileExtensions(), BuildOpenFileCallback(g))
}

func (g *Gui) requestClose() {
	if g.waitAnnotationChanges(g.requestClose) {
		return
	}
	if g.working || g.pdfOpenDialog != nil {
		dialog.ShowInformation("Operation in progress", "Wait for the current operation to finish before closing.", g.window)
		return
	}
	g.confirmWindowDocuments(g.closeWindow)
}

func (g *Gui) requestCloseDocument() {
	if g.markdownView != nil && g.markdownView.imagePreview != nil {
		g.markdownView.imagePreview.close()
		return
	}
	if g.waitAnnotationChanges(g.requestCloseDocument) {
		return
	}
	if !g.hasOpenContent() || g.working {
		return
	}
	g.confirmUnsavedChanges("Save changes before closing?", g.closeDocument)
}

func (g *Gui) closeDocumentEntry(entry *editorDocument) {
	if g.waitAnnotationChanges(func() { g.closeDocumentEntry(entry) }) {
		return
	}
	if g.closed || g.working || g.pdfOpenDialog != nil || !slices.Contains(g.documents, entry) {
		return
	}
	if entry != g.activeDocument {
		g.selectDocument(entry)
		if g.activeDocument != entry {
			return
		}
	}
	g.requestCloseDocument()
}

func (g *Gui) closeDocument() {
	g.stopGIFPlayback()
	g.logEvent("document_closed", "")
	g.removeActiveDocument()
	g.closeSearch()
	g.stopFormPulse()
	if g.showingMarkdown() {
		g.window.Canvas().Focus(nil)
		g.clearMarkdown()
		g.imageScroll.Show()
		g.updateHistoryActions()
		g.restoreRemainingDocument()
		return
	}
	if g.showingModel() {
		g.clearModel()
		g.panModifierDown = false
		g.imageScroll.Show()
		g.updateHistoryActions()
		g.restoreRemainingDocument()
		return
	}
	g.window.Canvas().Focus(nil)
	g.setAnnotationTool(toolSelect)
	g.stopOCR()
	g.stopSelectionModifierTimer()
	g.cancelThumbnailDrag()
	g.pdfGeneration++
	g.clearPDFDetail()
	g.panModifierDown = false
	g.setPanActive(false)
	g.clearAreaSelection()
	g.clearSignatureSelection()
	g.clearRedactionSelection()
	doc := g.document
	g.document = nil
	g.saveTarget = nil
	g.fileSizeBytes = 0
	g.objectPasteDocument = nil
	g.objectPasteCount = 0
	g.redactedTargets = nil
	g.zoom, g.fitZoom, g.zoomLevel = 1, 1, 0
	g.activePDFPage = 0
	g.imageSignatureLayer.SetDocument(nil, 0, image.Point{})
	g.signatureLayers = map[int]*signatureLayer{}
	g.formLayers = nil
	g.correctionLayers = nil
	g.panOverlays = []*panOverlay{g.imagePanOverlay}
	g.pdfCrops = nil
	g.pdfPageIDs = nil
	g.pdfThumbDropIndicators = nil
	g.pdfThumbImages, g.pdfPageImages = nil, nil
	g.pdfThumbPlaceholders = nil
	g.pdfThumbBorders, g.pdfPageBorders = nil, nil
	g.pdfThumbLoaded, g.pdfThumbLoading = nil, nil
	g.pdfPageLoaded, g.pdfPageLoading = nil, nil
	g.pdfPageSizes, g.pdfPageContainers = nil, nil
	g.pdfPages.RemoveAll()
	g.thumbList.RemoveAll()
	g.pdfScroll.Hide()
	g.sidebar.container.Hide()
	g.image.Image = nil
	g.image.SetMinSize(fyne.NewSize(0, 0))
	g.image.Refresh()
	g.imageScroll.Show()
	g.updateHistoryActions()
	if doc != nil {
		go func() {
			doc.Close()
			debug.FreeOSMemory()
		}()
	} else {
		go debug.FreeOSMemory()
	}
	g.restoreRemainingDocument()
}

func (g *Gui) closeWindow() {
	g.stopGIFPlayback()
	if !g.closed {
		g.saveSession()
	}
	if g.printJob != nil {
		state := g.printJob
		state.cancel()
		g.printJob = nil
		if printOwner == g {
			printOwner = nil
		}
	}
	g.cancelTextPlacement()
	g.logEvent("window_closed", "documents=%d", len(g.documents))
	g.setTextEditing(false)
	g.closeSearch()
	g.stopFormPulse()
	for _, layer := range g.signatureLayers {
		if layer.highlightInput != nil {
			layer.highlightInput.cancel()
		}
	}
	g.closeRetainedDocuments()
	g.closed = true
	g.stopDebugInfo()
	g.unregisterEditor()
	g.hideObjectOrderMenu()
	g.clearModel()
	g.clearMarkdown()
	g.stopOCR()
	if g.signatureDialog != nil {
		g.signatureDialog.Hide()
	}
	g.tipGeneration++
	if g.tipTimer != nil {
		g.tipTimer.Stop()
		g.tipTimer = nil
	}
	g.stopSelectionModifierTimer()
	g.cancelFilePicker()
	g.cancelThumbnailDrag()
	g.pdfGeneration++
	g.clearPDFDetail()
	if g.document != nil {
		doc := g.document
		g.document = nil
		go func() {
			doc.Close()
			debug.FreeOSMemory()
		}()
	}
	if g.annotationJobs != nil {
		close(g.annotationJobs)
		g.annotationJobs = nil
	}
	g.window.SetCloseIntercept(nil)
	g.window.Close()
	if len(g.app.Driver().AllWindows()) == 0 {
		releaseSignatureService(g.app)
		g.app.Quit()
	}
}

func (g *Gui) confirmDiscard(action func()) {
	g.confirmUnsavedChanges("Save changes before continuing?", action)
}

func (g *Gui) confirmUnsavedChanges(message string, action func()) {
	if g.document == nil || !g.document.Dirty() {
		action()
		return
	}
	doc := g.document
	prompt := dialog.NewCustomWithoutButtons("Unsaved changes", widget.NewLabel(message), g.window)
	g.pauseExternalOpening(prompt)
	save := widget.NewButton("Save", func() {
		prompt.Hide()
		g.saveWithCompletion(func() {
			if g.document == doc {
				g.confirmUnsavedChanges(message, action)
			}
		})
	})
	save.Importance = widget.HighImportance
	prompt.SetButtons([]fyne.CanvasObject{
		save,
		widget.NewButton("Don't save", func() { prompt.Hide(); action() }),
		widget.NewButton("Cancel", prompt.Hide),
	})
	prompt.Show()
}

func (g *Gui) updateTitle() {
	title := "Proview"
	if g.document != nil {
		title = fmt.Sprintf("%s — Proview", g.document.Name())
		if g.document.Dirty() {
			title = "• " + title
		}
	}
	if g.showingModel() {
		title = fmt.Sprintf("%s — Proview", g.modelViewport.mesh.Name)
	}
	if g.showingMarkdown() {
		title = fmt.Sprintf("%s — Proview", g.markdownView.doc.Name)
	}
	if g.window != nil {
		g.window.SetTitle(title)
	}
}

func (g *Gui) installKeyHandler() {
	previous := g.window.Canvas().OnTypedKey()
	g.window.Canvas().SetOnTypedKey(func(event *fyne.KeyEvent) {
		g.activateEditor()
		if g.handleDocumentNavigationKey(event.Name) {
			return
		}
		if g.handleModelKey(event.Name) {
			return
		}
		if event.Name == fyne.KeyEscape && (g.signatureEscapeHandled || g.closeSignatureDialogOnEscape() || g.cancelCurrentOperation()) {
			return
		}
		if event.Name == fyne.KeyDelete && g.hasSelectedSignature {
			if _, ok := g.window.Canvas().Focused().(*annotationEntry); ok {
				if previous != nil {
					previous(event)
				}
				return
			}
			g.deleteSelectedSignature()
			return
		}
		if previous != nil {
			previous(event)
		}
	})
	if canvas, ok := g.window.Canvas().(desktop.Canvas); ok {
		previousDown := canvas.OnKeyDown()
		previousUp := canvas.OnKeyUp()
		canvas.SetOnKeyDown(func(event *fyne.KeyEvent) {
			g.signatureEscapeHandled = event.Name == fyne.KeyEscape && g.closeSignatureDialogOnEscape()
			if g.signatureEscapeHandled {
				return
			}
			g.handleSelectionShift(event.Name, true)
			g.handlePanKey(event.Name, true)
			if previousDown != nil {
				previousDown(event)
			}
		})
		canvas.SetOnKeyUp(func(event *fyne.KeyEvent) {
			g.handleSelectionShift(event.Name, false)
			g.handlePanKey(event.Name, false)
			if previousUp != nil {
				previousUp(event)
			}
		})
	}
}

func (g *Gui) cancelCurrentOperation() bool {
	if g.cancelTextPlacement() {
		return true
	}
	if g.correctionDialog != nil {
		g.correctionDialog.Hide()
		return true
	}
	if g.textEditing {
		if g.cancelTextMove() {
			return true
		}
		g.setTextEditing(false)
		return true
	}
	if g.search != nil && g.search.bar.Visible() {
		g.closeSearch()
		return true
	}
	if g.formsActive {
		g.setFormsActive(false)
		return true
	}
	if g.objectOrderMenu != nil && g.objectOrderMenu.Visible() {
		g.hideObjectOrderMenu()
		return true
	}
	if g.showingMarkdown() && g.markdownView.clearSelection() {
		return true
	}
	if g.showingMarkdown() && g.markdownOutlineExpanded {
		g.markdownView.toggleOutline()
		return true
	}
	if g.modelCrop != nil && g.modelCrop.active {
		g.modelCrop.Deactivate()
		return true
	}
	if g.textSelection != nil {
		g.clearTextSelection()
		return true
	}
	if g.areaSelection != nil {
		g.clearAreaSelection()
		return true
	}
	if g.cancelThumbnailDrag() {
		return true
	}
	for _, crop := range g.pdfCrops {
		if crop.active {
			crop.Deactivate()
			return true
		}
	}
	for _, layer := range g.signatureLayers {
		if layer.highlightInput != nil && layer.highlightInput.cancel() {
			return true
		}
		if layer.redactInput != nil && layer.redactInput.cancel() {
			return true
		}
		if layer.input != nil && layer.input.cancel() {
			return true
		}
	}
	if g.annotationTool != toolSelect {
		g.setAnnotationTool(toolSelect)
		return true
	}
	if g.cropOverlay != nil && g.cropOverlay.active {
		g.cropOverlay.Deactivate()
		return true
	}
	for _, layer := range g.signatureLayers {
		for _, signature := range layer.widgets {
			if !signature.editActive || g.document == nil {
				continue
			}
			if g.document.CancelSignatureEdit(signature.id) == nil {
				signature.editActive = false
				g.syncDocumentView()
				return true
			}
		}
	}
	if g.imageSignatureLayer != nil {
		for _, signature := range g.imageSignatureLayer.widgets {
			if signature.editActive && g.document != nil {
				if g.document.CancelOverlayEdit(signature.id) == nil {
					signature.editActive = false
					g.syncDocumentView()
					return true
				}
			}
		}
	}
	return false
}
