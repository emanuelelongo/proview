//go:build cgo && !nocgo && !extlib

package pdfengine

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gen2brain/go-fitz"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func fixturePDF(objects []string, trailer string) []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R %s >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), trailer, xref)
	return out.Bytes()
}
func fixtureStream(content, attributes string) string {
	return fmt.Sprintf("<< /Length %d %s >>\nstream\n%sendstream", len(content), attributes, content)
}
func textFixture(pageAttributes, extraContent string) []byte {
	return fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R " + pageAttributes + " >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fixtureStream("BT /F1 16 Tf 30 140 Td (PUBLIC SECRET KEEP) Tj 0 -35 Td (SECOND LINE) Tj ET\n0 0 m 20 20 l S\n"+extraContent, ""),
	}, "")
}
func regionsFor(layout Layout, word string) []Region {
	var text strings.Builder
	for _, glyph := range layout.Glyphs {
		text.WriteRune(glyph.Rune)
	}
	index := strings.Index(text.String(), word)
	if index < 0 {
		return nil
	}
	start := len([]rune(text.String()[:index]))
	var result []Region
	for _, glyph := range layout.Glyphs[start : start+len([]rune(word))] {
		result = append(result, Region{Quad: glyph.Quad})
	}
	return result
}
func extractedText(t *testing.T, input []byte) string {
	t.Helper()
	doc, err := fitz.NewFromMemory(input)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	text, err := doc.Text(0)
	if err != nil {
		t.Fatal(err)
	}
	return text
}
func decodedObjects(t *testing.T, input []byte) string {
	t.Helper()
	ctx, err := api.ReadContext(context.Background(), bytes.NewReader(input), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	var result strings.Builder
	for _, entry := range ctx.Table {
		if entry.Free || entry.Object == nil {
			continue
		}
		result.WriteString(entry.Object.PDFString())
		if stream, ok := entry.Object.(types.StreamDict); ok {
			if err := stream.Decode(); err != nil {
				t.Fatal(err)
			}
			result.Write(stream.Content)
		}
	}
	return result.String()
}

func TestProbeNativeTextRedaction(t *testing.T) {
	input := textFixture("", "")
	layout, err := TextLayout(input, 0)
	if err != nil {
		t.Fatal(err)
	}
	regions := regionsFor(layout, "SECRET")
	if len(regions) != 6 {
		t.Fatalf("secret glyphs = %d", len(regions))
	}
	output, err := RedactAndSanitize(input, regions)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, textFixture("", "")) {
		t.Fatal("probe changed source bytes")
	}
	if err := api.Validate(context.Background(), bytes.NewReader(output), nil, nil); err != nil {
		t.Fatal(err)
	}
	text := extractedText(t, output)
	if strings.Contains(text, "SECRET") || !strings.Contains(text, "PUBLIC") || !strings.Contains(text, "KEEP") || !strings.Contains(text, "SECOND LINE") {
		t.Fatalf("unexpected redacted text: %q", text)
	}
	objects := decodedObjects(t, output)
	if strings.Contains(objects, "SECRET") {
		t.Fatal("removed text remains in decoded objects")
	}
	if !strings.Contains(objects, "20 20 l") {
		t.Fatal("unrelated vector path disappeared")
	}
	doc, err := fitz.NewFromMemory(output)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	img, err := doc.ImageDPI(0, 72)
	if err != nil {
		t.Fatal(err)
	}
	p := regions[2].Quad
	x, y := int((p.UL.X+p.LR.X)/2*layout.Width), int((p.UL.Y+p.LR.Y)/2*layout.Height)
	r, g, b, a := img.At(x, y).RGBA()
	if r > 0x1000 || g > 0x1000 || b > 0x1000 || a < 0xf000 {
		t.Fatalf("redaction not opaque black: %d %d %d %d", r, g, b, a)
	}
}

func TestProbeRotatedCropBoxAndUserUnit(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		t.Run(fmt.Sprint(rotation), func(t *testing.T) {
			input := textFixture(fmt.Sprintf("/CropBox [10 20 290 190] /Rotate %d /UserUnit 2", rotation), "")
			layout, err := TextLayout(input, 0)
			if err != nil {
				t.Fatal(err)
			}
			width, height := 560., 340.
			if rotation%180 != 0 {
				width, height = height, width
			}
			if math.Abs(layout.Width-width) > 0.01 || math.Abs(layout.Height-height) > 0.01 {
				t.Fatalf("bounds = %v x %v", layout.Width, layout.Height)
			}
			output, err := RedactAndSanitize(input, regionsFor(layout, "SECRET"))
			if err != nil {
				t.Fatal(err)
			}
			text := extractedText(t, output)
			if strings.Contains(text, "SECRET") || !strings.Contains(text, "PUBLIC") || !strings.Contains(text, "KEEP") {
				t.Fatalf("redacted text = %q", text)
			}
		})
	}
}

func TestProbeCatalogReconstructionRemovesAncillaryData(t *testing.T) {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 6 0 R /Names << /EmbeddedFiles << /Names [(private.txt) 7 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R /Annots [10 0 R] >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fixtureStream("BT /F1 16 Tf 30 140 Td (PUBLIC SECRET KEEP) Tj ET\n", ""),
		fixtureStream("<metadata>METADATA_SECRET</metadata>\n", "/Type /Metadata /Subtype /XML"),
		"<< /Type /Filespec /F (private.txt) /EF << /F 8 0 R >> >>",
		fixtureStream("ATTACHMENT_SECRET\n", "/Type /EmbeddedFile"),
		"<< /Title (TITLE_SECRET) /Author (AUTHOR_SECRET) >>",
		"<< /Type /Annot /Subtype /Text /Rect [250 10 270 30] /Contents (COMMENT_SECRET) >>",
	}
	input := fixturePDF(objects, "/Info 9 0 R")
	layout, err := TextLayout(input, 0)
	if err != nil {
		t.Fatal(err)
	}
	output, err := RedactAndSanitize(input, regionsFor(layout, "SECRET"))
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodedObjects(t, output)
	for _, marker := range []string{"METADATA_SECRET", "ATTACHMENT_SECRET", "TITLE_SECRET", "AUTHOR_SECRET", "COMMENT_SECRET"} {
		if strings.Contains(decoded, marker) {
			t.Errorf("catalog reconstruction retained %s", marker)
		}
	}
}

func TestProbeErrorsAndRepeatedContexts(t *testing.T) {
	if _, err := TextLayout(nil, 0); err == nil {
		t.Fatal("empty PDF accepted")
	}
	if _, err := TextLayout([]byte("not a PDF"), 0); err == nil {
		t.Fatal("malformed PDF accepted")
	}
	input := textFixture("", "")
	if _, err := TextLayout(input, 4); err == nil {
		t.Fatal("out of range page accepted")
	}
	for i := 0; i < 10; i++ {
		layout, err := TextLayout(input, 0)
		if err != nil {
			t.Fatal(err)
		}
		regions := regionsFor(layout, "SECRET")
		if _, err := RedactAndSanitize(input, regions); err != nil {
			t.Fatal(err)
		}
		regions[0].Page = 4
		if _, err := RedactAndSanitize(input, regions); err == nil {
			t.Fatal("invalid redaction page accepted")
		}
	}
}
