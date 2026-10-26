package gui

import (
	"context"
	"errors"
	"image"

	"fyne.io/fyne/v2"
	"github.com/gen2brain/go-fitz"
)

// Render a compact opening excerpt independently of the live reading viewport.
// The retained entry owns the job, so switching documents does not cancel it.
func (g *Gui) ensureMarkdownThumbnail(entry *editorDocument) {
	if g.closed || entry.markdown == nil || entry.thumbnail != nil || entry.thumbnailCancel != nil || entry.thumbnailFailed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	entry.thumbnailCancel = cancel
	doc := entry.markdown
	go func() {
		var picture image.Image
		preview, err := doc.PreviewPDF(ctx)
		if err == nil && ctx.Err() == nil {
			var renderer *fitz.Document
			renderer, err = fitz.NewFromMemory(preview.Data)
			if err == nil {
				picture, err = renderer.ImageDPI(0, 72)
				renderer.Close()
			}
		}
		fyne.Do(func() {
			defer cancel()
			entry.thumbnailCancel = nil
			if g.closed || ctx.Err() != nil || entry.markdown != doc {
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
					g.logEvent("markdown_thumbnail_failed", "level=WARN error=%q", err)
				}
			} else {
				entry.thumbnail = picture
			}
			g.refreshDocumentSidebar()
		})
	}()
}
