package gui

import (
	"sort"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2/theme"
)

func (g *Gui) updatePDFPageTops() {
	g.pdfPageTops = make([]float32, len(g.pdfPageContainers)+1)
	for i, page := range g.pdfPageContainers {
		g.pdfPageTops[i+1] = g.pdfPageTops[i] + page.MinSize().Height + theme.Padding()
	}
}

func (g *Gui) visiblePDFPageRange(top, bottom float32) (int, int) {
	count := len(g.pdfPageContainers)
	if len(g.pdfPageTops) != count+1 {
		g.updatePDFPageTops()
	}
	first := sort.Search(count, func(i int) bool { return g.pdfPageTops[i+1]-theme.Padding() > top })
	end := sort.Search(count, func(i int) bool { return g.pdfPageTops[i] >= bottom })
	return first, max(first, end)
}

func (g *Gui) pdfPagePinned(page int) bool {
	if g.hasSelectedSignature && g.selectedSignaturePage == page || g.selectedRedaction != 0 && g.selectedRedactionPage == page {
		return true
	}
	if g.textMove != nil && g.textMove.layer.page == page {
		return true
	}
	if g.textPlacement != nil && g.textPlacement.layer.page == page {
		return true
	}
	layer := g.signatureLayers[page]
	if layer == nil {
		return false
	}
	if g.textSelection != nil && g.textSelection.layer == layer || g.areaSelection != nil && g.areaSelection.layer == layer {
		return true
	}
	if layer.textGesture || layer.input != nil && (layer.input.active || len(layer.input.pending) > 0) || layer.highlightInput != nil && (layer.highlightInput.active || layer.highlightInput.pending) || layer.redactInput != nil && (layer.redactInput.active || layer.redactInput.pending) {
		return true
	}
	for _, object := range layer.widgets {
		if object.editActive {
			return true
		}
	}
	if forms := g.formLayers[page]; forms != nil && g.window != nil {
		for _, control := range forms.controls {
			if any(control) == any(g.window.Canvas().Focused()) {
				return true
			}
		}
	}
	return false
}

func (g *Gui) pdfPageWanted(page int) bool {
	return g.pdfKeepEnd == 0 || page >= g.pdfKeepStart && page < g.pdfKeepEnd || g.pdfPagePinned(page)
}

func (g *Gui) retainPDFPages(first, end int) {
	g.pdfKeepStart, g.pdfKeepEnd = max(0, first-2), min(len(g.pdfPageImages), end+2)
	g.cancelDistantPDFLoads()
	for page := range g.pdfResident {
		if g.pdfPageWanted(page) {
			continue
		}
		g.pdfPageImages[page].Image = nil
		g.pdfPageImages[page].Refresh()
		g.pdfPageLoaded[page] = false
		delete(g.pdfResident, page)
		delete(g.pdfBaseKeys, page)
		if layer := g.signatureLayers[page]; layer != nil {
			layer.Hide()
			layer.widgets, layer.redactionWidgets = nil, nil
			layer.highlightPreviews = nil
			layer.textHighlight, layer.searchHighlight = nil, nil
			if p := layer.highlightInput; p != nil {
				p.geometry = nil
				p.key = ""
			}
			if p := layer.redactInput; p != nil {
				p.geometry = nil
			}
		}
	}
}

func (g *Gui) pdfThumbWanted(page int) bool {
	return g.pdfThumbKeepEnd == 0 || page >= g.pdfThumbKeepStart && page < g.pdfThumbKeepEnd
}

func (g *Gui) retainPDFThumbnails(first, end int) {
	g.pdfThumbKeepStart, g.pdfThumbKeepEnd = max(0, first-2), min(len(g.pdfThumbImages), end+2)
	g.cancelDistantPDFLoads()
	for page := range g.pdfThumbResident {
		if g.pdfThumbWanted(page) {
			continue
		}
		g.pdfThumbImages[page].Image = nil
		g.pdfThumbImages[page].Refresh()
		g.pdfThumbLoaded[page] = false
		delete(g.pdfThumbResident, page)
		if page < len(g.pdfThumbPlaceholders) {
			g.pdfThumbPlaceholders[page].Show()
		}
	}
}

func (g *Gui) retainPDFPageImage(page int) {
	if g.pdfResident == nil {
		g.pdfResident = map[int]bool{}
	}
	if g.pdfBaseKeys == nil {
		g.pdfBaseKeys = map[int]document.PageBaseKey{}
	}
	g.pdfResident[page] = true
	g.pdfBaseKeys[page] = g.document.BasePageKey(page)
}

func (g *Gui) retainPDFThumbnailImage(page int) {
	if g.pdfThumbResident == nil {
		g.pdfThumbResident = map[int]bool{}
	}
	g.pdfThumbResident[page] = true
}
