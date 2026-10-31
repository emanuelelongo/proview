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
	case *image.RGBA64:
		return int64(len(p.Pix))
	case *image.NRGBA64:
		return int64(len(p.Pix))
	case *image.Gray:
		return int64(len(p.Pix))
	case *image.Gray16:
		return int64(len(p.Pix))
	case *image.Alpha:
		return int64(len(p.Pix))
	case *image.CMYK:
		return int64(len(p.Pix))
	case *image.Paletted:
		return int64(len(p.Pix)) + int64(len(p.Palette))*16
	case *image.YCbCr:
		// Decoded JPEGs keep subsampled chroma: often 1.5 bytes per pixel.
		return int64(len(p.Y) + len(p.Cb) + len(p.Cr))
	case *image.NYCbCrA:
		return int64(len(p.Y) + len(p.Cb) + len(p.Cr) + len(p.A))
	default:
		return int64(p.Bounds().Dx()) * int64(p.Bounds().Dy()) * 4
	}
}

// Called with source.mu held. Source image documents are never evicted.
func (s *Source) touchRaster(page int, thumbnail bool) {
	if s.format != FormatPDF {
		return
	}
	clock := s.rasterClock.Add(1)
	if thumbnail {
		s.pages[page].thumbnailUsed = clock
	} else {
		s.pages[page].previewUsed = clock
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
// It never waits for a native render: see trimLocked.
func (s *Source) TrimPreviews() {
	if s == nil {
		return
	}
	s.trimLocked(func(clock uint64) {
		for i := range s.pages {
			s.dropPreviewLocked(i, clock)
		}
	})
	if s.formRendererMu.TryLock() {
		if s.formRenderer != nil {
			s.formRenderer.Close()
			s.formRenderer = nil
			s.formRendererKey = 0
		}
		s.formRendererMu.Unlock()
	}
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
	s.trimLocked(func(clock uint64) {
		if page >= 0 && page < len(s.pages) {
			s.dropPreviewLocked(page, clock)
		}
	})
}

// trimLocked runs trim with source.mu held. Callers are UI paths (scroll and
// document switch), and source.mu is held for a whole MuPDF render, so a busy
// source queues the trim to one background worker instead of blocking. A
// queued trim keeps previews used after it was requested.
func (s *Source) trimLocked(trim func(clock uint64)) {
	if s.mu.TryLock() {
		trim(^uint64(0))
		s.mu.Unlock()
		return
	}
	clock := s.rasterClock.Load()
	s.trimMu.Lock()
	s.trimQueue = append(s.trimQueue, func() { trim(clock) })
	if s.trimRunning {
		s.trimMu.Unlock()
		return
	}
	s.trimRunning = true
	s.trimMu.Unlock()
	go s.runQueuedTrims()
}

func (s *Source) runQueuedTrims() {
	for {
		s.trimMu.Lock()
		queue := s.trimQueue
		s.trimQueue = nil
		if len(queue) == 0 {
			s.trimRunning = false
			s.trimMu.Unlock()
			return
		}
		s.trimMu.Unlock()
		s.mu.Lock()
		for _, trim := range queue {
			trim()
		}
		s.mu.Unlock()
	}
}

// dropPreviewLocked must be called with s.mu held.
func (s *Source) dropPreviewLocked(page int, clock uint64) {
	if s.pages[page].preview == nil || s.pages[page].previewUsed > clock {
		return
	}
	s.rasterBytes.Add(-rasterBytes(s.pages[page].preview))
	s.pages[page].preview = nil
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
