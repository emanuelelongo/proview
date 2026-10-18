package document

import (
	"bytes"
	"context"
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
