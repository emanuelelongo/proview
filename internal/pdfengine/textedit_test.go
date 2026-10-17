//go:build cgo && !nocgo && !extlib

package pdfengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gen2brain/go-fitz"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	fontfile "github.com/tdewolff/font"
	"golang.org/x/image/font/gofont/goregular"
)

func probeRange(t *testing.T, input []byte, page int, word string) (Layout, int, int) {
	t.Helper()
	l, err := TextLayout(input, page)
	if err != nil {
		t.Fatal(err)
	}
	var s strings.Builder
	for _, g := range l.Glyphs {
		s.WriteRune(g.Rune)
	}
	pos := strings.Index(s.String(), word)
	if pos < 0 {
		t.Fatalf("%q missing in %q", word, s.String())
	}
	return l, len([]rune(s.String()[:pos])), len([]rune(word))
}

func probeCorrect(t *testing.T, input []byte, page int, word, replacement string, dx, dy float64) []byte {
	t.Helper()
	_, start, count := probeRange(t, input, page, word)
	original := bytes.Clone(input)
	out, err := CorrectText(input, page, start, count, replacement, dx, dy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, original) {
		t.Fatal("source mutated")
	}
	if err := api.Validate(context.Background(), bytes.NewReader(out), nil, nil); err != nil {
		t.Fatal(err)
	}
	return out
}

func probeImage(t *testing.T, data []byte, page int) image.Image {
	t.Helper()
	doc, err := fitz.NewFromMemory(data)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	im, err := doc.ImageDPI(page, 144)
	if err != nil {
		t.Fatal(err)
	}
	return im
}

func probeSameWordPosition(t *testing.T, before, after []byte, page int, word string) {
	t.Helper()
	a, startA, n := probeRange(t, before, page, word)
	b, startB, _ := probeRange(t, after, page, word)
	for i := 0; i < n; i++ {
		p, q := a.Glyphs[startA+i].Quad, b.Glyphs[startB+i].Quad
		for j, x := range []Point{p.UL, p.UR, p.LL, p.LR} {
			y := []Point{q.UL, q.UR, q.LL, q.LR}[j]
			if math.Abs(x.X-y.X) > 0.00001 || math.Abs(x.Y-y.Y) > 0.00001 {
				t.Fatalf("%s moved: %+v vs %+v", word, p, q)
			}
		}
	}
}

func probeFixture(content, attributes string) []byte {
	return fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /Resources << /Font << /F1 4 0 R >> /XObject << /Im1 6 0 R >> >> /Contents 5 0 R " + attributes + " >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fixtureStream(content, ""),
		fixtureStream("\xff\x00\x00", "/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceRGB"),
	}, "")
}

