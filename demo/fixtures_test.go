package demo_test

import (
	"bytes"
	"context"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emanuelelongo/proview/internal/model3d"
	"emanuelelongo/proview/internal/pdfengine"
	"emanuelelongo/proview/pkg/document"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestFixturesExerciseNativePDFAndMerge(t *testing.T) {
	data := readFixture(t, "showcase.pdf")
	if err := api.Validate(context.Background(), bytes.NewReader(data), nil, nil); err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("showcase.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	defer doc.Close()
	if doc.PageCount() != 6 {
		t.Fatalf("pages: %d", doc.PageCount())
	}
	layout, err := pdfengine.CorrectionTextLayout(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, glyph := range layout.Glyphs {
		text.WriteRune(glyph.Rune)
	}
	if !strings.Contains(text.String(), "mountain") {
		t.Fatal("missing editable native text")
	}
	word := strings.Index(text.String(), "mountain")
	if _, err := pdfengine.CorrectText(data, 0, word, len("mountain"), "forest", 0, 0); err != nil {
		t.Fatalf("demo text must be editable: %v", err)
	}
	scan, err := pdfengine.TextLayout(data, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Glyphs) != 0 {
		t.Fatal("scan page must exercise OCR without native text")
	}
	fields, err := pdfengine.ReadForms(data)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[int]bool{}
	for _, field := range fields {
		if field.Page != 2 {
			t.Fatalf("form on unexpected page: %+v", field)
		}
		kinds[field.Kind] = true
	}
	for _, kind := range []int{pdfengine.FormText, pdfengine.FormCheckbox, pdfengine.FormCombo, pdfengine.FormList, pdfengine.FormRadio, pdfengine.FormReset} {
		if !kinds[kind] {
			t.Fatalf("missing field kind %d", kind)
		}
	}
	extra, err := document.Load("merge.pdf", bytes.NewReader(readFixture(t, "merge.pdf")))
	if err != nil {
		t.Fatal(err)
	}
	if err = doc.AppendPDFs(extra); err != nil {
		extra.Close()
		t.Fatal(err)
	}
	if doc.PageCount() != 7 {
		t.Fatalf("merged page count: %d", doc.PageCount())
	}
	for page := 0; page < source.PageCount(); page++ {
		preview, err := source.Thumbnail(page)
		if err != nil {
			t.Fatalf("render page %d: %v", page, err)
		}
		if preview.Bounds().Empty() {
			t.Fatalf("empty page %d", page)
		}
	}
}

func TestFixtureImagesAndModelAreReadable(t *testing.T) {
	for _, name := range []string{"crop.jpg", "scan.png", "johndoe.png", "lenna.png", "transparent.png", "qr.png"} {
		img, _, err := image.Decode(bytes.NewReader(readFixture(t, name)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if img.Bounds().Empty() {
			t.Fatalf("empty image: %s", name)
		}
		if name == "transparent.png" {
			_, _, _, alpha := img.At(0, 0).RGBA()
			if alpha != 0 {
				t.Fatal("transparent fixture lost alpha")
			}
		}
	}
	if _, err := model3d.Load("model.stl", bytes.NewReader(readFixture(t, "model.stl"))); err != nil {
		t.Fatalf("unreadable demo model: %v", err)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
