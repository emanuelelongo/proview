package gui

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"emanuelelongo/proview/pkg/document"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestStartupFileReaderUsesTheSharedDocumentLoader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image with spaces.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	input := image.NewNRGBA(image.Rect(0, 0, 12, 8))
	input.SetNRGBA(3, 4, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(file, input); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	reader, err := startupFileReader(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := loadDocumentReader(reader)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	if doc.Name() != filepath.Base(path) || doc.Format() != document.FormatPNG || doc.Location() == "" {
		t.Fatalf("loaded startup document = name %q, format %q, location %q", doc.Name(), doc.Format(), doc.Location())
	}
}

func TestStartupFileReaderRejectsMissingFile(t *testing.T) {
	reader, err := startupFileReader(filepath.Join(t.TempDir(), "missing.pdf"))
	if err == nil || reader != nil {
		t.Fatalf("missing startup file returned reader %v and error %v", reader, err)
	}
}

func TestOpenStartupFilesPreservesExistingDocumentWhenPathIsMissing(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	existing := testDocument(t, 20, 20)
	g.showDocument(existing)

	g.openStartupFiles([]string{filepath.Join(t.TempDir(), "missing.pdf")})

	if g.document != existing {
		t.Fatal("opening a missing file replaced the current document")
	}
}

func TestStartupJPEGFileReaderHandlesFinderPaths(t *testing.T) {
	for _, name := range []string{"IMG_2064.jpeg", "image with spaces.JPG", "immagine à 東京.jpeg"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := jpeg.Encode(file, image.NewRGBA(image.Rect(0, 0, 12, 8)), nil); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			reader, err := startupFileReader(path)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := loadDocumentReader(reader)
			if err != nil {
				t.Fatal(err)
			}
			defer doc.Close()
			if doc.Format() != document.FormatJPEG || doc.Name() != name {
				t.Fatalf("loaded name %q, format %q", doc.Name(), doc.Format())
			}
		})
	}
}