func TestTextCorrectionCorrections(t *testing.T) {
	for _, tc := range []struct {
		name, operator, replacement string
		dx, dy                      float64
	}{
		{"shorter", "(PUBLIC SECRET KEEP) Tj", "OK", 0, 0},
		{"longer", "(PUBLIC SECRET KEEP) Tj", "EDITED!", 0, 0},
		{"position", "(PUBLIC SECRET KEEP) Tj", "SECRET", 15, 25},
		{"delete", "(PUBLIC SECRET KEEP) Tj", "", 0, 0},
		{"kerning", "[(PUBLIC ) 35 (SE) -20 (CRET) 40 ( KEEP)] TJ", "EDIT", 0, 0},
		{"unicode", "(PUBLIC SECRET KEEP) Tj", "Caffè", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := probeFixture("q .85 .9 .7 rg 0 0 420 200 re f Q\nBT /F1 16 Tf 30 140 Td "+tc.operator+" 0 -65 Td (SECOND LINE) Tj ET\n0 0 m 20 20 l S\nq 12 0 0 12 350 10 cm /Im1 Do Q\n", "")
			out := probeCorrect(t, input, 0, "SECRET", tc.replacement, tc.dx, tc.dy)
			actual := extractedText(t, out)
			if tc.replacement != "SECRET" && strings.Contains(actual, "SECRET") {
				t.Fatalf("old text retained: %q", actual)
			}
			if tc.replacement != "" && !strings.Contains(actual, tc.replacement) {
				t.Fatalf("replacement missing: %q", actual)
			}
			for _, word := range []string{"PUBLIC", "KEEP", "SECOND LINE"} {
				probeSameWordPosition(t, input, out, 0, word)
			}
			before, after := probeImage(t, input, 0), probeImage(t, out, 0)
			// At 144 DPI, both old and new text fit inside this deliberately
			// broad zone. Every background/vector pixel outside must be identical.
			differences := 0
			for y := 0; y < before.Bounds().Dy(); y++ {
				for x := 0; x < before.Bounds().Dx(); x++ {
					if before.At(x, y) != after.At(x, y) {
						differences++
						if !image.Pt(x, y).In(image.Rect(175, 75, 450, 195)) {
							t.Fatalf("unrelated pixel changed at %d,%d", x, y)
						}
					}
				}
			}
			if differences == 0 {
				t.Fatal("render did not change")
			}
			if tc.name == "position" {
				a, ia, _ := probeRange(t, input, 0, "SECRET")
				b, ib, _ := probeRange(t, out, 0, "SECRET")
				p, q := a.Glyphs[ia].Quad.UL, b.Glyphs[ib].Quad.UL
				if math.Abs((q.X-p.X)*a.Width-tc.dx) > .05 || math.Abs((q.Y-p.Y)*a.Height-tc.dy) > .05 {
					t.Fatal("wrong displacement")
				}
			}
			if tc.name == "shorter" {
				probeArtifacts(t, "basic", input, out)
			}
		})
	}
}

func probeArtifacts(t *testing.T, name string, before, after []byte) {
	t.Helper()
	dir := os.Getenv("PROVIEW_TEXT_EDIT_OUTPUT")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		suffix string
		data   []byte
	}{{"before", before}, {"after", after}} {
		path := filepath.Join(dir, name+"-"+item.suffix)
		if err := os.WriteFile(path+".pdf", item.data, 0644); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(path + ".png")
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, probeImage(t, item.data, 0))
		closeErr := f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
}

func TestTextCorrectionRepeatedAndSharedText(t *testing.T) {
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /Resources << /XObject << /Shared 5 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /Resources << /XObject << /Shared 5 0 R >> >> /Contents 7 0 R >>",
		fixtureStream("BT /F1 16 Tf 10 20 Td (SECRET KEEP) Tj ET", "/Type /XObject /Subtype /Form /BBox [0 0 200 50] /Resources << /Font << /F1 8 0 R >> >>"),
		fixtureStream("q 1 0 0 1 20 120 cm /Shared Do Q q 1 0 0 1 20 40 cm /Shared Do Q", ""),
		fixtureStream("/Shared Do", ""),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, "")
	out := probeCorrect(t, input, 0, "SECRET", "EDIT", 0, 0)
	text := extractedText(t, out)
	if strings.Count(text, "SECRET") != 1 || strings.Count(text, "EDIT") != 1 || strings.Count(text, "KEEP") != 2 {
		t.Fatalf("wrong instance changed: %q", text)
	}
	if !reflect.DeepEqual(probeImage(t, input, 1), probeImage(t, out, 1)) {
		t.Fatal("other page changed")
	}
	second := probeCorrect(t, out, 0, "EDIT", "AGAIN", 0, 0)
	if !strings.Contains(extractedText(t, second), "AGAIN") {
		t.Fatal("cannot re-edit saved text")
	}
}

