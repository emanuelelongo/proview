package gui

import (
	"emanuelelongo/proview/pkg/document"
	"fmt"
	"image"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (g *Gui) promptSignature() {
	if g.working || g.showingModel() {
		return
	}
	g.showSignatureDropdown()
}

func defaultSignatureRect(pageWidth, pageHeight, signatureWidth, signatureHeight int) (document.NormalizedRect, error) {
	if pageWidth <= 0 || pageHeight <= 0 || signatureWidth <= 0 || signatureHeight <= 0 {
		return document.NormalizedRect{}, fmt.Errorf("invalid signature or page dimensions")
	}

	width := 0.3
	height := width * float64(pageWidth) * float64(signatureHeight) /
		(float64(pageHeight) * float64(signatureWidth))
	if height > 0.2 {
		height = 0.2
		width = height * float64(pageHeight) * float64(signatureWidth) /
			(float64(pageWidth) * float64(signatureHeight))
	}
	return document.NewNormalizedRect((1-width)/2, (1-height)/2, width, height)
}

func (g *Gui) selectSignature(layer *signatureLayer, id document.SignatureID) {
	if id != 0 || g.textPlacement != nil && g.textPlacement.original != nil {
		g.finishTextPlacement()
	}
	if id != 0 {
		g.clearTextSelection()
		g.clearRedactionSelection()
	}
	if g.selectedSignature != id && g.window != nil {
		g.annotationPropertyGeneration++
		if _, ok := g.window.Canvas().Focused().(*annotationEntry); ok {
			g.window.Canvas().Focus(nil)
		}
		if g.annotationPreviewTimer != nil {
			g.annotationPreviewTimer.Stop()
			g.annotationPreviewTimer = nil
			g.annotationPreviewWork = nil
		}
		g.restoreAnnotationPreview()
	}
	for _, candidate := range g.signatureLayers {
		if candidate == layer && id != 0 {
			candidate.selectSignature(id)
		} else {
			candidate.selectSignature(0)
		}
	}
	g.selectedSignature = id
	g.hasSelectedSignature = id != 0
	if layer != nil {
		g.selectedSignaturePage = layer.page
	}
	if g.imageSignatureLayer != nil {
		selected := document.SignatureID(0)
		if layer == g.imageSignatureLayer {
			selected = id
		}
		g.imageSignatureLayer.selectSignature(selected)
	}
	g.refreshAnnotationControls()
	g.updateTransformActions()
}

func (g *Gui) clearSignatureSelection() {
	g.hideObjectOrderMenu()
	g.selectSignature(nil, 0)
}

func (g *Gui) deleteSelectedSignature() {
	if g.selectedRedaction != 0 {
		g.deleteSelectedRedaction()
		return
	}
	if g.working {
		return
	}
	if g.document == nil || !g.hasSelectedSignature {
		dialog.ShowInformation("No signature selected", "Select a signature to delete it.", g.window)
		return
	}
	page := g.selectedSignaturePage
	if p := g.textPlacement; p != nil && p.original != nil && p.original.ID == g.selectedSignature {
		g.cancelTextPlacement()
	}
	if err := g.document.RemoveSignature(g.selectedSignature); err != nil {
		g.showOperationError("preview", err)
		return
	}
	g.clearSignatureSelection()
	if layer := g.signatureLayers[page]; layer != nil {
		layer.reload()
	}
	g.documentChanged(page)
}

func (g *Gui) promptSignatureStroke() {
	if g.document == nil || !g.hasSelectedSignature {
		dialog.ShowInformation("No signature selected", "Select a signature to change its stroke width.", g.window)
		return
	}
	var selected document.SignatureOverlay
	found := false
	for _, signature := range g.document.Signatures() {
		if signature.ID == g.selectedSignature {
			selected = signature
			found = true
			break
		}
	}
	if !found {
		g.showOperationError("signature_edit", document.ErrSignatureNotFound)
		return
	}
	if _, ok := selected.Annotation(); ok {
		g.setAnnotationTool(toolSelect)
		g.refreshAnnotationControls()
		return
	}
	if selected.IsVector() {
		if selected.Asset().Drawing == nil {
			g.showSignatureProperties(selected)
			return
		}
		g.editSelectedDrawing(selected)
		return
	}

	value := widget.NewLabel(fmt.Sprintf("%d", selected.StrokeWidth))
	slider := widget.NewSlider(0, 10)
	slider.Step = 1
	slider.Value = float64(selected.StrokeWidth)
	slider.OnChanged = func(current float64) {
		value.SetText(fmt.Sprintf("%d", int(math.Round(current))))
	}
	control := container.NewBorder(nil, nil, nil, value, slider)
	dialog.NewForm(
		"Signature Stroke",
		"Apply",
		"Cancel",
		[]*widget.FormItem{widget.NewFormItem("Width", control)},
		func(confirm bool) {
			if !confirm || g.document == nil {
				return
			}
			page := g.selectedSignaturePage
			id := g.selectedSignature
			if err := g.document.SetSignatureStroke(id, int(math.Round(slider.Value))); err != nil {
				g.showOperationError("preview", err)
				return
			}
			if layer := g.signatureLayers[page]; layer != nil {
				layer.reload()
				g.selectSignature(layer, id)
			}
			g.documentChanged(page)
		},
		g.window,
	).Show()
}

func (g *Gui) activatePDFPage(page int) {
	g.activateEditor()
	if g.document == nil || g.document.Format() != document.FormatPDF {
		return
	}
	if page >= 0 && page < g.document.PageCount() {
		if page == g.activePDFPage {
			return
		}
		g.finishTextPlacement()
		if g.activePDFPage < len(g.pdfPageBorders) {
			g.pdfPageBorders[g.activePDFPage].Hide()
		}
		g.clearAreaSelection()
		g.deactivatePDFCrops()
		g.activePDFPage = page
		if !g.thumbnailSelectionFrozen && (g.thumbnailSelectionDocument != g.document || len(g.thumbnailSelection) <= 1) {
			g.setThumbnailSelection([]int{page})
		}
		g.updateFileInfo()
		if g.annotationTool == toolRedact {
			if layer := g.signatureLayers[page]; layer != nil && layer.redactInput != nil {
				layer.redactInput.load()
			}
		}
		if page < len(g.pdfPageBorders) {
			g.pdfPageBorders[page].Show()
		}
	}
}

func (g *Gui) pdfPageTop(page int) float32 {
	top := float32(0)
	for index := 0; index < page && index < len(g.pdfPageContainers); index++ {
		top += g.pdfPageContainers[index].MinSize().Height + theme.Padding()
	}
	return top
}

func (g *Gui) loadVisiblePDFPages() {
	defer g.syncOCR()
	if g.document == nil || g.document.Format() != document.FormatPDF || g.pdfScroll.Size().Height <= 0 {
		return
	}
	top := g.pdfScroll.Offset.Y
	bottom := top + g.pdfScroll.Size().Height
	lastVisible := -1
	selected, largestOverlap := -1, float32(0)
	pageTop := float32(0)
	for page, object := range g.pdfPageContainers {
		pageBottom := pageTop + object.MinSize().Height
		if pageBottom > top && pageTop < bottom {
			g.loadPDFPage(page)
			lastVisible = page
			overlap := min(pageBottom, bottom) - max(pageTop, top)
			if overlap > largestOverlap {
				selected, largestOverlap = page, overlap
			}
		}
		pageTop = pageBottom + theme.Padding()
	}
	if selected >= 0 {
		g.activatePDFPage(selected)
	}
	g.refreshPDFDetail()
	if lastVisible >= 0 && lastVisible+1 < len(g.pdfPageContainers) {
		g.loadPDFPage(lastVisible + 1)
	}
}

func (g *Gui) loadVisiblePDFThumbnails() {
	if g.closed || g.document == nil || g.document.Format() != document.FormatPDF || g.sidebar.container.Size().Height <= 0 {
		return
	}
	if g.thumbnailDrag != nil {
		g.updateThumbnailDrop(g.thumbnailDrag)
	}
	top := g.sidebar.container.Offset.Y
	bottom := top + g.sidebar.container.Size().Height
	thumbTop := float32(0)
	lastVisible := -1
	for page, object := range g.thumbList.Objects {
		thumbBottom := thumbTop + object.MinSize().Height
		if thumbBottom > top && thumbTop < bottom {
			g.loadPDFThumbnail(page)
			lastVisible = page
		}
		thumbTop = thumbBottom + theme.Padding()
	}
	if lastVisible >= 0 && lastVisible+1 < len(g.thumbList.Objects) {
		g.loadPDFThumbnail(lastVisible + 1)
	}
}

func (g *Gui) loadPDFThumbnail(page int) {
	if page < 0 || page >= len(g.pdfThumbLoading) || g.pdfThumbLoaded[page] || g.pdfThumbLoading[page] {
		return
	}
	g.pdfThumbLoading[page] = true
	doc := g.document
	generation := g.pdfGeneration
	snapshot := doc.RenderingSnapshot()
	started := time.Now()
	go func() {
		var preview image.Image
		var err error
		if overlays, _ := snapshot.OverlaysForPage(page); len(overlays) > 0 {
			preview, err = snapshot.RenderPage(page)
		} else {
			preview, err = snapshot.Thumbnail(page)
		}
		fyne.Do(func() {
			if g.closed || g.document != doc || generation != g.pdfGeneration || page >= len(g.pdfThumbLoading) {
				return
			}
			g.pdfThumbLoading[page] = false
			if err != nil {
				g.showOperationError("preview", err)
				return
			}
			if g.pdfThumbLoaded[page] {
				return
			}
			g.pdfThumbImages[page].Image = preview
			g.pdfThumbImages[page].Refresh()
			g.pdfThumbLoaded[page] = true
			g.hideThumbnailPlaceholder(page)
			if elapsed := time.Since(started); elapsed >= time.Second {
				g.logEvent("pdf_thumbnail_slow", "page=%d elapsed=%s", page+1, elapsed.Round(time.Millisecond))
			}
		})
	}()
}

func (g *Gui) loadPDFPage(page int) {
	if g.document == nil || g.document.Format() != document.FormatPDF ||
		page < 0 || page >= len(g.pdfPageLoaded) || g.pdfPageLoaded[page] || g.pdfPageLoading[page] {
		return
	}
	g.pdfPageLoading[page] = true
	doc := g.document
	generation := g.pdfGeneration
	snapshot := doc.RenderingSnapshot()
	go func() {
		preview, err := snapshot.Preview(page)
		fyne.Do(func() {
			if g.document != doc || generation != g.pdfGeneration || page >= len(g.pdfPageLoading) {
				return
			}
			g.pdfPageLoading[page] = false
			if err != nil {
				g.showOperationError("preview", err)
				return
			}
			g.pdfPageImages[page].Image = preview
			if g.pdfPageSizes[page] != preview.Bounds().Size() {
				g.pdfPageSizes[page] = preview.Bounds().Size()
				g.pdfPageImages[page].SetMinSize(fyne.NewSize(float32(preview.Bounds().Dx())*g.zoom, float32(preview.Bounds().Dy())*g.zoom))
				g.pdfPages.Refresh()
			}
			g.pdfPageImages[page].Refresh()
			if layer := g.signatureLayers[page]; layer != nil {
				layer.SetPageSize(preview.Bounds().Size())
			}
			g.pdfPageLoaded[page] = true
			g.refreshPDFDetail()
			g.scheduleLensPreviews(g.signatureLayers[page])
			if overlays, err := doc.SignaturesForPage(page); err == nil && (len(overlays) > 0 || len(doc.RedactionsForPage(page)) > 0) {
				g.refreshPagePreview(page)
			}
		})
	}()
}

func (g *Gui) refreshPagePreview(page int) {
	if g.document == nil || g.document.Format() != document.FormatPDF ||
		page < 0 || page >= len(g.pdfThumbImages) {
		return
	}
	if page < len(g.pdfPageLoaded) && !g.pdfPageLoaded[page] {
		return
	}
	doc := g.document
	revision := doc.Revision()
	snapshot := doc.RenderingSnapshot()
	go func() {
		preview, err := snapshot.RenderPage(page)
		fyne.Do(func() {
			if g.document != doc || doc.Revision() != revision || page >= len(g.pdfThumbImages) {
				return
			}
			if err != nil {
				g.showOperationError("preview", err)
				return
			}
			g.pdfThumbImages[page].Image = preview
			g.pdfThumbImages[page].Refresh()
			g.pdfThumbLoaded[page] = true
			g.hideThumbnailPlaceholder(page)
		})
	}()
}

func (g *Gui) hideThumbnailPlaceholder(page int) {
	if page >= 0 && page < len(g.pdfThumbPlaceholders) {
		g.pdfThumbPlaceholders[page].Hide()
	}
}

func (g *Gui) documentChanged(page int) {
	layer := g.signatureLayers[page]
	if g.document != nil && g.document.Format() != document.FormatPDF {
		layer = g.imageSignatureLayer
	}
	g.scheduleLensPreviews(layer)
	g.refreshPDFDetail()
	g.refreshPagePreview(page)
	g.updateHistoryActions()
}
