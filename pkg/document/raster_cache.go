package document

import (
	"image"
	"sync/atomic"
)

// Source rasters are disposable. History, export and page dimensions retain the
// immutable PDF bytes and page references, never depend on raster residency.
//
// The per-source limit is capped, while the shared limit is divided between all
// live PDF sources. This keeps one long document well cached without letting a
// workspace full of sources multiply the same budget.
const (
	maxSourceRasterBytes int64 = 64 << 20
	minSourceRasterBytes int64 = 16 << 20
	maxSharedRasterBytes int64 = 128 << 20
)

var rasterSourceCount atomic.Int64

// sourceRasterLimit derives the current per-source share. It is approximate:
// limits only shrink when another source registers, and existing caches are
// trimmed on their next raster access.
func sourceRasterLimit() int64 {
	count := rasterSourceCount.Load()
	if count < 1 {
		count = 1
	}
	shared := maxSharedRasterBytes / count
	return min(maxSourceRasterBytes, max(minSourceRasterBytes, shared))
}

func (s *Source) registerRasterSource() {
	if s == nil || s.format != FormatPDF {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerRasterSourceLocked()
}

func (s *Source) registerRasterSourceLocked() {
	if s.rasterRegistered {
		return
	}
	s.rasterRegistered = true
	rasterSourceCount.Add(1)
}

func (s *Source) unregisterRasterSourceLocked() {
	if !s.rasterRegistered {
		return
	}
	s.rasterRegistered = false
	rasterSourceCount.Add(-1)
}

func rasterBytes(p image.Image) int64 {
	if p == nil {
		return 0
	}
	switch p := p.(type) {
	case *image.RGBA:
		return int64(len(p.Pix))
	case *image.NRGBA:
		return int64(len(p.Pix))
	default:
		return int64(p.Bounds().Dx()) * int64(p.Bounds().Dy()) * 8
	}
}

// Called with source.mu held. Source image documents are never evicted.
func (s *Source) touchRaster(page int, thumbnail bool) {
	if s.format != FormatPDF {
		return
	}
	s.rasterClock++
	if thumbnail {
		s.pages[page].thumbnailUsed = s.rasterClock
	} else {
		s.pages[page].previewUsed = s.rasterClock
	}
	s.trimRasters(sourceRasterLimit())
}

func (s *Source) trimRasters(limit int64) {
	for s.rasterBytes.Load() > limit {
		oldest, index, thumbnail := ^uint64(0), -1, false
		for i, page := range s.pages {
			if page.preview != nil && page.previewUsed < oldest {
				oldest, index, thumbnail = page.previewUsed, i, false
			}
			if page.thumbnail != nil && page.thumbnailUsed < oldest {
				oldest, index, thumbnail = page.thumbnailUsed, i, true
			}
		}
		if index < 0 {
			break
		}
		page := &s.pages[index]
		if thumbnail {
			s.rasterBytes.Add(-rasterBytes(page.thumbnail))
			page.thumbnail = nil
		} else {
			s.rasterBytes.Add(-rasterBytes(page.preview))
			page.preview = nil
		}
	}
}

// TrimPreviews releases cached full-size page previews while retaining
// thumbnails. Inactive image and GIF documents release their decoded raster and
// animation; both are rebuilt lazily from the immutable encoded bytes.
func (s *Source) TrimPreviews() {
	if s == nil {
		return
	}
	s.mu.Lock()
	for i := range s.pages {
		if s.pages[i].preview != nil {
			s.rasterBytes.Add(-rasterBytes(s.pages[i].preview))
			s.pages[i].preview = nil
		}
	}
	if s.rasterBytes.Load() < 0 {
		s.rasterBytes.Store(0)
	}
	s.mu.Unlock()
	s.trimAnimation()
	if s.annotationBase != nil {
		s.annotationBase.TrimPreviews()
	}
}

// TrimPagePreview releases the cached full-size preview for a single page.
func (s *Source) TrimPagePreview(page int) {
	if s == nil || s.format != FormatPDF {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if page < 0 || page >= len(s.pages) {
		return
	}
	if s.pages[page].preview != nil {
		s.rasterBytes.Add(-rasterBytes(s.pages[page].preview))
		s.pages[page].preview = nil
	}
	if s.rasterBytes.Load() < 0 {
		s.rasterBytes.Store(0)
	}
}

// rasterByteCount reports retained preview/thumbnail bytes for diagnostics
// without waiting for a native render that holds source.mu.
func (s *Source) rasterByteCount() int64 {
	if s == nil {
		return 0
	}
	return s.rasterBytes.Load()
}