func TestTextCorrectionPreservesForms(t *testing.T) {
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [6 0 R] /DA (/F1 12 Tf 0 g) /DR << /Font << /F1 4 0 R >> >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R /Annots [6 0 R] >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fixtureStream("BT /F1 16 Tf 30 170 Td (SECRET KEEP) Tj ET", ""),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 50 200 80] /P 3 0 R /F 4 /V (Initial) >>",
	}, "")
	before, err := ReadForms(input)
	if err != nil {
		t.Fatal(err)
	}
	l, err := TextLayout(input, 0)
	if err != nil {
		t.Fatal(err)
	}
	var s strings.Builder
	for _, g := range l.Glyphs {
		s.WriteRune(g.Rune)
	}
	if len(strings.Fields(s.String())) == 0 {
		t.Fatal("fixture has no native text")
	}
	word := strings.Fields(s.String())[0]
	out := probeCorrect(t, input, 0, word, "EDIT", 0, 0)
	after, err := ReadForms(out)
	if err != nil {
		t.Fatal(err)
	}
	before[0].ID = after[0].ID // Object numbers can change during a full rewrite.
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("fields changed: %+v / %+v", before, after)
	}
	if _, err := FillForms(out, map[int]string{after[0].ID: "Still editable"}); err != nil {
		t.Fatal(err)
	}
}

func TestTextCorrectionRejectsUnsupported(t *testing.T) {
	for _, tc := range []struct{ name, content, attributes, replacement string }{
		{"missing-glyph", "BT /F1 16 Tf 30 140 Td (SECRET) Tj ET", "", "漢"},
		{"ambiguous-duplicate-glyphs", "BT /F1 16 Tf 30 140 Td (SECRET) Tj ET BT /F1 16 Tf 30 140 Td (SECRET) Tj ET", "", "EDIT"},
		{"rotated-page", "BT /F1 16 Tf 30 140 Td (SECRET) Tj ET", "/Rotate 90", "EDIT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := probeFixture(tc.content, tc.attributes)
			_, start, n := probeRange(t, input, 0, "SECRET")
			out, err := CorrectText(input, 0, start, n, tc.replacement, 0, 0)
			if err == nil || out != nil {
				t.Fatal("unsupported edit accepted")
			}
			t.Log(err)
		})
	}
}

func TestTextCorrectionEmbeddedFont(t *testing.T) {
	widths := strings.Repeat("600 ", 95)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /TrueType /BaseFont /GoRegular /Encoding /WinAnsiEncoding /FirstChar 32 /LastChar 126 /Widths [" + widths + "] /FontDescriptor 6 0 R >>",
		fixtureStream("BT /F1 16 Tf 30 140 Td (PUBLIC SECRET KEEP) Tj ET", ""),
		"<< /Type /FontDescriptor /FontName /GoRegular /Flags 32 /FontBBox [-100 -300 1100 1100] /ItalicAngle 0 /Ascent 1000 /Descent -300 /CapHeight 700 /StemV 80 /FontFile2 7 0 R >>",
		fixtureStream(string(goregular.TTF), fmt.Sprintf("/Length1 %d", len(goregular.TTF))),
	}
	input := fixturePDF(objects, "")
	out := probeCorrect(t, input, 0, "SECRET", "Caffè €", 0, 0)
	if !strings.Contains(extractedText(t, out), "Caffè €") {
		t.Fatal("embedded font Unicode lost")
	}
	probeSameWordPosition(t, input, out, 0, "KEEP")
	probeArtifacts(t, "embedded", input, out)
	font, err := fontfile.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	ids := []uint16{0}
	seen := map[uint16]bool{0: true}
	for _, r := range "PUBLIC SECRET KEEP" {
		id := font.GlyphIndex(r)
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	reduced, err := font.Subset(ids, fontfile.SubsetOptions{Tables: fontfile.KeepAllTables})
	if err != nil {
		t.Fatal(err)
	}
	if reduced.GlyphIndex('Z') != 0 {
		t.Fatal("fixture still contains excluded glyph")
	}
	fontBytes := reduced.Write()
	objects[3] = strings.ReplaceAll(objects[3], "/GoRegular", "/ABCDEF+GoRegular")
	objects[5] = strings.ReplaceAll(objects[5], "/GoRegular", "/ABCDEF+GoRegular")
	objects[6] = fixtureStream(string(fontBytes), fmt.Sprintf("/Length1 %d", len(fontBytes)))
	subset := fixturePDF(objects, "")
	if len(subset) >= len(input)/2 {
		t.Fatal("font subset did not substantially reduce document size")
	}
	t.Logf("embedded document: %d bytes; subset: %d bytes", len(input), len(subset))
	corrected := probeCorrect(t, subset, 0, "SECRET", "KEEP", 0, 0)
	if strings.Contains(extractedText(t, corrected), "SECRET") {
		t.Fatal("subset correction lost text")
	}
	_, start, n := probeRange(t, subset, 0, "SECRET")
	if result, err := CorrectText(subset, 0, start, n, "Z", 0, 0); err == nil || result != nil {
		t.Fatal("subset missing glyph accepted")
	}
	probeArtifacts(t, "subset", subset, corrected)
}

