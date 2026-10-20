package ocr

import (
	"strings"
	"testing"
)

func TestParseHOCRCharactersAndFallback(t *testing.T) {
	const source = `<html><body><div class="ocr_page"><div class="ocr_carea"><p class="ocr_par"><span class="ocr_line">
 <span class="ocrx_word" title="bbox 10 10 70 30; x_wconf 94">
 <span class="ocrx_cinfo" title="x_bboxes 10 10 20 30">è</span>
 <span class="ocrx_cinfo" title="x_bboxes 20 10 50 30">fi</span>
 <span class="ocrx_cinfo" title="x_bboxes 50 10 70 30">&amp;</span></span>
 <span class="ocrx_word" title="bbox 80 10 100 30; x_wconf 80">ok</span>
 </span></p></div></div></body></html>`
	layout, err := ParseHOCR(strings.NewReader(source), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(layout.Words) != 2 || layout.Text(0, 1) != "èfi& ok" || len(layout.Words[0].Characters) != 3 || layout.Words[0].Confidence != 94 {
		t.Fatalf("%+v", layout)
	}
	units := layout.SelectionLayout()
	if units.Text(1, 2) != "fi&" || units.Text(2, 3) != "& ok" || units.Words[1].Quad[0].X != .2 {
		t.Fatalf("%+v", units)
	}
	for _, invalid := range []string{"x_bboxes 20 10 500 30", "x_bboxes 20 10 20 30", "missing"} {
		bad := strings.Replace(source, "x_bboxes 20 10 50 30", invalid, 1)
		l, err := ParseHOCR(strings.NewReader(bad), 100, 100)
		if err != nil || len(l.Words[0].Characters) != 0 || l.Text(0, 1) != "èfi& ok" {
			t.Fatalf("fallback: %+v %v", l, err)
		}
	}
	for _, bad := range []string{"garbage", "<html/>", strings.Replace(source, "bbox 10 10 70 30", "bbox 10 10 700 30", 1), source[:len(source)-10]} {
		if _, err := ParseHOCR(strings.NewReader(bad), 100, 100); err == nil {
			t.Fatal("accepted malformed hOCR")
		}
	}
}
