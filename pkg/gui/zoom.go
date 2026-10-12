package gui

import (
	"emanuelelongo/proview/pkg/document"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

const (
	maxZoomLevel = 10
	zoomStep     = float32(1.25)
)

func (g *Gui) zoomIn() {
	if g.showingModel() {
		g.modelViewport.zoom(1.25)
		return
	}
	if g.document == nil || g.zoomLevel >= maxZoomLevel {
		return
	}
	g.cropOverlay.Deactivate()
	g.zoomLevel++
	g.updateZoomFromLevel()
	g.setPanActive(g.panModifierDown)
}

func (g *Gui) zoomOut() {
	if g.showingModel() {
		g.modelViewport.zoom(.8)
		return
	}
	if g.document == nil || g.zoomLevel == 0 {
		return
	}
	g.cropOverlay.Deactivate()
	g.zoomLevel--
	g.updateZoomFromLevel()
	g.setPanActive(g.panModifierDown)
}

func (g *Gui) resetZoom() {
	if g.showingModel() {
		g.modelCrop.Deactivate()
		g.modelViewport.camera.PanX = 0
		g.modelViewport.camera.PanY = 0
		g.modelViewport.camera.Zoom = 1
		g.modelViewport.requestRender()
		return
	}
	if g.document == nil {
		return
	}
	g.cropOverlay.Deactivate()
	g.fitZoom = g.calculateFitZoom()
	g.zoomLevel = 0
	g.updateZoomFromLevel()
	g.setPanActive(false)
}

func (g *Gui) updateZoomFromLevel() {
	g.zoom = g.fitZoom * float32(math.Pow(float64(zoomStep), float64(g.zoomLevel)))
	g.applyZoom()
}

func (g *Gui) calculateFitZoom() float32 {
	var pageSize fyne.Size
	var viewport fyne.Size
	if g.document.Format() == document.FormatPDF {
		size, err := g.document.PageSize(0)
		if err != nil {
			return 1
		}
		pageSize = fyne.NewSize(float32(size.X), float32(size.Y))
		viewport = g.pdfScroll.Size()
	} else {
		if g.image.Image == nil {
			return 1
		}
		bounds := g.image.Image.Bounds()
		pageSize = fyne.NewSize(float32(bounds.Dx()), float32(bounds.Dy()))
		viewport = g.imageScroll.Size()
	}
	availableWidth := viewport.Width - theme.Padding()*2
	availableHeight := viewport.Height - theme.Padding()*2
	if pageSize.Width <= 0 || pageSize.Height <= 0 || availableWidth <= 0 || availableHeight <= 0 {
		return 1
	}
	return min(availableWidth/pageSize.Width, availableHeight/pageSize.Height)
}

func (g *Gui) applyZoom() {
	for _, layer := range g.signatureLayers {
		layer.hideRedactionReveals()
		if layer.redactInput != nil {
			layer.redactInput.finish()
		}
		if layer.input != nil {
			layer.input.finish()
		}
	}
	if g.document == nil {
		return
	}
	if g.document.Format() == document.FormatPDF {
		for page, pageImage := range g.pdfPageImages {
			size := g.pdfPageSizes[page]
			pageImage.SetMinSize(fyne.NewSize(float32(size.X)*g.zoom, float32(size.Y)*g.zoom))
			if layer := g.signatureLayers[page]; layer != nil {
				layer.SetZoom(g.zoom)
			}
		}
		g.pdfPages.Refresh()
		g.loadVisiblePDFPages()
		return
	}
	if g.image.Image == nil {
		return
	}
	bounds := g.image.Image.Bounds()
	g.image.SetMinSize(fyne.NewSize(float32(bounds.Dx())*g.zoom, float32(bounds.Dy())*g.zoom))
	g.imageSignatureLayer.SetZoom(g.zoom)
	g.imageContent.Refresh()
}