func TestTextCorrectionLigatureRejection(t *testing.T) {
	// One physical glyph maps to three Unicode characters. Neither a whole
	// word nor a partial ligature is safe for this experimental matcher.
	cmap := "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def /CMapName /Test def /CMapType 2 def 1 begincodespacerange <00> <FF> endcodespacerange 2 beginbfrange <20> <7D> <0020> <7F> <FF> <007F> endbfrange 1 beginbfchar <7E> <006600660069> endbfchar endcmap CMapName currentdict /CMap defineresource pop end end"
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /TrueType /BaseFont /GoRegular /Encoding /WinAnsiEncoding /FirstChar 32 /LastChar 126 /Widths [" + strings.Repeat("600 ", 95) + "] /FontDescriptor 6 0 R /ToUnicode 8 0 R >>",
		fixtureStream("BT /F1 16 Tf 30 140 Td (PUBLIC o~ce KEEP) Tj ET", ""),
		"<< /Type /FontDescriptor /FontName /GoRegular /Flags 32 /FontBBox [-100 -300 1100 1100] /ItalicAngle 0 /Ascent 1000 /Descent -300 /CapHeight 700 /StemV 80 /FontFile2 7 0 R >>",
		fixtureStream(string(goregular.TTF), fmt.Sprintf("/Length1 %d", len(goregular.TTF))), fixtureStream(cmap, ""),
	}, "")
	_, start, n := probeRange(t, input, 0, "office")
	for _, r := range []struct{ start, n int }{{start, n}, {start + 2, 1}} {
		if out, err := CorrectText(input, 0, r.start, r.n, "EDIT", 0, 0); err == nil || out != nil {
			t.Fatal("ambiguous ligature accepted")
		}
	}
}

func TestTextCorrectionPreservesStacking(t *testing.T) {
	// Inserting after the original ET preserves the later opaque shape.
	input := probeFixture("BT /F1 16 Tf 30 140 Td (SECRET) Tj ET\n1 1 1 rg 25 130 90 35 re f\n", "")
	out := probeCorrect(t, input, 0, "SECRET", "SECRET", 0, 0)
	if !reflect.DeepEqual(probeImage(t, input, 0), probeImage(t, out, 0)) {
		t.Fatal("correction changed paint order")
	}

}

func TestTextCorrectionPreservesClipAndTransformedWholeObject(t *testing.T) {
	for _, content := range []string{
		"q 30 130 32 35 re W n BT /F1 16 Tf 30 140 Td (SECRET) Tj ET Q",
		"q 1 0 0 1 15 20 cm BT /F1 16 Tf 30 140 Td (SECRET) Tj ET Q",
		"BT /F1 16 Tf 30 140 Td (SECRET ) Tj 1 0 0 rg (KEEP) Tj ET",
		"BT /F1 16 Tf 1 0 0 rg 30 140 Td (SECRET ) Tj 0 0 1 rg (KEEP) Tj ET",
	} {
		input := probeFixture(content, "")
		out := probeCorrect(t, input, 0, "SECRET", "SECRET", 0, 0)
		if !reflect.DeepEqual(probeImage(t, input, 0), probeImage(t, out, 0)) {
			t.Fatal("clip or transformed text changed")
		}
	}
}

