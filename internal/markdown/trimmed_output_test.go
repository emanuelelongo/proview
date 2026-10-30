package markdown

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrimmedDocumentOutputUsesTransientImages(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "image.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 40, 30))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	doc, err := Load("test.md", filepath.Join(dir, "test.md"), strings.NewReader("![image](image.png)\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	active, err := doc.PrintPDF(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	doc.TrimImages()
	snapshot := doc.resourceSnapshot()
	if snapshot == doc || snapshot.Image("image.png") == nil {
		t.Fatal("trimmed output snapshot has no decoded image")
	}
	trimmed, err := doc.PrintPDF(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(active.Data, []byte("/Subtype /Image")) || !bytes.Contains(trimmed.Data, []byte("/Subtype /Image")) {
		t.Fatal("trimmed output replaced the image with a placeholder")
	}
	if !doc.ImagesTrimmed() || doc.HasDecodedImages() {
		t.Fatal("output restored images into the inactive document")
	}
}
