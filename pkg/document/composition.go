package document

import (
	"bytes"
	"emanuelelongo/proview/internal/pdfengine"
	"emanuelelongo/proview/pkg/annotations"
	img "emanuelelongo/proview/pkg/images"
	"fmt"
	xdraw "golang.org/x/image/draw"
	"image"
	"image/color"
	"image/draw"
	"math"
)

func (s SignatureOverlay) IsHighlight() bool {
	return s.annotation != nil && s.annotation.Kind == annotations.Highlight
}
func (s SignatureOverlay) IsLens() bool {
	return s.annotation != nil && s.annotation.Kind == annotations.Shape && s.annotation.Shape == annotations.Magnifier
}
func (s SignatureOverlay) ArtworkAsset(aspect float64) (signaturesAsset []byte, err error) {
	if len(s.pdfAppearance) > 0 {
		raw, err := normalizedAppearance(s.pdfAppearance, s.Transform)
		if err == nil && s.IsHighlight() {
			return multiplyArtworkPDF(raw)
		}
		return raw, err
	}
	base := s.asset
	if base == nil {
		fallback := s.Asset()
		base = &fallback
	}
	a, e := base.Transformed(s.Transform)
	if e != nil {
		return nil, e
	}
	a, e = a.WithAspect(aspect)
	if e != nil {
		return nil, e
	}
	raw, err := a.PDF()
	if err == nil && s.IsHighlight() {
		return multiplyArtworkPDF(raw)
	}
	return raw, err
}

func (d *Document) RenderPage(page int) (image.Image, error) { return d.renderPageDPI(page, 144, true) }

// RenderBasePageDPI renders source content and form values without editor overlays.
// Pixel density never changes document coordinates or the ordinary preview cache.
func (d *Document) RenderBasePageDPI(page int, dpi float64) (image.Image, error) {
	if err := d.validatePage(page); err != nil {
		return nil, err
	}
	if math.IsNaN(dpi) || math.IsInf(dpi, 0) || dpi <= 0 || dpi > 600 {
		return nil, fmt.Errorf("Invalid preview resolution")
	}
	return d.basePageDPI(page, dpi)
}
func (d *Document) basePageDPI(page int, dpi float64) (image.Image, error) {
	if d.Format() != FormatPDF {
		return d.RenderBasePage(page)
	}
	source, index, e := d.pageSource(page)
	if e != nil {
		return nil, e
	}

	if dpi == 144 {
		return d.Preview(page)
	}
	preview, changed, e := d.formPreview(source, index, dpi)
	if !changed {
		source.mu.Lock()
		if source.pdf == nil {
			source.mu.Unlock()
			return nil, fmt.Errorf("The PDF is closed")
		}
		if source.hasPDFAnnotations {
			preview, e = renderPDFPage(source.original, index, dpi)
		} else {
			preview, e = source.pdf.ImageDPI(index, dpi)
		}
		source.mu.Unlock()
	}
	if e != nil {
		return nil, e
	}
	return (img.Transform{Turns: d.pages[page].turns}).Apply(preview), nil
}

func (d *Document) renderPageDPI(page int, dpi float64, lenses bool) (image.Image, error) {
	return d.renderPageWithHighlights(page, dpi, lenses, nil)
}

// HighlightPreviews composes a page once, collecting only highlight fragments.
// Call on an immutable snapshot in the preview worker.
func (d *Document) HighlightPreviews(page int, dpi float64) (map[OverlayID]image.Image, error) {
	previews := map[OverlayID]image.Image{}
	_, err := d.renderPageWithHighlights(page, dpi, true, previews)
	return previews, err
}
func (d *Document) renderPageWithHighlights(page int, dpi float64, lenses bool, previews map[OverlayID]image.Image) (image.Image, error) {
	base, e := d.basePageDPI(page, dpi)
	if e != nil {
		return nil, e
	}
	var out *image.NRGBA
	for _, s := range d.signatures {
		if s.Page != page {
			continue
		}
		if s.IsLens() && !lenses {
			continue
		}
		target := normalizedImageRect(s.Rect, base.Bounds())
		if target.Empty() {
			continue
		}
		var p image.Image
		if s.IsLens() {
			p, e = d.RenderLens(s.ID, dpi)
		} else if len(s.pdfAppearance) > 0 {
			if dpi <= 144 && s.Transform.Turns == 0 && !s.Transform.FlipX && !s.Transform.FlipY {
				p = s.preview
			} else {
				raw, err := s.ArtworkAsset(0)
				if err != nil {
					return nil, err
				}
				p, e = pdfengine.RenderFormPage(raw, 0, dpi)
			}
		} else if s.IsVector() {
			a, err := s.asset.Transformed(s.Transform)
			if err != nil {
				return nil, err
			}
			p, e = a.Rasterize(target.Dx(), target.Dy())
		} else {
			p = s.Preview()
		}
		if e != nil {
			return nil, e
		}
		if out == nil {
			out = image.NewNRGBA(base.Bounds())
			draw.Draw(out, out.Bounds(), base, base.Bounds().Min, draw.Src)
			base = out
		}
		if s.IsHighlight() {
			p = MultiplyHighlight(out, p, target)
			if previews != nil {
				previews[s.ID] = p
			}
		}
		// Vector artwork is already rendered at its target size; raster images stretch only to the object's rectangle.
		if p.Bounds().Dx() == target.Dx() && p.Bounds().Dy() == target.Dy() {
			draw.Draw(out, target, p, p.Bounds().Min, draw.Over)
		} else {
			xdraw.BiLinear.Scale(out, target, p, p.Bounds(), draw.Over, nil)
		}
	}
	return d.paintRedactions(page, base), nil
}