func TestTextCorrectionIsolatesSharedContents(t *testing.T) {
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /Resources << /Font << /F1 6 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /Resources << /Font << /F1 6 0 R >> >> /Contents 5 0 R >>",
		fixtureStream("BT /F1 16 Tf 30 140 Td (SECRET KEEP) Tj ET", ""),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, "")
	out := probeCorrect(t, input, 0, "SECRET", "EDIT", 0, 0)
	if !reflect.DeepEqual(probeImage(t, input, 1), probeImage(t, out, 1)) {
		t.Fatal("editing shared Contents changed another page")
	}
}

func TestTextCorrectionRejectsUnsupportedTextStates(t *testing.T) {
	for _, content := range []string{
		"BT /F1 16 Tf 3 Tr 30 140 Td (SECRET) Tj ET",
		"BT /F1 16 Tf 80 Tz 30 140 Td (SECRET) Tj ET",
		"20 20 m BT /F1 16 Tf 30 140 Td (SECRET) Tj ET 30 30 l S",
	} {
		input := probeFixture(content, "")
		layout, err := CorrectionTextLayout(input, 0)
		if err != nil {
			t.Fatal(err)
		}
		if out, err := CorrectText(input, 0, 0, len(layout.Glyphs), "EDIT", 0, 0); err == nil || out != nil {
			t.Fatal("unsupported text state accepted")
		}
	}
}

func TestTextEditingRejectsProposedOverlap(t *testing.T) {
	input := probeFixture("BT /F1 16 Tf 30 140 Td (PUBLIC SECRET KEEP) Tj ET", "")
	layout, start, count := probeRange(t, input, 0, "SECRET")
	_, keep, _ := probeRange(t, input, 0, "KEEP")
	aligned := (layout.Glyphs[keep].Origin.X - layout.Glyphs[start].Origin.X) * layout.Width
	for _, tc := range []struct {
		value string
		dx    float64
	}{{"EXPANSION", 0}, {"SECRET", 30}, {"SECRET", aligned}} {
		original := bytes.Clone(input)
		output, err := CorrectText(input, 0, start, count, tc.value, tc.dx, 0)
		if !errors.Is(err, ErrTextOverlap) || output != nil {
			t.Fatalf("overlap accepted: %v", err)
		}
		if !bytes.Equal(input, original) {
			t.Fatal("rejected edit changed source")
		}
	}
	output := probeCorrect(t, input, 0, "SECRET", "EXPANSION", 0, 30)
	probeSameWordPosition(t, input, output, 0, "KEEP")
	if !strings.Contains(extractedText(t, output), "EXPANSION") {
		t.Fatal("free position rejected")
	}
}

func TestTextCorrectionCanRepairExistingOverlap(t *testing.T) {
	// Model a PDF saved by the earlier prototype: the correction is in its
	// own Form, extending over KEEP in the surrounding source text.
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /Resources << /Font << /F1 4 0 R >> /XObject << /Correction 6 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fixtureStream("BT /F1 16 Tf 30 140 Td (PUBLIC ) Tj 1 0 0 1 165 140 Tm (KEEP) Tj ET /Correction Do", ""),
		fixtureStream("BT /F1 16 Tf 95 140 Td (CORRECTION) Tj ET", "/Type /XObject /Subtype /Form /BBox [0 0 420 200] /Resources << /Font << /F1 4 0 R >> >>"),
	}, "")
	for _, tc := range []struct {
		name, value string
		dy          float64
	}{
		{"shorten", "OK", 0},
		{"delete", "", 0},
		{"move", "CORRECTION", 30},
		{"keep-overlap", "CORRECTION!", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "keep-overlap" {
				_, start, count := probeRange(t, input, 0, "CORRECTION")
				output, err := CorrectText(input, 0, start, count, tc.value, 0, tc.dy)
				if !errors.Is(err, ErrTextOverlap) || output != nil {
					t.Fatalf("overlap accepted: %v", err)
				}
				return
			}
			out := probeCorrect(t, input, 0, "CORRECTION", tc.value, 0, tc.dy)
			text := extractedText(t, out)
			if tc.value != "CORRECTION" && tc.value != "CORRECTION!" && strings.Contains(text, "CORRECTION") {
				t.Fatal("old overlapping word remains")
			}
			if tc.value != "" && !strings.Contains(text, tc.value) {
				t.Fatalf("replacement missing: %q", text)
			}
			probeSameWordPosition(t, input, out, 0, "PUBLIC")
			probeSameWordPosition(t, input, out, 0, "KEEP")
		})
	}
}

