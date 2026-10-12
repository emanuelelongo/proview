package document

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"unicode"

	"emanuelelongo/proview/internal/ocr"
	"emanuelelongo/proview/internal/textselect"
	img "emanuelelongo/proview/pkg/images"
	xdraw "golang.org/x/image/draw"
)

const (
	ocrMaxPixels    = 12_000_000
	ocrMaxDimension = 2400
)

// TextSelectionKey identifies the visible source content, not the view zoom.
// Redactions deliberately invalidate all pages, including shared-image reuses.
func (d *Document) TextSelectionKey(page int) string {
	if d == nil || d.validatePage(page) != nil {
		return ""
	}
	security := ""
	if d.HasRedactions() {
		security = fmt.Sprintf("%d:%v", d.Revision(), d.RedactionKeys())
	}
	if d.Format() == FormatPDF {
		p := d.pages[page]
		return fmt.Sprintf("%p:%d:%d:%d:%s", p.source, p.index, p.id, p.turns, security)
	}
	return fmt.Sprintf("%p:%v:%v", d.source, d.crop, d.imageTransform)
}

// SelectableText must run on a rendering snapshot, off the UI thread. It never
// writes an OCR layer into the source or changes editing/history state.
func (d *Document) SelectableText(ctx context.Context, page int, engine ocr.Engine) (textselect.Layout, error) {
	if err := ctx.Err(); err != nil {
		return textselect.Layout{}, err
	}
	if err := d.validatePage(page); err != nil {
		return textselect.Layout{}, err
	}
	if d.Format() == FormatPDF && d.HasRedactions() {
		safe := d.RenderingSnapshot()
		// Added objects retain their editing behavior; recognition targets source content.
		safe.signatures = nil
		data, err := safe.exportPDF()
		if err != nil {
			return textselect.Layout{}, err
		}
		if err = ctx.Err(); err != nil {
			return textselect.Layout{}, err
		}
		source, err := Load("ocr-sanitized.pdf", bytes.NewReader(data))
		if err != nil {
			return textselect.Layout{}, err
		}
		defer source.Close()
		doc, err := New(source)
		if err != nil {
			return textselect.Layout{}, err
		}
		return doc.SelectableText(ctx, page, engine)
	}
	var raster image.Image
	var err error
	transform := d.imageTransform
	if d.Format() == FormatPDF {
		geometry, e := d.TextGeometry(page)
		if e != nil {
			return textselect.Layout{}, e
		}
		layout := selectableNativeText(geometry)
		if len(layout.Words) > 0 {
			return layout, ctx.Err()
		}
		size, e := d.PageSize(page)
		if e != nil {
			return textselect.Layout{}, e
		}
		// PDF PageSize is the 144 DPI preview size, including editor rotation.
		dpi := math.Min(300, 144*math.Sqrt(float64(ocrMaxPixels)/(float64(size.X)*float64(size.Y))))
		dpi = math.Min(dpi, 144*float64(ocrMaxDimension)/float64(max(size.X, size.Y)))
		raster, err = d.basePageDPI(page, dpi)
		transform = img.Transform{Turns: d.pages[page].turns}
	} else {
		raster, err = d.Preview(page)
		if err == nil && d.crop != nil {
			raster = img.Crop(raster, normalizedImageRect(*d.crop, raster.Bounds()))
		}
	}
	if err != nil {
		return textselect.Layout{}, err
	}
	if err = ctx.Err(); err != nil {
		return textselect.Layout{}, err
	}
	if engine == nil {
		return textselect.Layout{}, fmt.Errorf("OCR engine is unavailable")
	}
	raster = ocrRaster(raster)
	layout, err := engine.Recognize(ctx, raster)
	if err != nil {
		return textselect.Layout{}, err
	}
	layout.Transform(transform.Turns, transform.FlipX, transform.FlipY)
	return layout, ctx.Err()
}

func ocrRaster(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	// Camera photos can contain very large glyphs and paper texture. Downsampling
	// to a useful text scale improves segmentation without changing view/export pixels.
	scale := math.Min(1, float64(ocrMaxDimension)/float64(max(w, h)))
	scale = math.Min(scale, math.Sqrt(float64(ocrMaxPixels)/(float64(w)*float64(h))))
	if scale < 1 {
		w = max(1, int(float64(w)*scale))
		h = max(1, int(float64(h)*scale))
	}

	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	if b.Dx() == w && b.Dy() == h {
		draw.Draw(out, out.Bounds(), src, b.Min, draw.Over)
	} else {
		xdraw.BiLinear.Scale(out, out.Bounds(), src, b, draw.Over, nil)
	}
	return out
}

func selectableNativeText(native TextLayout) textselect.Layout {
	result := textselect.Layout{Width: native.Width, Height: native.Height}
	for i := 0; i < len(native.Glyphs); {
		if unicode.IsSpace(native.Glyphs[i].Rune) || native.Glyphs[i].Rune == 0 {
			i++
			continue
		}
		end := i
		var text strings.Builder
		for end < len(native.Glyphs) && native.Glyphs[end].Line == native.Glyphs[i].Line && !unicode.IsSpace(native.Glyphs[end].Rune) && native.Glyphs[end].Rune != 0 {
			text.WriteRune(native.Glyphs[end].Rune)
			end++
		}
		quads := native.Selection(i, end-1)
		if len(quads) > 0 {
			q := quads[0]
			result.Words = append(result.Words, textselect.Word{Text: text.String(), Line: native.Glyphs[i].Line, Confidence: 100, Quad: textselect.Quad{{X: q.UL.X, Y: q.UL.Y}, {X: q.UR.X, Y: q.UR.Y}, {X: q.LR.X, Y: q.LR.Y}, {X: q.LL.X, Y: q.LL.Y}}})
		}
		i = end
	}
	return result
}
