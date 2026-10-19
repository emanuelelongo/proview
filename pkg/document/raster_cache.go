package document

import "image"

// Source rasters are disposable. History, export and page dimensions retain the
// immutable PDF bytes and page references, never depend on raster residency.
const maxSourceRasterBytes int64 = 64 << 20

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
	s.trimRasters(maxSourceRasterBytes)
}

func (s *Source) trimRasters(limit int64) {
	for s.rasterBytes > limit {
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
			s.rasterBytes -= rasterBytes(page.thumbnail)
			page.thumbnail = nil
		} else {
			s.rasterBytes -= rasterBytes(page.preview)
			page.preview = nil
		}
	}
}
