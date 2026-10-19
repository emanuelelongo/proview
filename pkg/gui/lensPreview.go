package gui

import (
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"image"
	"math"
	"time"
)

// Each layer coalesces drag updates and rejects obsolete rendering results.
func (g *Gui) scheduleLensPreviews(layer *signatureLayer) {
	if layer == nil || g.document == nil {
		return
	}
	if g.document.Format() == document.FormatPDF && layer.page < len(g.pdfPageLoaded) && !g.pdfPageLoaded[layer.page] {
		return
	}
	found := false
	for _, o := range g.document.Overlays() {
		if o.Page == layer.page && (o.IsLens() || o.IsHighlight()) {
			found = true
			break
		}
	}
	if !found {
		return
	}
	if g.thumbnailRefresh != nil {
		g.thumbnailRefresh.after = time.Now().Add(thumbnailRefreshDelay)
	}
	layer.lensGeneration++
	if layer.lensTimer != nil || layer.lensRunning {
		return
	}
	layer.lensTimer = time.AfterFunc(time.Second/60, func() {
		fyne.Do(func() {
			layer.lensTimer = nil
			g.renderLensPreviews(layer)
		})
	})
}

// A single running frame owns the worker. New motion replaces the pending
// request; obsolete work immediately starts that latest request, without another
// debounce delay. Never queue a render for every pointer event.
func (g *Gui) renderLensPreviews(layer *signatureLayer) {
	if g.closed || g.document == nil || layer.document != g.document || layer.lensRunning {
		return
	}
	if g.previewRenderer == nil {
		g.previewRenderer = &document.PreviewRenderer{}
	}
	renderer := g.previewRenderer

	doc := g.document
	snapshot := doc.RenderingSnapshot()
	gen := layer.lensGeneration
	revision := doc.Revision()
	layer.lensRunning = true
	page := layer.page
	lensDPI := g.lensPreviewDPI()
	highlightDPI := g.pdfOverlayPreviewDPI(page)
	go func() {
		previews := map[document.OverlayID]image.Image{}
		var highlightErr error
		hasHighlights := false
		for _, o := range snapshot.Overlays() {
			if o.Page == page && o.IsHighlight() {
				hasHighlights = true
				break
			}
		}
		if hasHighlights {
			previews, highlightErr = renderer.HighlightPreviews(snapshot, page, highlightDPI)
			if previews == nil {
				previews = map[document.OverlayID]image.Image{}
			}
		}
		for _, o := range snapshot.Overlays() {
			if o.Page == page && o.IsLens() {
				p, e := renderer.RenderLens(snapshot, o.ID, lensDPI)
				if e == nil {
					previews[o.ID] = p
				}
			}
		}
		fyne.Do(func() {
			layer.lensRunning = false
			if g.closed || g.document != doc || layer.document != doc || layer.page != page {
				if !g.closed && layer.document == g.document {
					g.renderLensPreviews(layer)
				}
				return
			}
			if layer.lensGeneration != gen || doc.Revision() != revision {
				g.renderLensPreviews(layer)
				return
			}
			if highlightErr != nil {
				g.logEvent("highlight_preview_failed", "level=WARN page=%d error=%q", page+1, highlightErr)
			}
			for _, w := range layer.widgets {
				if p := previews[w.id]; p != nil && g.annotationPreviewID != w.id && w.preview != p {
					if w.annotation != nil && w.annotation.Kind == annotations.Highlight {
						if layer.highlightPreviews == nil {
							layer.highlightPreviews = map[document.OverlayID]image.Image{}
						}
						layer.highlightPreviews[w.id] = p
					}
					w.preview = p
					w.image.Image = p
					w.image.Show()
					w.image.Refresh()
				}
			}
		})
	}()
}

// Match screen density without increasing the ordinary page/highlight previews.
func (g *Gui) lensPreviewDPI() float64 {
	scale := 1.
	if g.window != nil {
		scale = float64(g.window.Canvas().Scale())
	}
	return math.Min(600, 144*math.Max(1, float64(g.zoom)*scale))
}

func (g *Gui) pdfOverlayPreviewDPI(page int) float64 {
	s := g.pdfDetail
	if s != nil && s.image != nil && s.rendered.doc == g.document && s.rendered.generation == g.pdfGeneration && s.rendered.page == page {
		return s.rendered.dpi
	}
	return 144
}
