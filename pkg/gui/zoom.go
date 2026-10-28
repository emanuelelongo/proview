package gui

import (
	"emanuelelongo/proview/pkg/document"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

const (
	minZoomLevel = -12
	maxZoomLevel = 10
	zoomStep     = float32(1.25)
)

func (g *Gui) zoomIn()  { g.zoomBy(1, nil) }
func (g *Gui) zoomOut() { g.zoomBy(-1, nil) }
func (g *Gui) zoomBy(delta int, point *fyne.Position) {
	g.openingFitDocument = nil
	if g.showingMarkdown() {
		if p := g.markdownView.imagePreview; p != nil {
			p.zoom(delta, nil)
			return
		}
		g.markdownView.zoom(delta)
		return
	}
	if g.showingModel() {
		factor := 1.25
		if delta < 0 {
			factor = .8
		}
		g.modelViewport.zoom(factor)
		return
	}
	if g.document == nil {
		return
	}
	level := max(minZoomLevel, min(maxZoomLevel, g.zoomLevel+delta))
	if level == g.zoomLevel {
		return
	}
	g.cropOverlay.Deactivate()
	g.zoomLevel = level
	g.updateZoomAt(point)
	g.setPanActive(g.panModifierDown)
}

func (g *Gui) resetZoom() {
	g.openingFitDocument = nil
	if g.showingMarkdown() {
		if p := g.markdownView.imagePreview; p != nil {
			p.fit()
			return
		}
		g.markdownView.scale = 1
		g.markdownView.rebuild()
		return
	}
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

// Opening a new document starts at its beginning; it has no previous view to
// anchor. Fit-to-window on an existing document still preserves its position.
func (g *Gui) fitNewDocument() {
	g.fitZoom = g.calculateFitZoom()
	g.zoomLevel = 0
	g.zoom = g.fitZoom
	g.applyZoom()
	g.setPanActive(false)
}

func (g *Gui) updateZoomFromLevel() { g.updateZoomAt(nil) }

// Preserve a point within its own page, so inter-page gaps do not get scaled
// and a later page does not drift toward the beginning of a long PDF.
func (g *Gui) updateZoomAt(point *fyne.Position) {
	scroll := g.imageScroll
	pdf := g.document != nil && g.document.Format() == document.FormatPDF
	if pdf {
		scroll = g.pdfScroll
	}
	if scroll == nil {
		g.zoom = g.fitZoom * float32(math.Pow(float64(zoomStep), float64(g.zoomLevel)))
		g.applyZoom()
		return
	}
	anchor := fyne.NewPos(scroll.Size().Width/2, scroll.Size().Height/2)
	if point != nil {
		anchor = *point
	}
	page := 0
	if pdf {
		y := scroll.Offset.Y + anchor.Y
		page, _ = g.visiblePDFPageRange(y, y+1)
		page = min(page, max(0, len(g.pdfPageContainers)-1))
	}
	origin, size := g.zoomPageRect(scroll, page, pdf)
	relative := fyne.NewPos((scroll.Offset.X+anchor.X-origin.X)/max(1, size.Width), (scroll.Offset.Y+anchor.Y-origin.Y)/max(1, size.Height))
	onScrolled := scroll.OnScrolled
	scroll.OnScrolled = nil
	g.zoom = g.fitZoom * float32(math.Pow(float64(zoomStep), float64(g.zoomLevel)))
	g.layoutZoom()
	scroll.Refresh()
	origin, size = g.zoomPageRect(scroll, page, pdf)
	offset := fyne.NewPos(origin.X+relative.X*size.Width-anchor.X, origin.Y+relative.Y*size.Height-anchor.Y)
	scroll.ScrollToOffset(offset)
	scroll.OnScrolled = onScrolled
	if pdf {
		g.loadVisiblePDFPages()
	}
}

func (g *Gui) zoomPageRect(scroll *container.Scroll, page int, pdf bool) (fyne.Position, fyne.Size) {
	if pdf && page < len(g.pdfPageImages) {
		size := g.pdfPageImages[page].MinSize()
		width := max(scroll.Size().Width, g.pdfPages.MinSize().Width)
		return fyne.NewPos((width-size.Width)/2, g.pdfPageTop(page)), size
	}
	if g.imageSignatureLayer != nil {
		origin, size := g.imageSignatureLayer.displayRect()
		return origin.Add(g.imageSignatureLayer.Position()), size
	}
	return fyne.Position{}, scroll.Content.MinSize()
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
	g.layoutZoom()
	if g.document != nil && g.document.Format() == document.FormatPDF {
		g.loadVisiblePDFPages()
	} else if g.imageScroll != nil {
		g.imageScroll.Refresh()
	}
}

func (g *Gui) layoutZoom() {
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
		g.updatePDFPageTops()
		g.pdfPages.Refresh()
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
