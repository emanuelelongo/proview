package document

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"emanuelelongo/proview/pkg/annotations"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestMultiplyHighlightPreservesBlackAndTransparentPixels(t *testing.T) {
	base := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	for x, c := range []color.NRGBA{{255, 255, 255, 255}, {0, 0, 0, 255}, {100, 150, 200, 255}, {0, 0, 0, 0}} {
		base.SetNRGBA(x, 0, c)
	}
	mask := image.NewNRGBA(base.Bounds())
	draw.Draw(mask, mask.Bounds(), image.NewUniform(color.NRGBA{255, 235, 0, 255}), image.Point{}, draw.Src)
	fragment := MultiplyHighlight(base, mask, base.Bounds())
	want := []color.NRGBA{{255, 235, 0, 255}, {0, 0, 0, 255}, {100, 138, 0, 255}, {255, 235, 0, 255}}
	for x, w := range want {
		if c := color.NRGBAModel.Convert(fragment.At(x, 0)).(color.NRGBA); c != w {
			t.Fatalf("pixel %d=%v want %v", x, c, w)
		}
	}
}

func TestComposeRGBAFastPathMatchesNRGBA(t *testing.T) {
	bounds := image.Rect(17, 29, 41, 49)
	base := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			base.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 5), B: uint8(x + y), A: uint8((x + y) * 3)})
		}
	}
	target := NormalizedRect{X: .25, Y: .25, Width: .5, Height: .5}
	raster := image.NewNRGBA(image.Rect(0, 0, 12, 10))
	draw.Draw(raster, raster.Bounds(), image.NewUniform(color.NRGBA{R: 220, G: 30, B: 80, A: 111}), image.Point{}, draw.Src)
	highlight := image.NewNRGBA(image.Rect(0, 0, 12, 10))
	draw.Draw(highlight, highlight.Bounds(), image.NewUniform(color.NRGBA{R: 255, G: 235, A: 180}), image.Point{}, draw.Src)
	doc := &Document{signatures: []SignatureOverlay{
		{Page: 0, Rect: target, preview: raster},
		{Page: 0, Rect: target, preview: highlight, annotation: &annotations.Data{Kind: annotations.Highlight}},
	}}

	actual, err := doc.composePage(0, 144, false, nil, base)
	if err != nil {
		t.Fatal(err)
	}
	expected := image.NewNRGBA(bounds)
	draw.Draw(expected, bounds, base, bounds.Min, draw.Src)
	draw.Draw(expected, image.Rect(23, 34, 35, 44), raster, raster.Bounds().Min, draw.Over)
	mask := MultiplyHighlight(expected, highlight, image.Rect(23, 34, 35, 44))
	draw.Draw(expected, image.Rect(23, 34, 35, 44), mask, mask.Bounds().Min, draw.Over)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			got := color.NRGBAModel.Convert(actual.At(x, y)).(color.NRGBA)
			want := expected.NRGBAAt(x, y)
			if got != want {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}

	opaque := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			opaque.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 5), B: uint8(x + y), A: 255})
		}
	}
	actual, err = doc.composePage(0, 144, false, nil, opaque)
	if err != nil {
		t.Fatal(err)
	}
	expected = image.NewNRGBA(bounds)
	draw.Draw(expected, bounds, opaque, bounds.Min, draw.Src)
	draw.Draw(expected, image.Rect(23, 34, 35, 44), raster, raster.Bounds().Min, draw.Over)
	mask = MultiplyHighlight(expected, highlight, image.Rect(23, 34, 35, 44))
	draw.Draw(expected, image.Rect(23, 34, 35, 44), mask, mask.Bounds().Min, draw.Over)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			got := color.NRGBAModel.Convert(actual.At(x, y)).(color.NRGBA)
			if want := expected.NRGBAAt(x, y); got != want {
				t.Fatalf("opaque pixel (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

func TestNativeHighlightFullColorAndBlackTextThroughPDFRoundtrip(t *testing.T) {
	ctx, err := newAppearanceContext(100, 80)
	if err != nil {
		t.Fatal(err)
	}
	page, _, _, err := ctx.PageDict(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := addPDFStream(ctx, []byte("0 g 30 45 20 15 re f"), nil)
	if err != nil {
		t.Fatal(err)
	}
	page["Contents"] = *ref
	raw, err := writePDFContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	source, err := Load("highlight.pdf", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(doc.Close)
	data := annotations.Default(annotations.Highlight)
	data.PageWidth, data.PageHeight = 100, 80
	data.Quads = []annotations.Quad{{{X: 10, Y: 10}, {X: 90, Y: 10}, {X: 90, Y: 40}, {X: 10, Y: 40}}}
	addAnnotationGeometry(t, doc, data)
	check := func(img image.Image) {
		t.Helper()
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		at := func(x, y int) color.NRGBA { return color.NRGBAModel.Convert(img.At(x*w/100, y*h/80)).(color.NRGBA) }
		if c := at(20, 20); c.R < 250 || c.G < 230 || c.G > 240 || c.B > 5 {
			t.Fatalf("highlight color=%v", c)
		}
		if c := at(40, 25); c.R > 5 || c.G > 5 || c.B > 5 {
			t.Fatalf("highlight obscures black content: %v", c)
		}
		if c := at(5, 5); c.R < 250 || c.G < 250 || c.B < 250 {
			t.Fatalf("highlight leaked outside geometry: %v", c)
		}
	}
	img, err := doc.RenderPage(0)
	if err != nil {
		t.Fatal(err)
	}
	check(img)
	previews, err := doc.HighlightPreviews(0, 144)
	if err != nil || len(previews) != 1 {
		t.Fatalf("GUI fragments=%d err=%v", len(previews), err)
	}
	for range 3 {
		raw = exportEditable(t, doc)
		reopened := reopenEditable(t, raw)
		d, ok := reopened.Overlays()[0].Annotation()
		if !ok || d.Opacity != 1 {
			t.Fatal("native opacity changed")
		}
		img, err = reopened.RenderPage(0)
		if err != nil {
			t.Fatal(err)
		}
		check(img)
		src, err := Load("native.pdf", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		img, err = src.Preview(0)
		if err != nil {
			t.Fatal(err)
		}
		check(img)
		src.Close()
		doc = reopened
	}
	raw, err = doc.exportPDFFlattened()
	if err != nil {
		t.Fatal(err)
	}
	flattened, err := Load("flat.pdf", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer flattened.Close()
	img, err = flattened.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	check(img)
	// The blend operation stays standard vector graphics in the flattened export.
	saved, err := readPDFObjects(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range saved.Table {
		if entry != nil {
			if st, ok := entry.Object.(types.StreamDict); ok && st.Dict["Subtype"] == types.Name("Image") {
				t.Fatal("flattening rasterized vector content")
			}
		}
	}
}

// Keep the image.Image fallback as an independent reference for direct reads.
type genericHighlightImage struct{ image.Image }

func TestMultiplyHighlightDirectPixelsMatchGenericColors(t *testing.T) {
	base := image.NewNRGBA(image.Rect(13, 21, 269, 25))
	for y := 21; y < 25; y++ {
		for x := 13; x < 269; x++ {
			base.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 100, A: uint8(x - 13)})
		}
	}
	mask := image.NewNRGBA(image.Rect(0, 0, 256, 4))
	draw.Draw(mask, mask.Bounds(), image.NewUniform(color.NRGBA{R: 80, G: 220, B: 10, A: 177}), image.Point{}, draw.Src)
	for _, target := range []image.Rectangle{base.Bounds(), image.Rect(10, 20, 266, 24)} {
		actual := MultiplyHighlight(base, mask, target).(*image.NRGBA)
		expected := MultiplyHighlight(genericHighlightImage{base}, mask, target).(*image.NRGBA)
		if !bytes.Equal(actual.Pix, expected.Pix) {
			t.Fatal("direct blending changed alpha, subimage offsets or clipping")
		}
	}
}

func BenchmarkMultiplyHighlight(b *testing.B) {
	base := image.NewNRGBA(image.Rect(0, 0, 1800, 40))
	draw.Draw(base, base.Bounds(), image.White, image.Point{}, draw.Src)
	mask := image.NewNRGBA(base.Bounds())
	draw.Draw(mask, mask.Bounds(), image.NewUniform(color.NRGBA{255, 235, 0, 255}), image.Point{}, draw.Src)
	for _, direct := range []bool{false, true} {
		b.Run(fmt.Sprintf("direct=%v", direct), func(b *testing.B) {
			var source image.Image = genericHighlightImage{base}
			if direct {
				source = base
			}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				MultiplyHighlight(source, mask, base.Bounds())
			}
		})
	}
}
