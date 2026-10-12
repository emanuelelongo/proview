//go:build cgo && !nocgo && !extlib

package pdfengine

import (
	"bytes"
	"context"
	"fmt"
	"golang.org/x/image/font/gofont/goregular"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Security acceptance checks run in the ordinary suite and on native CI jobs.
func TestRedactionGateInlineAlternativeText(t *testing.T) {
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fixtureStream("/Span << /ActualText (ALTERNATIVE_SECRET) >> BDC\nBT /F1 16 Tf 30 140 Td (PUBLIC SECRET KEEP) Tj ET\nEMC\n", ""),
	}, "")
	layout, err := TextLayout(input, 0)
	if err != nil {
		t.Fatal(err)
	}
	output, err := RedactAndSanitize(input, regionsFor(layout, "SECRET"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(decodedObjects(t, output), "ALTERNATIVE_SECRET") {
		t.Fatal("BLOCKER: redacted output retains inline /ActualText containing sensitive data")
	}
}

func ocrImageFixture(shared, resourceMetadata bool) []byte {
	return ocrImageFixtureOptions(shared, resourceMetadata, "")
}

func ocrImageFixtureOptions(shared, resourceMetadata bool, mask string) []byte {
	return scanImageFixture(shared, resourceMetadata, mask, true)
}

func scanImageFixture(shared, resourceMetadata bool, mask string, withOCR bool) []byte {
	kids, count := "[3 0 R]", 1
	if shared {
		kids, count = "[3 0 R 7 0 R]", 2
	}
	// A white scan with a distinctive red patch underneath the OCR word SECRET.
	// Page two clips this patch out, but shares the original image object.
	pixels := bytes.Repeat([]byte{255, 255, 255}, 300*200)
	for y := 42; y < 65; y++ {
		for x := 99; x < 154; x++ {
			i := (y*300 + x) * 3
			pixels[i], pixels[i+1], pixels[i+2] = 255, 0, 0
		}
	}
	metadata := ""
	if resourceMetadata {
		metadata = "/Metadata 9 0 R"
	}
	content := "q 300 0 0 200 0 0 cm /Im0 Do Q\n"
	if withOCR {
		content += "BT /F1 16 Tf 3 Tr 30 140 Td (PUBLIC SECRET KEEP) Tj ET\n"
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids %s /Count %d >>", kids, count),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> /XObject << /Im0 6 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fixtureStream(content, ""),
		fixtureStream(string(pixels), "/Type /XObject /Subtype /Image /Width 300 /Height 200 /ColorSpace /DeviceRGB /BitsPerComponent 8 "+metadata+" "+mask),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /XObject << /Im0 6 0 R >> >> /Contents 8 0 R >>",
		fixtureStream("q 0 0 300 60 re W n 300 0 0 200 0 0 cm /Im0 Do Q\n", ""),
		fixtureStream("<metadata>RESOURCE_METADATA_SECRET</metadata>\n", "/Type /Metadata /Subtype /XML"),
	}
	if strings.Contains(mask, "SMask") {
		objects = append(objects, fixtureStream(string(bytes.Repeat([]byte{255}, 300*200)), "/Type /XObject /Subtype /Image /Width 300 /Height 200 /ColorSpace /DeviceGray /BitsPerComponent 8"))
	}
	return fixturePDF(objects, "")
}

func assertRemovedImagePatch(t *testing.T, output []byte) {
	t.Helper()
	ctx, err := api.ReadContext(context.Background(), bytes.NewReader(output), nil)
	if err != nil {
		t.Fatal(err)
	}
	images := 0
	for number, entry := range ctx.Table {
		stream, ok := entry.Object.(types.StreamDict)
		if !ok || stream.Dict.NameEntry("Subtype") == nil || *stream.Dict.NameEntry("Subtype") != "Image" {
			continue
		}
		images++
		if err := stream.Decode(); err != nil {
			t.Fatal(err)
		}
		// All fixtures use 300x200 RGB samples; no codecs or color conversions
		// are involved, so the selected patch can be inspected independently.
		if len(stream.Content) != 300*200*3 {
			t.Fatalf("unexpected image format in object %d", number)
		}
		index := (52*300 + 125) * 3
		if bytes.Equal(stream.Content[index:index+3], []byte{255, 0, 0}) {
			t.Errorf("BLOCKER: original selected scan pixels remain recoverable in image object %d", number)
		}
	}
	if images == 0 {
		t.Fatal("scan disappeared entirely")
	}
}

func TestRedactionGateOCRImage(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared=%v", shared), func(t *testing.T) {
			input := ocrImageFixture(shared, false)
			layout, err := TextLayout(input, 0)
			if err != nil {
				t.Fatal(err)
			}
			output, err := RedactAndSanitize(input, regionsFor(layout, "SECRET"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(extractedText(t, output), "SECRET") {
				t.Fatal("OCR text survived redaction")
			}
			assertRemovedImagePatch(t, output)
		})
	}
}

