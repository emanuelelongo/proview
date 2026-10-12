package gui

import (
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"image"
	"time"
)

// Each layer coalesces drag updates and rejects obsolete rendering results.
func (g *Gui) scheduleLensPreviews(layer *signatureLayer) {
	if layer == nil || g.document == nil {
		return
	}
	found := false
	for _, o := range g.document.Overlays() {
		if o.Page == layer.page && o.IsLens() {
			found = true
			break
		}
	}
	if !found {
		return
	}
	layer.lensGeneration++
	if layer.lensTimer != nil || layer.lensRunning {
		return
	}
	layer.lensTimer = time.AfterFunc(60*time.Millisecond, func() {
		fyne.Do(func() {
			layer.lensTimer = nil
			if g.closed || g.document == nil || layer.document != g.document {
				return
			}
			doc := g.document
			snapshot := doc.RenderingSnapshot()
			gen := layer.lensGeneration
			revision := doc.Revision()
			layer.lensRunning = true
			page := layer.page
			go func() {
				previews := map[document.OverlayID]image.Image{}
				for _, o := range snapshot.Overlays() {
					if o.Page == page && o.IsLens() {
						p, e := snapshot.RenderLens(o.ID, 144)
						if e == nil {
							previews[o.ID] = p
						}
					}
				}
				fyne.Do(func() {
					layer.lensRunning = false
					if g.closed || g.document != doc {
						return
					}
					if layer.lensGeneration != gen || doc.Revision() != revision {
						g.scheduleLensPreviews(layer)
						return
					}
					for _, w := range layer.widgets {
						if p := previews[w.id]; p != nil {
							w.preview = p
							w.image.Image = p
							w.image.Refresh()
						}
					}
				})
			}()
		})
	})
}
