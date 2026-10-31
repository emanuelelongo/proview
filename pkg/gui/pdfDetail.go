package gui

import (
	"context"
	"image"
	"math"
	"time"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
)

type pdfDetailKey struct {
	doc        *document.Document
	generation uint64
	content    document.PageBaseKey
	page       int
	dpi        float64
}

// Keep one enhanced page per window. Ordinary 144-DPI previews remain the
// fallback; rapid zoom/scroll changes replace one pending request.
type pdfDetailState struct {
	target, rendered, failed pdfDetailKey
	image, fallback          image.Image
	timer                    *time.Timer
	cancel                   context.CancelFunc
	running                  bool
}

func pdfDisplayDPI(size image.Point, zoom, scale float64) float64 {
	if size.X <= 0 || size.Y <= 0 {
		return 144
	}
	density := math.Max(1, zoom*scale)
	// Small density buckets avoid rendering again for tiny window resizes.
	dpi := math.Ceil(144*density/24) * 24
	limit := math.Min(600, math.Min(144*math.Sqrt(16_000_000/(float64(size.X)*float64(size.Y))), 144*8192/float64(max(size.X, size.Y))))
	return math.Max(144, math.Min(dpi, limit))
}

func (g *Gui) releasePDFDetail(s *pdfDetailState) {
	page := s.rendered.page
	if s.image != nil && s.rendered.doc == g.document && s.rendered.generation == g.pdfGeneration && page >= 0 && page < len(g.pdfPageImages) && g.pdfPageImages[page].Image == s.image {
		g.pdfPageImages[page].Image = s.fallback
		g.pdfPageImages[page].Refresh()
	}
	s.image, s.fallback = nil, nil
	g.previewRenderer = nil
	s.rendered = pdfDetailKey{}
}

func (g *Gui) clearPDFDetail() {
	g.clearPDFLoads()
	g.clearPDFBasePreviews()
	g.clearThumbnailRefresh()
	g.previewRenderer = nil
	if s := g.pdfDetail; s != nil {
		if s.timer != nil {
			s.timer.Stop()
		}
		if s.cancel != nil {
			s.cancel()
		}
		g.releasePDFDetail(s)
	}
	g.pdfDetail = nil
}

func (g *Gui) refreshPDFDetail() {
	if g.closed || g.document == nil || g.document.Format() != document.FormatPDF {
		return
	}
	page := g.activePDFPage
	if page < 0 || page >= len(g.pdfPageLoaded) || !g.pdfPageLoaded[page] {
		return
	}
	scale := 1.
	if g.window != nil {
		scale = float64(g.window.Canvas().Scale())
	}
	key := pdfDetailKey{doc: g.document, generation: g.pdfGeneration, content: g.document.BasePageKey(page), page: page, dpi: pdfDisplayDPI(g.pdfPageSizes[page], float64(g.zoom), scale)}
	if g.pdfDetail == nil {
		g.pdfDetail = &pdfDetailState{}
	}
	s := g.pdfDetail
	if s.image != nil && (s.rendered.doc != key.doc || s.rendered.generation != key.generation || s.rendered.page != key.page || key.dpi <= 144) {
		g.releasePDFDetail(s)
	}
	s.target = key
	if key.dpi <= 144 || s.rendered == key && g.pdfPageImages[page].Image == s.image || s.failed == key || s.running || s.timer != nil {
		return
	}
	s.timer = time.AfterFunc(100*time.Millisecond, func() {
		fyne.Do(func() {
			s.timer = nil
			if g.closed || g.pdfDetail != s {
				return
			}
			g.renderPDFDetail(s)
		})
	})
}

func (g *Gui) renderPDFDetail(s *pdfDetailState) {
	key := s.target
	if key.dpi <= 144 || g.document != key.doc || g.pdfGeneration != key.generation {
		return
	}
	snapshot := key.doc.RenderingSnapshot()
	if g.previewRenderer == nil {
		g.previewRenderer = &document.PreviewRenderer{}
	}
	renderer := g.previewRenderer
	s.running = true
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	slots := g.pdfRenderSlots()
	go func() {
		var picture image.Image
		err := withPDFRenderSlot(ctx, slots, func() error {
			var renderErr error
			picture, renderErr = renderer.RenderBasePageDPI(snapshot, key.page, key.dpi)
			return renderErr
		})
		fyne.Do(func() {
			cancel()
			s.cancel = nil
			s.running = false
			if g.closed || g.pdfDetail != s {
				return
			}
			if g.document != key.doc || g.pdfGeneration != key.generation || key.doc.BasePageKey(key.page) != key.content || s.target != key {
				g.refreshPDFDetail()
				return
			}
			if err != nil {
				s.failed = key
				g.logEvent("pdf_detail_failed", "level=WARN page=%d dpi=%.0f error=%q", key.page+1, key.dpi, err)
				return
			}
			if s.image == nil || g.pdfPageImages[key.page].Image != s.image {
				s.fallback = g.pdfPageImages[key.page].Image
			}
			s.image, s.rendered = picture, key
			g.pdfPageImages[key.page].Image = picture
			g.pdfPageImages[key.page].Refresh()
			// Only pixels change: keep page sizes, scroll offsets and overlay geometry.
			g.scheduleLensPreviews(g.signatureLayers[key.page])
		})
	}()
}
