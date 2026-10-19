package gui

import (
	"time"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
)

const thumbnailRefreshDelay = 2 * time.Second

// Edited thumbnails are deliberately eventual. Keep one pending request per
// page, one worker per window, and leave editor rendering outside this queue.
type thumbnailRefreshState struct {
	doc        *document.Document
	generation uint64
	pending    map[int]struct{}
	after      time.Time
	timer      *time.Timer
	running    bool
}

func (g *Gui) clearThumbnailRefresh() {
	if s := g.thumbnailRefresh; s != nil && s.timer != nil {
		s.timer.Stop()
	}
	g.thumbnailRefresh = nil
}

func (g *Gui) refreshPagePreview(page int) {
	if g.closed || g.document == nil || g.document.Format() != document.FormatPDF || page < 0 || page >= len(g.pdfThumbImages) || !g.pdfThumbWanted(page) {
		return
	}
	// Pages never opened in the editor keep the lazy sidebar loading path.
	if page < len(g.pdfPageLoaded) && !g.pdfPageLoaded[page] {
		return
	}
	s := g.thumbnailRefresh
	if s == nil || s.doc != g.document || s.generation != g.pdfGeneration {
		g.clearThumbnailRefresh()
		s = &thumbnailRefreshState{doc: g.document, generation: g.pdfGeneration, pending: map[int]struct{}{}}
		g.thumbnailRefresh = s
	}
	s.pending[page] = struct{}{}
	s.after = time.Now().Add(thumbnailRefreshDelay)
	g.armThumbnailRefresh(s)
}

func (g *Gui) armThumbnailRefresh(s *thumbnailRefreshState) {
	if s.running || s.timer != nil || len(s.pending) == 0 {
		return
	}
	s.timer = time.AfterFunc(max(0, time.Until(s.after)), func() {
		fyne.Do(func() {
			s.timer = nil
			g.renderThumbnailRefresh(s)
		})
	})
}

func (g *Gui) thumbnailEditsActive() bool {
	if g.working || g.annotationPending > 0 || g.annotationPreviewID != 0 || g.textPlacement != nil || g.textMove != nil {
		return true
	}
	for _, layer := range g.signatureLayers {
		if layer.textGesture || (layer.input != nil && layer.input.active) || (layer.highlightInput != nil && (layer.highlightInput.active || layer.highlightInput.pending)) || (layer.redactInput != nil && (layer.redactInput.active || layer.redactInput.pending)) {
			return true
		}
		for _, object := range layer.widgets {
			if object.editActive {
				return true
			}
		}
	}
	return false
}

func (g *Gui) renderThumbnailRefresh(s *thumbnailRefreshState) {
	if g.closed || g.thumbnailRefresh != s || g.document != s.doc || g.pdfGeneration != s.generation || s.running {
		return
	}
	if g.thumbnailEditsActive() {
		s.after = time.Now().Add(thumbnailRefreshDelay)
	}
	if time.Now().Before(s.after) {
		g.armThumbnailRefresh(s)
		return
	}
	page := -1
	for candidate := range s.pending {
		if !g.pdfThumbWanted(candidate) {
			delete(s.pending, candidate)
			continue
		}
		if page < 0 || candidate < page {
			page = candidate
		}
	}
	if page < 0 {
		return
	}
	delete(s.pending, page)
	snapshot, revision := s.doc.RenderingSnapshot(), s.doc.Revision()
	s.running = true
	go func() {
		preview, err := snapshot.RenderThumbnail(page)
		fyne.Do(func() {
			s.running = false
			if g.closed || g.thumbnailRefresh != s || g.document != s.doc || g.pdfGeneration != s.generation {
				return
			}
			if s.doc.Revision() != revision {
				s.pending[page] = struct{}{}
				s.after = time.Now().Add(thumbnailRefreshDelay)
			} else if err != nil {
				g.logEvent("pdf_thumbnail_failed", "level=WARN page=%d error=%q", page+1, err)
			} else if page < len(g.pdfThumbImages) && g.pdfThumbWanted(page) {
				g.pdfThumbImages[page].Image = preview
				g.pdfThumbImages[page].Refresh()
				g.pdfThumbLoaded[page] = true
				g.retainPDFThumbnailImage(page)
				g.hideThumbnailPlaceholder(page)
			}
			g.armThumbnailRefresh(s)
		})
	}()
}