// SafeRenderPage uses actual removal, including shared image reuse, before a
// raster derivative can be copied or magnified. Hover reveal is never involved.
func (d *Document) SafeRenderPage(page int, dpi float64) (image.Image, error) {
	if d.Format() != FormatPDF || !d.HasRedactions() {
		return d.renderPageDPI(page, dpi, true)
	}
	data, e := d.exportPDF()
	if e != nil {
		return nil, e
	}
	source, e := Load("redacted.pdf", bytes.NewReader(data))
	if e != nil {
		return nil, e
	}
	defer source.Close()
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.pdf.ImageDPI(page, dpi)
}

func (d *Document) RenderLens(id OverlayID, dpi float64) (image.Image, error) {
	i := d.signatureIndex(id)
	if i < 0 || !d.signatures[i].IsLens() {
		return nil, ErrSignatureNotFound
	}
	s := d.signatures[i]
	under := d.RenderingSnapshot()
	under.signatures = under.signatures[:0]
	for _, o := range d.signatures[:i] {
		if !o.IsLens() {
			under.signatures = append(under.signatures, o)
		}
	}
	zoom := s.annotation.Magnification
	sampleDPI := dpi
	if d.Format() == FormatPDF {
		size, err := d.PageSize(s.Page)
		if err != nil {
			return nil, err
		}
		sampleDPI = lensSamplingDPI(dpi, zoom, size)
	}
	base, e := under.SafeRenderPage(s.Page, sampleDPI)
	if e != nil {
		return nil, e
	}
	target := normalizedImageRect(s.Rect, base.Bounds())
	w, h := target.Dx(), target.Dy()
	if d.Format() == FormatPDF {
		// Sampling detail changes independently of the displayed lens dimensions.
		w, h = max(1, int(math.Round(float64(w)*dpi/sampleDPI))), max(1, int(math.Round(float64(h)*dpi/sampleDPI)))
	}
	if w < 1 || h < 1 || int64(w)*int64(h) > 128_000_000 {
		return nil, fmt.Errorf("The lens is too large")
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(target.Min.X)+float64(target.Dx())/2, float64(target.Min.Y)+float64(target.Dy())/2
	// A bounded sample is scaled once, then only visible disk pixels are retained.
	sample := image.Rect(int(math.Floor(cx-float64(target.Dx())/2/zoom)), int(math.Floor(cy-float64(target.Dy())/2/zoom)), int(math.Ceil(cx+float64(target.Dx())/2/zoom)), int(math.Ceil(cy+float64(target.Dy())/2/zoom)))
	sampled := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(sampled, sampled.Bounds(), base, sample, draw.Src, nil)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			distance := math.Hypot((float64(x)+.5-float64(w)/2)/float64(w/2), (float64(y)+.5-float64(h)/2)/float64(h/2))
			coverage := math.Min(1, math.Max(0, (1-distance)*float64(min(w, h))/2))
			if coverage == 0 {
				continue
			}
			c := sampled.NRGBAAt(x, y)
			if (1-distance)*float64(min(w, h))/2 < 1.5 {
				c = color.NRGBA{A: 255}
			}
			c.A = uint8(float64(c.A) * coverage)
			out.SetNRGBA(x, y, c)
		}
	}
	return s.Transform.Apply(out), nil
}

// Enlarge vector text before sampling, bounded to 16 million source pixels and
// the native renderer's 600-DPI limit. Image sources keep their original detail.
func lensSamplingDPI(dpi, zoom float64, size image.Point) float64 {
	const maxPixels = 16_000_000
	pixels := float64(size.X) * float64(size.Y)
	if pixels <= 0 {
		return dpi
	}
	limit := math.Min(600, math.Min(144*math.Sqrt(maxPixels/pixels), 144*8192/float64(max(size.X, size.Y))))
	return math.Min(dpi*zoom, limit)
}
