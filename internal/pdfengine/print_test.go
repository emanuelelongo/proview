//go:build cgo && !nocgo && !extlib

package pdfengine

import "testing"

func TestPrintRendererHonorsAnnotationPrintFlags(t *testing.T) {
	data := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << >> /Contents 4 0 R /Annots [5 0 R 7 0 R] >>",
		fixtureStream("", ""),
		"<< /Type /Annot /Subtype /Stamp /Rect [10 60 40 90] /F 0 /AP << /N 6 0 R >> >>",
		fixtureStream("1 0 0 rg 0 0 30 30 re f\n", "/Type /XObject /Subtype /Form /BBox [0 0 30 30] /Resources << >>"),
		"<< /Type /Annot /Subtype /Stamp /Rect [60 60 90 90] /F 36 /AP << /N 8 0 R >> >>",
		fixtureStream("0 0 1 rg 0 0 30 30 re f\n", "/Type /XObject /Subtype /Form /BBox [0 0 30 30] /Resources << >>"),
	}, "")
	view, err := RenderFormPage(data, 0, 72)
	if err != nil {
		t.Fatal(err)
	}
	printed, err := RenderPrintPage(data, 0, 72)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := view.At(25, 25).RGBA()
	if r < 60000 || g > 1000 || b > 1000 {
		t.Fatal("screen-only annotation fixture is missing")
	}
	r, g, b, _ = printed.At(25, 25).RGBA()
	if r < 60000 || g < 60000 || b < 60000 {
		t.Fatal("screen-only annotation was printed")
	}
	r, g, b, _ = printed.At(75, 25).RGBA()
	if r > 1000 || g > 1000 || b < 60000 {
		t.Fatal("print-only annotation was omitted")
	}
}

func TestPrintRendererRejectsOversizedAndMalformedPages(t *testing.T) {
	large := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10000 10000] /Resources << >> /Contents 4 0 R >>",
		fixtureStream("", ""),
	}, "")
	if _, err := RenderPrintPage(large, 0, 72); err == nil {
		t.Fatal("unbounded print allocation accepted")
	}
	if _, err := RenderPrintPage([]byte("invalid"), 0, 72); err == nil {
		t.Fatal("invalid PDF accepted")
	}
}
