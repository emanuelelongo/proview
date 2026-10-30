package gui

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"

	"emanuelelongo/proview/internal/markdown"
	"emanuelelongo/proview/pkg/document"
)

func TestSwitchingToMarkdownReleasesPDFView(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.closeWindow()
	md, err := markdown.Load("notes.md", "", strings.NewReader("# Notes"))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(md, nil)
	notes := g.activeDocument
	g.addDocument(workspacePDF(t, 3), nil, nil)
	if len(g.pdfPageImages) == 0 || len(g.signatureLayers) == 0 {
		t.Fatal("PDF view was not built")
	}
	g.selectDocument(notes)
	driver.drain()
	if g.activeDocument != notes || g.markdownView == nil {
		t.Fatal("Markdown document was not activated")
	}
	if g.pdfPageImages != nil || g.pdfThumbImages != nil || len(g.signatureLayers) != 0 || len(g.pdfPages.Objects) != 0 || len(g.thumbList.Objects) != 0 || g.previewRenderer != nil || g.image.Image != nil {
		t.Fatal("inactive PDF view stayed referenced by the Markdown view")
	}
}

func TestSwitchingAwayFromImageReleasesFullSizeThumbnailSource(t *testing.T) {
	g, _ := newDropTestGui(t)
	defer g.closeWindow()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1600, 1200))); err != nil {
		t.Fatal(err)
	}
	source, err := document.LoadBytes("large.png", "", encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	g.addDocument(doc, nil, nil)
	entry := g.activeDocument
	if entry.thumbnailSource == nil || entry.thumbnail == nil {
		t.Fatal("active image thumbnail was not captured")
	}
	md, err := markdown.Load("notes.md", "", strings.NewReader("# Notes"))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(md, nil)
	notes := g.activeDocument
	for range 3 {
		if entry.thumbnailSource != nil {
			t.Fatal("inactive image kept the full-size preview through thumbnail identity")
		}
		if entry.thumbnail == nil || entry.thumbnail.Bounds().Dx() > maxThumbnailPixelWidth {
			t.Fatal("inactive image lost its bounded sidebar thumbnail")
		}
		_, rasterBytes := doc.MemoryUsage()
		if rasterBytes != 0 {
			t.Fatalf("inactive image kept %d decoded raster bytes", rasterBytes)
		}
		g.selectDocument(entry)
		if g.image.Image == nil || entry.thumbnailSource == nil {
			t.Fatal("image did not restore after being trimmed")
		}
		g.selectDocument(notes)
	}
}
