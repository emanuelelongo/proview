package gui

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emanuelelongo/proview/internal/markdown"
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

func animationFixture(t *testing.T) []byte {
	t.Helper()
	palette := color.Palette{color.Black, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 20, 10), palette)
	second := image.NewPaletted(first.Bounds(), palette)
	for i := range second.Pix {
		second.Pix[i] = 1
	}
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestGIFImagePlaybackEditingUndoAndLifetime(t *testing.T) {
	g, _ := newDropTestGui(t)
	defer g.closeWindow()
	data := animationFixture(t)
	source, err := document.Load("animation.gif", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	g.addDocument(doc, nil, nil)
	p := g.gifPlayback
	if p == nil || p.player == nil {
		t.Fatal("GIF playback not started")
	}
	p.player.Pause()
	now := time.Now().Add(time.Second)
	g.tickGIFPlayback(p, now)
	g.tickGIFPlayback(p, now.Add(time.Second))
	if color.GrayModel.Convert(g.image.Image.At(0, 0)).(color.Gray).Y != 255 {
		t.Fatal("second GIF frame not displayed")
	}
	base, _ := doc.Preview(0)
	if color.GrayModel.Convert(base.At(0, 0)).(color.Gray).Y != 0 || doc.Dirty() || !bytes.Equal(doc.Original(), data) {
		t.Fatal("playback mutated source/history")
	}
	g.startCrop()
	if !p.paused || color.GrayModel.Convert(g.image.Image.At(0, 0)).(color.Gray).Y != 0 {
		t.Fatal("crop did not freeze immutable first frame")
	}
	g.cropOverlay.Deactivate()
	if err := doc.TransformImage("right"); err != nil {
		t.Fatal(err)
	}
	g.syncDocumentView()
	if g.gifPlayback != nil || doc.Animation() != nil {
		t.Fatal("editing still animates")
	}
	g.undo()
	if g.gifPlayback == nil || doc.Animation() == nil {
		t.Fatal("Undo did not restore animation")
	}
	g.addDocument(testDocument(t, 10, 10), nil, nil)
	before := g.image.Image
	p.clock.Tick(.5)
	if g.gifPlayback != nil || g.image.Image != before {
		t.Fatal("stale GIF changed another document")
	}
	g.selectDocument(g.documents[0])
	if g.gifPlayback == nil {
		t.Fatal("returning to GIF did not restart playback")
	}
	g.closeDocument()
	if g.gifPlayback != nil {
		t.Fatal("closing GIF retained its clock")
	}
}

func TestGIFMarkdownLinkedImagesVisibilityPreviewAndStaticOutput(t *testing.T) {
	g, _ := newDropTestGui(t)
	defer g.closeWindow()
	g.window.Resize(fyne.NewSize(700, 700))
	g.window.Show()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "demo.gif"), animationFixture(t), 0600); err != nil {
		t.Fatal(err)
	}
	source := "<a href=\"demo.gif\">\n<strong>Watch</strong><img src=\"demo.gif\" alt=\"Demo\" width=\"200\">\n</a>\n\n![Again](demo.gif)\n\n" + strings.Repeat("Long reading paragraph.\n\n", 40)
	doc, err := markdown.Load("read.md", filepath.Join(dir, "read.md"), strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Animations) != 1 {
		t.Fatal("GIF animation not loaded")
	}
	g.addMarkdown(doc, nil)
	g.window.Canvas().Capture()
	v := g.markdownView
	p := g.gifPlayback
	if p == nil || len(v.animatedImages) != 2 || len(v.gifGroups) != 1 {
		t.Fatal("linked or repeated GIF missing")
	}
	segment := v.animatedImages[0]
	if segment.visual == nil || segment.block == nil || !segment.block.positioned {
		t.Fatal("GIF was not laid out")
	}
	bold := false
	for _, child := range segment.block.Segments {
		if text, ok := child.(*widget.TextSegment); ok && strings.Contains(text.Text, "Watch") && text.Style.TextStyle.Bold {
			bold = true
		}
	}
	if !bold || len(segment.block.links) == 0 {
		t.Fatal("HTML link lost formatting or click ranges")
	}
	if segment.player != v.animatedImages[1].player {
		t.Fatal("repeated GIF duplicates player buffers")
	}
	segment.player.Pause()
	now := time.Now().Add(time.Second)
	g.tickGIFPlayback(p, now)
	g.tickGIFPlayback(p, now.Add(time.Second))
	if color.GrayModel.Convert(segment.visual.Image.At(0, 0)).(color.Gray).Y != 255 {
		t.Fatal("Markdown GIF did not advance")
	}
	if color.GrayModel.Convert(doc.Images["demo.gif"].At(0, 0)).(color.Gray).Y != 0 {
		t.Fatal("print/thumbnail first frame changed")
	}
	before := segment.image
	v.scroll.ScrollToBottom()
	g.window.Canvas().Capture()
	g.tickGIFPlayback(p, now.Add(2*time.Second))
	g.tickGIFPlayback(p, now.Add(3*time.Second))
	if segment.image != before {
		t.Fatal("offscreen GIF kept advancing")
	}
	v.showImagePreview(segment.image)
	preview := v.imagePreview
	if preview == nil || preview.animation != segment.player {
		t.Fatal("enlarged GIF lost animation")
	}
	g.tickGIFPlayback(p, now.Add(4*time.Second))
	g.tickGIFPlayback(p, now.Add(5*time.Second))
	if preview.picture.Image != segment.image || segment.image == before {
		t.Fatal("enlarged GIF did not animate")
	}
	g.setMarkdownTheme("dark")
	if !preview.closed || v.imagePreview != nil {
		t.Fatal("theme rebuild retained an obsolete preview player")
	}
	if g.gifPlayback == nil || g.gifPlayback == p {
		t.Fatal("theme rebuild did not replace clock")
	}
	g.clearMarkdown()
	p.clock.Tick(.5)
	if g.gifPlayback != nil {
		t.Fatal("Markdown teardown retained clock")
	}
}
