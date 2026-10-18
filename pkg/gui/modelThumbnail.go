package gui

import (
	"context"
	"errors"

	"emanuelelongo/proview/internal/model3d"
	"fyne.io/fyne/v2"
)

// Generate once per retained model, independently of its live camera and renderer.
func (g *Gui) ensureModelThumbnail(entry *editorDocument) {
	if g.closed || entry.mesh == nil || entry.thumbnail != nil || entry.thumbnailCancel != nil || entry.thumbnailFailed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	entry.thumbnailCancel = cancel
	mesh := entry.mesh
	go func() {
		picture, err := model3d.RenderHighQuality(ctx, mesh, model3d.DefaultCamera(), 320, 240)
		fyne.Do(func() {
			defer cancel()
			entry.thumbnailCancel = nil
			if g.closed || ctx.Err() != nil || entry.mesh != mesh {
				return
			}
			retained := false
			for _, candidate := range g.documents {
				if candidate == entry {
					retained = true
					break
				}
			}
			if !retained {
				return
			}
			if err != nil {
				entry.thumbnailFailed = true
				if !errors.Is(err, context.Canceled) {
					g.logEvent("model_thumbnail_failed", "level=WARN error=%q", err)
				}
			} else {
				entry.thumbnail = picture
			}
			g.refreshDocumentSidebar()
		})
	}()
}