func TestTextCorrectionDeletionStillRejectsAmbiguousSourceGlyphs(t *testing.T) {
	input := probeFixture("BT /F1 16 Tf 30 140 Td (SECRET) Tj ET BT /F1 16 Tf 30 140 Td (SECRET) Tj ET", "")
	_, start, count := probeRange(t, input, 0, "SECRET")
	out, err := CorrectText(input, 0, start, count, "", 0, 0)
	if err == nil || out != nil {
		t.Fatal("ambiguous duplicate glyphs were deleted")
	}
}

func TestTextCorrectionBaselineGeometry(t *testing.T) {
	input := probeFixture("BT /F1 14 Tf 1 0 0 1 30 150 Tm (SMALL) Tj ET BT /F1 24 Tf 1 0 0 1 200 150 Tm (LARGE) Tj ET", "")
	for _, extract := range []func([]byte, int) (Layout, error){TextLayout, CorrectionTextLayout} {
		layout, err := extract(input, 0)
		if err != nil {
			t.Fatal(err)
		}
		var small, large *Glyph
		for i := range layout.Glyphs {
			switch layout.Glyphs[i].Rune {
			case 'S':
				small = &layout.Glyphs[i]
			case 'L':
				if layout.Glyphs[i].Origin.X > .4 {
					large = &layout.Glyphs[i]
				}
			}
		}
		if small == nil || large == nil {
			t.Fatal("missing fixture text")
		}
		if math.Abs(small.Origin.Y-large.Origin.Y)*layout.Height > .001 || math.Abs(small.Origin.Y*layout.Height-50) > .001 {
			t.Fatal("extracted baseline depends on font size")
		}
		if math.Abs(small.Quad.UL.Y-large.Quad.UL.Y)*layout.Height < 1 {
			t.Fatal("fixture should have different ascenders")
		}
	}
}

