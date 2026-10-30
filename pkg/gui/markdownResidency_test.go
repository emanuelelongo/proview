package gui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emanuelelongo/proview/internal/markdown"
)

func TestInactiveMarkdownImagesAreTrimmedAndRestored(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.window.Show()
	defer g.clearMarkdown()

	directory := t.TempDir()
	imagePath := filepath.Join(directory, "image.png")
	file, err := os.Create(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	picture := image.NewRGBA(image.Rect(0, 0, 32, 20))
	picture.Set(2, 3, color.RGBA{R: 90, G: 140, B: 210, A: 255})
	if err := png.Encode(file, picture); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	firstDoc, err := markdown.Load("first.md", filepath.Join(directory, "first.md"), strings.NewReader("![local](image.png)\n"))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(firstDoc, nil)
	first, firstView := g.activeDocument, g.markdownView
	oldViewport := firstView.viewport
	oldRenderer := oldViewport.CreateRenderer()
	oldObjects := oldRenderer.Objects()
	if firstDoc.Image("image.png") == nil || len(firstView.textBlocks) == 0 {
		t.Fatal("Markdown image was not loaded into the active view")
	}

	secondDoc, err := markdown.Load("second.md", "", strings.NewReader("A small text-only document."))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(secondDoc, nil)
	if !firstDoc.ImagesTrimmed() || firstDoc.Image("image.png") != nil {
		t.Fatal("inactive Markdown document retained decoded image resources")
	}
	if !firstView.suspended || firstView.viewport != nil || firstView.outlinePanel != nil || firstView.textBlocks != nil || first.markdownView != firstView {
		t.Fatal("inactive Markdown canvas tree was not released while retaining its entry")
	}
	// Fyne can still traverse and lay out the detached viewport while freeing
	// dirty textures during the next window resize.
	objects := oldRenderer.Objects()
	if len(objects) != len(oldObjects) || objects[0] != oldObjects[0] {
		t.Fatal("detached Markdown renderer lost its canvas children")
	}
	oldRenderer.Layout(oldViewport.Size())

	g.selectDocument(first)
	driver.runNext(t)
	if g.working || g.activeDocument != first || g.markdownView != firstView || firstView.suspended {
		t.Fatal("trimmed Markdown document did not reactivate")
	}
	restored := firstDoc.Image("image.png")
	if restored == nil || restored.Bounds() != picture.Bounds() || color.NRGBAModel.Convert(restored.At(2, 3)) != color.NRGBAModel.Convert(picture.At(2, 3)) {
		t.Fatal("reactivation did not restore the original local image")
	}
}
