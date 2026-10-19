package document

import (
	"image"
	"maps"
	"sync"
)

// PreviewRenderer retains one base page and one safe lens sampling page across
// immutable rendering snapshots. It is bounded independently of document length.
// Callers must keep the snapshots' sources alive for the renderer's lifetime.
type PreviewRenderer struct {
	baseMu, lensMu, highlightMu sync.Mutex
	base, lens                  previewPage
	highlights                  previewPage
	fragments                   map[OverlayID]image.Image
}

type previewPage struct {
	doc     *Document
	page    int
	dpi     float64
	image   image.Image
	content PageBaseKey
}

func (p *previewPage) matches(d *Document, page int, dpi float64) bool {
	return p.doc != nil && p.page == page && p.dpi == dpi && p.doc.source == d.source && d.stateEquals(p.doc.stateView(), false)
}

func (p *previewPage) render(d *Document, page int, dpi float64, safe bool) (image.Image, error) {
	if p.matches(d, page, dpi) {
		return p.image, nil
	}
	var picture image.Image
	var err error
	if safe {
		picture, err = d.SafeRenderPage(page, dpi)
	} else {
		picture, err = d.RenderBasePageDPI(page, dpi)
	}
	if err == nil {
		*p = previewPage{doc: d.RenderingSnapshot(), page: page, dpi: dpi, image: picture}
	}
	return picture, err
}

// RenderLens reuses sampling pixels when only a lens's geometry changes. The
// cache key includes earlier objects, forms, transforms and all redactions.
func (r *PreviewRenderer) RenderLens(d *Document, id OverlayID, dpi float64) (image.Image, error) {
	r.lensMu.Lock()
	defer r.lensMu.Unlock()
	return d.renderLens(id, dpi, r)
}

// HighlightPreviews reuses the source raster while recomposing objects in order.
func (r *PreviewRenderer) HighlightPreviews(d *Document, page int, dpi float64) (map[OverlayID]image.Image, error) {
	r.highlightMu.Lock()
	defer r.highlightMu.Unlock()
	// Only objects through the final highlight can affect these fragments.
	// In particular, dragging a later lens must not recompose any highlights.
	prefix := d.RenderingSnapshot()
	prefix.signatures = prefix.signatures[:0]
	last := 0
	for _, overlay := range d.signatures {
		if overlay.Page == page {
			prefix.signatures = append(prefix.signatures, overlay)
			if overlay.IsHighlight() {
				last = len(prefix.signatures)
			}
		}
	}
	prefix.signatures = prefix.signatures[:last]
	if r.highlights.matches(prefix, page, dpi) {
		return maps.Clone(r.fragments), nil
	}
	base, err := r.RenderBasePageDPI(d, page, dpi)
	if err != nil {
		return nil, err
	}
	previews := map[OverlayID]image.Image{}
	_, err = prefix.composePage(page, dpi, true, previews, base)
	if err == nil {
		r.highlights = previewPage{doc: prefix, page: page, dpi: dpi}
		r.fragments = maps.Clone(previews)
	}
	return previews, err
}

// RenderBasePageDPI shares the source raster with highlight composition, so an
// overlay edit does not trigger another native render of the enhanced page.
func (r *PreviewRenderer) RenderBasePageDPI(d *Document, page int, dpi float64) (image.Image, error) {
	r.baseMu.Lock()
	defer r.baseMu.Unlock()
	return r.renderBase(d, page, dpi)
}

func (r *PreviewRenderer) renderBase(d *Document, page int, dpi float64) (image.Image, error) {
	if d.Format() == FormatPDF {
		key := d.BasePageKey(page)
		if r.base.image != nil && r.base.page == page && r.base.dpi == dpi && r.base.content == key {
			return r.base.image, nil
		}
		picture, err := d.RenderBasePageDPI(page, dpi)
		if err == nil {
			r.base = previewPage{page: page, dpi: dpi, image: picture, content: key}
		}
		return picture, err
	}
	baseDoc := d.RenderingSnapshot()
	baseDoc.signatures = nil
	baseDoc.redactions = nil
	return r.base.render(baseDoc, page, dpi, false)
}