func TestTextEditingPreservesCustomSpacing(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"word_negative", "BT /F1 16 Tf -.036714 Tw 30 140 Td (PUBLIC SECRET KEEP) Tj ET"},
		{"word_positive", "BT /F1 16 Tf 3 Tw 30 140 Td (PUBLIC SECRET KEEP) Tj ET"},
		{"character_positive", "BT /F1 16 Tf .4 Tc 30 140 Td (PUBLIC SECRET KEEP) Tj ET"},
		{"character_negative", "BT /F1 16 Tf -.3 Tc 30 140 Td (PUBLIC SECRET KEEP) Tj ET"},
		{"combined", "BT /F1 16 Tf .4 Tc 2 Tw 30 140 Td (PUBLIC SECRET KEEP) Tj ET"},
		{"quote", "BT /F1 16 Tf 30 140 Td 2 .4 (PUBLIC SECRET KEEP) \" ET"},
		{"restore_state", "BT /F1 16 Tf .4 Tc 2 Tw ET q BT .1 Tc -1 Tw ET Q BT 30 140 Td (PUBLIC SECRET KEEP) Tj ET"},
		{"font_text_scale", "BT /F1 32 Tf .8 Tc 4 Tw .5 0 0 .5 30 140 Tm (PUBLIC SECRET KEEP) Tj ET"},
		{"ctm_scale", "q .5 0 0 .5 0 0 cm BT /F1 32 Tf .8 Tc 4 Tw 60 280 Td (PUBLIC SECRET KEEP) Tj ET Q"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := probeFixture(tc.content+"\nBT /F1 16 Tf 0 Tc 0 Tw 30 65 Td (SECOND LINE) Tj ET", "")
			identical := probeCorrect(t, input, 0, "SECRET", "SECRET", 0, 0)
			probeSameWordPosition(t, input, identical, 0, "SECRET")
			if !reflect.DeepEqual(probeImage(t, input, 0), probeImage(t, identical, 0)) {
				t.Fatal("unchanged text lost spacing or rendering fidelity")
			}
			for _, value := range []string{"EDIT", "EXPANSION", "EDIT NOW", ""} {
				dy := float64(0)
				if value == "EXPANSION" || value == "EDIT NOW" {
					dy = 30
				}
				output := probeCorrect(t, input, 0, "SECRET", value, 0, dy)
				for _, word := range []string{"PUBLIC", "KEEP", "SECOND LINE"} {
					probeSameWordPosition(t, input, output, 0, word)
				}
				actual := extractedText(t, output)
				if strings.Contains(actual, "SECRET") {
					t.Fatal("original text survived")
				}
				if value != "" && !strings.Contains(actual, value) {
					t.Fatalf("replacement missing: %q", actual)
				}
			}
			// The generated Form must remain editable even when surrounding source text
			// retains nonzero spacing after the first edit.
			output := probeCorrect(t, input, 0, "SECRET", "EDIT", 0, 0)
			output = probeCorrect(t, output, 0, "EDIT", "NEXT", 5, 0)
			probeSameWordPosition(t, input, output, 0, "KEEP")
		})
	}
}

func TestTextEditingAllowsSpacingElsewhereOnPage(t *testing.T) {
	input := probeFixture("BT /F1 16 Tf 30 140 Td (SECRET) Tj ET\nBT /F1 16 Tf .4 Tc 3 Tw 30 65 Td (PUBLIC KEEP) Tj ET", "")
	output := probeCorrect(t, input, 0, "SECRET", "EDIT", 0, 0)
	probeSameWordPosition(t, input, output, 0, "PUBLIC KEEP")
}

func TestTextEditingRejectsMixedSpacingWithinSelection(t *testing.T) {
	input := probeFixture("BT /F1 16 Tf .2 Tc 30 140 Td (SEC) Tj .4 Tc (RET KEEP) Tj ET", "")
	_, start, count := probeRange(t, input, 0, "SECRET")
	output, err := CorrectText(input, 0, start, count, "EDIT", 0, 0)
	if err == nil || output != nil || !strings.Contains(err.Error(), "Mixed spacing") {
		t.Fatalf("mixed spacing accepted: %v", err)
	}
}

func TestTextEditingAppliesSpacingToReplacementSpaces(t *testing.T) {
	for _, spacing := range []string{".4 Tc 3 Tw", "-.3 Tc -.036714 Tw", "0 Tc 3 Tw"} {
		input := probeFixture("BT /F1 16 Tf "+spacing+" 30 140 Td (SECRET) Tj ET", "")
		reference := probeFixture("BT /F1 16 Tf "+spacing+" 30 140 Td (EDIT NOW) Tj ET", "")
		output := probeCorrect(t, input, 0, "SECRET", "EDIT NOW", 0, 0)
		probeSameWordPosition(t, reference, output, 0, "EDIT NOW")
		reedited := probeCorrect(t, output, 0, "EDIT NOW", "EDIT NOW", 0, 0)
		probeSameWordPosition(t, reference, reedited, 0, "EDIT NOW")
		if !reflect.DeepEqual(probeImage(t, reference, 0), probeImage(t, output, 0)) {
			t.Fatalf("replacement spacing differs from source formatting: %s", spacing)
		}
	}
}