func TestRedactionGateResourceMetadata(t *testing.T) {
	// Keep the original resource reachable on a second page; otherwise pixel
	// replacement creates a new image and accidentally hides this limitation.
	input := ocrImageFixture(true, true)
	layout, err := TextLayout(input, 0)
	if err != nil {
		t.Fatal(err)
	}
	output, err := RedactAndSanitize(input, regionsFor(layout, "PUBLIC"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(decodedObjects(t, output), "RESOURCE_METADATA_SECRET") {
		t.Fatal("BLOCKER: metadata inside a retained image resource survived catalog reconstruction")
	}
}

func TestRedactionFormAlternativeText(t *testing.T) {
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /XObject << /Fm 6 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fixtureStream("q /Fm Do Q", ""),
		fixtureStream("/Span << /ActualText (ALTERNATIVE_SECRET) >> BDC BT /F1 16 Tf 30 140 Td (PUBLIC SECRET KEEP) Tj ET EMC", "/Type /XObject /Subtype /Form /BBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Metadata 7 0 R"),
		fixtureStream("RESOURCE_METADATA_SECRET", "/Type /Metadata /Subtype /XML"),
	}, "")
	layout, err := TextLayout(input, 0)
	if err != nil {
		t.Fatal(err)
	}
	regions := regionsFor(layout, "SECRET")
	if len(regions) == 0 {
		t.Fatal("visible Form text was not selectable")
	}
	output, err := RedactAndSanitize(input, regions)
	if err != nil {
		t.Fatal(err)
	}
	if objects := decodedObjects(t, output); strings.Contains(objects, "SECRET") {
		t.Fatalf("Form retained sensitive data: %s", objects)
	}
	if text := extractedText(t, output); !strings.Contains(text, "PUBLIC") || !strings.Contains(text, "KEEP") {
		t.Fatalf("Form public content lost: %q", text)
	}
}

func TestRedactionRejectsUnsupportedStructuresWithoutOutput(t *testing.T) {
	for name, replacement := range map[string]string{
		"optional layers": "/OCProperties << /OCGs [] >>",
		"XFA":             "/AcroForm << /XFA (SECRET) >>",
	} {
		t.Run(name, func(t *testing.T) {
			input := bytes.Replace(textFixture("", ""), []byte("/Type /Catalog"), []byte("/Type /Catalog "+replacement), 1)
			layout, err := TextLayout(input, 0)
			if err != nil {
				t.Fatal(err)
			}
			output, err := RedactAndSanitize(input, regionsFor(layout, "SECRET"))
			if err == nil || len(output) != 0 {
				t.Fatal("unsupported structure produced export bytes")
			}
		})
	}
	for _, mask := range []string{"/ImageMask true", "/SMask 10 0 R"} {
		altered := ocrImageFixtureOptions(false, false, mask)
		layout, err := TextLayout(altered, 0)
		if err != nil {
			t.Fatal(err)
		}
		output, err := RedactAndSanitize(altered, regionsFor(layout, "SECRET"))
		if err == nil || len(output) != 0 {
			t.Fatalf("mask %s produced bytes: %v", mask, err)
		}
	}
}

func TestSharedScanRotationAndPreservedPixels(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		t.Run(fmt.Sprint(rotation), func(t *testing.T) {
			input := ocrImageFixture(true, false)
			// Read/write independently to maintain a valid xref after page rotation.
			ctx, err := api.ReadContext(context.Background(), bytes.NewReader(input), nil)
			if err != nil {
				t.Fatal(err)
			}
			page := ctx.Table[3].Object.(types.Dict)
			page.Insert("Rotate", types.Integer(rotation))
			var rotated bytes.Buffer
			if err := api.WriteContext(context.Background(), ctx, &rotated); err != nil {
				t.Fatal(err)
			}
			layout, err := TextLayout(rotated.Bytes(), 0)
			if err != nil {
				t.Fatal(err)
			}
			output, err := RedactAndSanitize(rotated.Bytes(), regionsFor(layout, "SECRET"))
			if err != nil {
				t.Fatal(err)
			}
			assertRemovedImagePatch(t, output)
			parsed, err := api.ReadContext(context.Background(), bytes.NewReader(output), nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range parsed.Table {
				stream, ok := entry.Object.(types.StreamDict)
				if !ok || stream.Dict.NameEntry("Subtype") == nil || *stream.Dict.NameEntry("Subtype") != "Image" {
					continue
				}
				if err := stream.Decode(); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(stream.Content[:3], []byte{255, 255, 255}) {
					t.Fatal("unselected image pixels changed")
				}
			}
		})
	}
}

