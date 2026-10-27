package gui

import (
	"emanuelelongo/proview/internal/markdown"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

func TestMarkdownThumbnailRendersExcerptAfterSwitchWithoutCanvasCapture(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.closeWindow()
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	doc, err := markdown.Load("long.md", "", strings.NewReader("# Recognizable heading\n\n"+strings.Repeat("Opening paragraph with readable words.\n\n", 200)))
	if err != nil {
		t.Fatal(err)
	}
	g.window = markdownThumbnailNoCaptureWindow{Window: g.window, canvas: markdownThumbnailNoCaptureCanvas{Canvas: g.window.Canvas(), t: t}}
	g.addMarkdown(doc, nil)
	entry := g.activeDocument
	g.markdownView.zoom(2)
	g.window.Resize(fyne.NewSize(700, 500))
	g.addDocument(testDocument(t, 40, 30), nil, nil)
	for entry.thumbnailCancel != nil {
		driver.runNext(t)
	}
	if entry.thumbnail == nil || entry.thumbnail.Bounds().Size() != image.Pt(320, 240) {
		t.Fatal("background excerpt was lost after switching documents")
	}
}

type markdownThumbnailNoCaptureWindow struct {
	fyne.Window
	canvas fyne.Canvas
}

func (w markdownThumbnailNoCaptureWindow) Canvas() fyne.Canvas { return w.canvas }

type markdownThumbnailNoCaptureCanvas struct {
	fyne.Canvas
	t *testing.T
}

func (c markdownThumbnailNoCaptureCanvas) Capture() image.Image {
	c.t.Fatal("thumbnail captured the live canvas")
	return nil
}

func TestMarkdownThumbnailRetainsRenderedContentAcrossSwitches(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	g.addMarkdown(readingDocument(t), nil)
	entry := g.activeDocument
	settleMarkdownDiagrams(t, g, driver)
	for entry.thumbnail == nil {
		driver.runNext(t)
	}
	thumbnail := entry.thumbnail
	bounds := thumbnail.Bounds()
	if bounds.Dx() < 80 || bounds.Dx() > 320 || bounds.Dy() < 80 || bounds.Dy() > 240 {
		t.Fatalf("invalid thumbnail size: %v", bounds)
	}
	colors := map[color.NRGBA]bool{}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			colors[color.NRGBAModel.Convert(thumbnail.At(x, y)).(color.NRGBA)] = true
		}
	}
	if len(colors) < 16 {
		t.Fatal("thumbnail does not contain rendered content")
	}
	g.markdownView.scroll.ScrollToOffset(fyne.NewPos(0, 150))
	offset := g.markdownView.scroll.Offset
	g.addDocument(testDocument(t, 40, 30), nil, nil)
	found := false
	for _, object := range test.LaidOutObjects(g.documentList) {
		if preview, ok := object.(*canvas.Image); ok && preview.Image == thumbnail {
			found = true
		}
	}
	if !found || entry.thumbnail != thumbnail {
		t.Fatal("Markdown thumbnail was lost when switching to an image")
	}
	if path := os.Getenv("PROVIEW_MARKDOWN_THUMBNAIL_PREVIEW"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, g.window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	g.selectDocument(entry)
	settleMarkdownDiagrams(t, g, driver)
	if entry.thumbnail != thumbnail || g.markdownView.scroll.Offset != offset {
		t.Fatal("thumbnail regeneration changed the retained reading state")
	}
	g.closeWindow()
	driver.drain()
}

func TestMarkdownThumbnailWorkerIgnoresClosedDocument(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	openTestMermaid(t, g, "flowchart LR\n A --> B")
	entry := g.activeDocument
	g.refreshDocumentSidebar()
	g.closeWindow()
	for entry.thumbnailCancel != nil {
		driver.runNext(t)
	}
	if entry.thumbnail != nil {
		t.Fatal("closed Markdown published a thumbnail")
	}
}