func TestTextEditingSpacingInsideSharedForms(t *testing.T) {
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /Resources << /XObject << /Shared 4 0 R >> >> /Contents 5 0 R >>",
		fixtureStream("BT /F1 16 Tf .4 Tc 2 Tw 10 20 Td (SECRET KEEP) Tj ET", "/Type /XObject /Subtype /Form /BBox [0 0 250 50] /Resources << /Font << /F1 6 0 R >> >>"),
		fixtureStream("q 1 0 0 1 20 120 cm /Shared Do Q q 1 0 0 1 20 40 cm /Shared Do Q", ""),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, "")
	output := probeCorrect(t, input, 0, "SECRET", "EDIT", 0, 0)
	if text := extractedText(t, output); strings.Count(text, "SECRET") != 1 || strings.Count(text, "EDIT") != 1 {
		t.Fatalf("wrong shared instance changed: %q", text)
	}
	before, after := probeImage(t, input, 0), probeImage(t, output, 0)
	for y := 200; y < before.Bounds().Dy(); y++ {
		for x := 0; x < before.Bounds().Dx(); x++ {
			if before.At(x, y) != after.At(x, y) {
				t.Fatal("other Form instance changed")
			}
		}
	}
	output = probeCorrect(t, output, 0, "EDIT", "NEXT", 0, 0)
	if !strings.Contains(extractedText(t, output), "NEXT") {
		t.Fatal("cannot re-edit Form text")
	}
}

func TestTextEditingRejectsInheritedFormSpacing(t *testing.T) {
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /Resources << /XObject << /Shared 4 0 R >> >> /Contents 5 0 R >>",
		fixtureStream("BT /F1 16 Tf 10 20 Td (SECRET KEEP) Tj ET", "/Type /XObject /Subtype /Form /BBox [0 0 250 50] /Resources << /Font << /F1 6 0 R >> >>"),
		fixtureStream("BT .4 Tc ET /Shared Do", ""),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, "")
	_, start, count := probeRange(t, input, 0, "SECRET")
	output, err := CorrectText(input, 0, start, count, "EDIT", 0, 0)
	if err == nil || output != nil || !strings.Contains(err.Error(), "Inherited Form text spacing") {
		t.Fatalf("inherited Form spacing accepted: %v", err)
	}
}

func TestTextEditingDuplicatesNativeTextWithoutChangingOriginal(t *testing.T) {
	input := probeFixture("BT /F1 16 Tf .4 Tc 2 Tw 30 140 Td (PUBLIC SECRET KEEP) Tj ET", "")
	_, start, count := probeRange(t, input, 0, "SECRET")
	original := bytes.Clone(input)
	for _, offset := range []float64{0, 30} {
		output, err := DuplicateText(input, 0, start, count, "SECRET", offset, 0)
		if !errors.Is(err, ErrTextOverlap) || output != nil {
			t.Fatalf("overlapping duplicate accepted: %v", err)
		}
	}
	output, err := DuplicateText(input, 0, start, count, "SECRET", 0, 35)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, original) {
		t.Fatal("duplicate changed input")
	}
	if err := api.Validate(context.Background(), bytes.NewReader(output), nil, nil); err != nil {
		t.Fatal(err)
	}
	text := extractedText(t, output)
	if strings.Count(text, "SECRET") != 2 {
		t.Fatalf("missing original or copy: %q", text)
	}
	for _, word := range []string{"PUBLIC", "SECRET", "KEEP"} {
		probeSameWordPosition(t, input, output, 0, word)
	}
	layout, err := CorrectionTextLayout(output, 0)
	if err != nil {
		t.Fatal(err)
	}
	index := -1
	for i, g := range layout.Glyphs {
		if g.Rune == 'S' && math.Abs((g.Origin.Y-.3)*layout.Height-35) < .05 {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("duplicate missing at requested position")
	}
	edited, err := CorrectText(output, 0, index, 6, "EDIT", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	text = extractedText(t, edited)
	if strings.Count(text, "SECRET") != 1 || strings.Count(text, "EDIT") != 1 {
		t.Fatalf("copy is not independently editable: %q", text)
	}
}