func TestEmbeddedTrueTypeLigatureSelection(t *testing.T) {
	widths := strings.Repeat("600 ", 95)
	cmap := "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def /CMapName /Test def /CMapType 2 def 1 begincodespacerange <00> <FF> endcodespacerange 2 beginbfrange <20> <7D> <0020> <7F> <FF> <007F> endbfrange 1 beginbfchar <7E> <006600660069> endbfchar endcmap CMapName currentdict /CMap defineresource pop end end"
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 500 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /TrueType /BaseFont /GoRegular /Encoding /WinAnsiEncoding /FirstChar 32 /LastChar 126 /Widths [" + widths + "] /FontDescriptor 6 0 R /ToUnicode 8 0 R >>",
		fixtureStream("BT /F1 16 Tf 30 140 Td (PUBLIC o~ce KEEP) Tj ET", ""),
		"<< /Type /FontDescriptor /FontName /GoRegular /Flags 32 /FontBBox [-100 -300 1100 1100] /ItalicAngle 0 /Ascent 1000 /Descent -300 /CapHeight 700 /StemV 80 /FontFile2 7 0 R >>",
		fixtureStream(string(goregular.TTF), fmt.Sprintf("/Length1 %d", len(goregular.TTF))), fixtureStream(cmap, ""),
	}, "")
	layout, err := TextLayout(input, 0)
	if err != nil {
		t.Fatal(err)
	}
	glyphs := make([]rune, len(layout.Glyphs))
	for i, g := range layout.Glyphs {
		glyphs[i] = g.Rune
	}
	start := strings.Index(string(glyphs), "office")
	if start < 0 {
		t.Fatalf("ligature extraction: %s", string(glyphs))
	}
	quads := layout.Selection(start+2, start+2)
	if len(quads) != 1 {
		t.Fatal("ligature did not select its entire word")
	}
	output, err := RedactAndSanitize(input, []Region{{Page: 0, Quad: quads[0]}})
	if err != nil {
		t.Fatal(err)
	}
	text := extractedText(t, output)
	if strings.Contains(text, "office") || !strings.Contains(strings.Join(strings.Fields(text), ""), "PUBLIC") || !strings.Contains(text, "KEEP") {
		t.Fatalf("ligature redaction: %q", text)
	}
}

func TestIncrementalHistoryIsNotCopied(t *testing.T) {
	previous := textFixture("", "% HISTORY_SECRET\n")
	start := bytes.LastIndex(previous, []byte("startxref\n"))
	var previousXref int
	if _, err := fmt.Sscanf(string(previous[start+10:]), "%d", &previousXref); err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	input.Write(previous)
	objectOffset := input.Len()
	fmt.Fprintf(&input, "5 0 obj\n%s\nendobj\n", fixtureStream("BT /F1 16 Tf 30 140 Td (PUBLIC SECRET KEEP) Tj ET\n", ""))
	xref := input.Len()
	fmt.Fprintf(&input, "xref\n5 1\n%010d 00000 n \ntrailer\n<< /Root 1 0 R /Size 6 /Prev %d >>\nstartxref\n%d\n%%%%EOF\n", objectOffset, previousXref, xref)
	layout, err := TextLayout(input.Bytes(), 0)
	if err != nil {
		t.Fatal(err)
	}
	output, err := RedactAndSanitize(input.Bytes(), regionsFor(layout, "SECRET"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output, []byte("HISTORY_SECRET")) || strings.Contains(decodedObjects(t, output), "SECRET") || bytes.Contains(output, []byte("/Prev")) {
		t.Fatal("export copied sensitive revision history")
	}
}

func TestComposedExportStillRejectsHiddenOriginal(t *testing.T) {
	original := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fixtureStream("BT /F1 16 Tf 30 140 Td (PUBLIC SECRET KEEP) Tj ET", ""),
	}, "")
	composed := textFixture("", "")
	layout, err := TextLayout(composed, 0)
	if err != nil {
		t.Fatal(err)
	}
	output, err := RedactComposedPDF(composed, regionsFor(layout, "SECRET"), [][]byte{original})
	if err == nil || len(output) != 0 {
		t.Fatal("composition bypassed original hidden-layer rejection")
	}
}

func TestInlineStencilCannotBypassImageChecks(t *testing.T) {
	input := textFixture("", "q 300 0 0 200 0 0 cm BI /W 1 /H 1 /BPC 1 /IM true ID \x00 EI Q\n")
	_, err := TextLayout(input, 0)
	if err == nil {
		t.Fatal("inline stencil accepted for text selection")
	}
	// Use geometry from plain text so that export rejection is tested separately.
	layout, err := TextLayout(textFixture("", ""), 0)
	if err != nil {
		t.Fatal(err)
	}
	output, err := RedactAndSanitize(input, regionsFor(layout, "SECRET"))
	if err == nil || len(output) != 0 {
		t.Fatal("inline stencil produced export bytes")
	}
}

func TestAreaRedactionRemovesPixelsFromScanWithoutOCR(t *testing.T) {
	input := scanImageFixture(true, true, "", false)
	layout, err := TextLayout(input, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(layout.Glyphs) != 0 {
		t.Fatal("scan fixture unexpectedly contains OCR")
	}
	area := Quad{UL: Point{.32, .20}, UR: Point{.53, .20}, LL: Point{.32, .34}, LR: Point{.53, .34}}
	output, err := RedactAndSanitize(input, []Region{{Page: 0, Quad: area}})
	if err != nil {
		t.Fatal(err)
	}
	assertRemovedImagePatch(t, output)
	if strings.Contains(decodedObjects(t, output), "RESOURCE_METADATA_SECRET") {
		t.Fatal("scan metadata survived area export")
	}
	if err := api.Validate(context.Background(), bytes.NewReader(output), nil, nil); err != nil {
		t.Fatal(err)
	}
}
