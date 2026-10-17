package document

import (
	"bytes"
	"emanuelelongo/proview/pkg/annotations"
	"fmt"
	xdraw "golang.org/x/image/draw"
	"image"
	"image/color"
	"image/draw"
	"math"
)

func (s SignatureOverlay) IsLens() bool {
	return s.annotation != nil && s.annotation.Kind == annotations.Shape && s.annotation.Shape == annotations.Magnifier
}
func (s SignatureOverlay) ArtworkAsset(aspect float64) (signaturesAsset []byte, err error) {
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
	return a.PDF()
}

func (d *Document) RenderPage(page int) (image.Image, error) { return d.renderPageDPI(page, 144, true) }
func (d *Document) basePageDPI(page int, dpi float64) (image.Image, error) {
	if d.Format() != FormatPDF {
		return d.RenderBasePage(page)
	}
	source, index, e := d.pageSource(page)
	if e != nil {
		return nil, e
	}
	if dpi == 144 {
		return source.Preview(index)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.pdf == nil {
		return nil, fmt.Errorf("The PDF is closed")
	}
	return source.pdf.ImageDPI(index, dpi)
}
func (d *Document) renderPageDPI(page int, dpi float64, lenses bool) (image.Image, error) {
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
	base, e := under.SafeRenderPage(s.Page, dpi)
	if e != nil {
		return nil, e
	}
	target := normalizedImageRect(s.Rect, base.Bounds())
	w, h := target.Dx(), target.Dy()
	if w < 1 || h < 1 || int64(w)*int64(h) > 128_000_000 {
		return nil, fmt.Errorf("The lens is too large")
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(target.Min.X)+float64(w)/2, float64(target.Min.Y)+float64(h)/2
	zoom := s.annotation.Magnification
	// A bounded sample is scaled once, then only visible disk pixels are retained.
	sample := image.Rect(int(math.Floor(cx-float64(w)/2/zoom)), int(math.Floor(cy-float64(h)/2/zoom)), int(math.Ceil(cx+float64(w)/2/zoom)), int(math.Ceil(cy+float64(h)/2/zoom)))
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
