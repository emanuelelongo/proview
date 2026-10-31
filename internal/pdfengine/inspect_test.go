//go:build cgo && !nocgo && !extlib

package pdfengine

import "testing"

func TestInspectPDFClassifiesPageAnnotations(t *testing.T) {
	for _, tc := range []struct {
		name, annots    string
		visual, imports bool
	}{
		{"none", "", false, false},
		{"borderless link", "/Annots [<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /Border [0 0 0] >>]", false, false},
		{"bordered link", "/Annots [<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /Border [0 0 1] >>]", true, false},
		{"hidden text", "/Annots [<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /F 2 >>]", false, false},
		{"square", "/Annots [<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] >>]", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info, err := InspectPDF(textFixture(tc.annots, ""))
			if err != nil {
				t.Fatal(err)
			}
			if info.AnnotPages[0] != tc.visual || info.HasImportable != tc.imports || info.HasForms {
				t.Fatalf("info = %+v", info)
			}
		})
	}
	if _, err := InspectPDF([]byte("not a PDF")); err == nil {
		t.Fatal("malformed PDF accepted")
	}
}

// Text layout cleans only resources reachable from the requested page, but it
// must keep rejecting inherited Form resources exactly like the export path.
func TestTextLayoutPageScopedFormCleaning(t *testing.T) {
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /XObject << /Fm 6 0 R >> /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fixtureStream("q /Fm Do Q", ""),
		fixtureStream("BT /F1 16 Tf 30 140 Td (FORM TEXT) Tj ET", "/Type /XObject /Subtype /Form /BBox [0 0 300 200]"),
	}, "")
	if _, err := TextLayout(input, 0); err == nil {
		t.Fatal("inherited Form resources accepted")
	}
}
