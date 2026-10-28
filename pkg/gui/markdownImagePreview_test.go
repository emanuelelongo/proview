package gui

import (
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emanuelelongo/proview/internal/markdown"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestMarkdownImagePreviewZoomAndLifetime(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.window.Resize(fyne.NewSize(800, 740))
	g.window.Show()
	path, _ := filepath.Abs("../../README.md")
	doc, err := markdown.Load("README.md", path, strings.NewReader("![Icon](assets/app-icon.png)\n\n"+strings.Repeat("Reading text.\n\n", 30)))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(doc, nil)
	defer g.clearMarkdown()
	g.window.Canvas().Capture()
	v := g.markdownView
	readingScale, offset := v.scale, v.scroll.Offset
	entry := g.activeDocument
	baseline := len(driver.AllWindows())
	var block *markdownSelectableText
	var position fyne.Position
	for _, candidate := range v.textBlocks {
		for _, object := range candidate.renderer.WidgetRenderer.Objects() {
			if picture, ok := object.(*canvas.Image); ok {
				block = candidate
				position = picture.Position().Add(fyne.NewPos(picture.Size().Width/2, picture.Size().Height/2))
			}
		}
	}
	if block == nil {
		t.Fatal("image missing")
	}
	block.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: position}})
	if block.Cursor() != desktop.PointerCursor {
		t.Fatal("image has no pointer cursor")
	}
	block.Tapped(&fyne.PointEvent{Position: position})
	p := v.imagePreview
	if p == nil || p.picture.Image != doc.Images["assets/app-icon.png"] || len(driver.AllWindows()) != baseline {
		t.Fatal("image preview not opened from original pixels")
	}
	g.window.Canvas().Capture()
	fitted := p.scale
	g.zoomIn() // Native menu shortcuts must zoom the overlay, not the reader.
	if p.scale <= fitted || p.fitted {
		t.Fatal("preview zoom did not enlarge the image")
	}
	p.overlay.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.Key1, Modifier: fyne.KeyModifierShortcutDefault})
	if p.scale != 1 {
		t.Fatal("100 percent view unavailable")
	}
	before := p.scroll.Offset
	interaction := &markdownPreviewInteraction{preview: p}
	interaction.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(-30, -30)})
	if p.scroll.Offset == before {
		t.Fatal("enlarged image cannot be panned")
	}
	anchor := fyne.NewPos(p.viewport.Size().Width/2, p.viewport.Size().Height/2)
	beforePoint := anchor.Add(p.scroll.Offset)
	beforePoint.X /= p.scale
	beforePoint.Y /= p.scale
	interaction.scrollWithModifiers(&fyne.ScrollEvent{PointEvent: fyne.PointEvent{Position: anchor}, Scrolled: fyne.NewDelta(0, 1)}, fyne.KeyModifierShortcutDefault)
	afterPoint := anchor.Add(p.scroll.Offset)
	afterPoint.X /= p.scale
	afterPoint.Y /= p.scale
	if math.Abs(float64(beforePoint.X-afterPoint.X)) > 1 || math.Abs(float64(beforePoint.Y-afterPoint.Y)) > 1 {
		t.Fatal("wheel zoom lost the point under the pointer")
	}
	g.resetZoom()
	g.window.Resize(fyne.NewSize(1800, 1100))
	g.window.Canvas().Capture()
	available := g.window.Canvas().Size()
	if p.viewport.Size() != fyne.NewSize(available.Width-48, available.Height-48) {
		t.Fatalf("preview does not use the full window: %v in %v", p.viewport.Size(), available)
	}
	if math.Abs(float64(p.scale-p.fitScale())) > .001 {
		t.Fatal("fit did not expand with the window")
	}
	g.zoomIn()
	if p.viewport.Size().Width < 1700 || p.viewport.Size().Height < 1000 {
		t.Fatal("zoom restored the fixed preview bounds")
	}
	g.resetZoom()
	g.window.Resize(fyne.NewSize(700, 600))
	g.window.Canvas().Capture()
	if p.imageSize().Width > p.viewport.Size().Width+1 || p.imageSize().Height > p.viewport.Size().Height+1 {
		t.Fatal("fit did not follow preview resize")
	}
	if !p.fitted || math.Abs(float64(p.scale-p.fitScale())) > .001 {
		t.Fatal("fit to window failed")
	}
	// Repeated native resize events must preserve manual zoom and its center.
	p.setScale(4, nil)
	center := func() fyne.Position {
		size := p.viewport.Size()
		return p.scroll.Offset.Add(fyne.NewPos(size.Width/2, size.Height/2))
	}
	anchorBeforeResize := center()
	for _, size := range []fyne.Size{fyne.NewSize(900, 800), fyne.NewSize(650, 500), fyne.NewSize(1200, 900)} {
		g.window.Resize(size)
		g.window.Canvas().Capture()
		point := center()
		if p.scale != 4 || p.fitted || math.Abs(float64(point.X-anchorBeforeResize.X)) > 1 || math.Abs(float64(point.Y-anchorBeforeResize.Y)) > 1 {
			t.Fatalf("resize lost manual zoom/center: %v -> %v at %v", anchorBeforeResize, point, size)
		}
	}
	g.resetZoom()
	for _, size := range []fyne.Size{fyne.NewSize(900, 600), fyne.NewSize(700, 800), fyne.NewSize(1100, 700)} {
		g.window.Resize(size)
		g.window.Canvas().Capture()
		if !p.fitted || p.scroll.Offset != (fyne.Position{}) || p.picture.Size() != p.imageSize() || math.Abs(float64(p.scale-p.fitScale())) > .001 {
			t.Fatal("resize left stale fitted image geometry")
		}
	}
	if path := os.Getenv("PROVIEW_IMAGE_PREVIEW"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, g.window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	p.overlay.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if v.imagePreview != nil || len(driver.AllWindows()) != baseline {
		t.Fatal("Escape did not close preview")
	}
	if g.activeDocument != entry || v.scale != readingScale || v.scroll.Offset != offset || len(g.documents) != 1 {
		t.Fatal("preview changed the reading workspace")
	}
	block.Tapped(&fyne.PointEvent{Position: position})
	if v.imagePreview == nil {
		t.Fatal("preview could not reopen")
	}
	g.window.Canvas().Capture()
	fyneTest.TapCanvas(g.window.Canvas(), fyne.NewPos(1, 1))
	if v.imagePreview != nil || g.window.Canvas().Overlays().Top() != nil {
		t.Fatal("outside click did not dismiss preview")
	}
	block.Tapped(&fyne.PointEvent{Position: position})
	g.window.Canvas().Capture()
	inside := fyne.NewPos(v.imagePreview.viewport.Size().Width/2, v.imagePreview.viewport.Size().Height/2)
	// The native driver preserves focused ancestors on child taps. TapCanvas's
	// simplified focus handling unfocuses non-focusable children, so exercise
	// the child event directly while retaining the overlay's keyboard focus.
	(&markdownPreviewInteraction{preview: v.imagePreview}).Tapped(&fyne.PointEvent{Position: inside})
	if v.imagePreview == nil {
		t.Fatal("clicking the image dismissed it")
	}
	g.requestCloseDocument()
	if v.imagePreview != nil || g.activeDocument != entry {
		t.Fatal("Close shortcut changed source document instead of dismissing preview")
	}
	block.Tapped(&fyne.PointEvent{Position: position})
	g.window.Canvas().Focus(nil)
	if v.imagePreview != nil {
		t.Fatal("focus loss did not dismiss preview")
	}
	block.Tapped(&fyne.PointEvent{Position: position})
	g.addDocument(testDocument(t, 10, 10), nil, nil)
	if v.imagePreview != nil {
		t.Fatal("document switch left preview open")
	}
	g.closeWindow()
}

func TestMarkdownLinkCursorTracksWrappedText(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.window.Resize(fyne.NewSize(500, 740))
	g.window.Show()
	doc, err := markdown.Load("links.md", "", strings.NewReader("Ordinary text. ["+strings.Repeat("long link label ", 10)+"](https://example.com)."))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(doc, nil)
	defer g.clearMarkdown()
	g.window.Canvas().Capture()
	block := g.markdownView.textBlocks[0]
	links, ordinary := 0, 0
	for _, fragment := range block.fragments() {
		position := fragment.position.Add(fyne.NewPos(2, fragment.size.Height/2))
		block.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: position}})
		if fragment.start >= block.links[0].start && fragment.start < block.links[0].end {
			links++
			if block.Cursor() != desktop.PointerCursor {
				t.Fatal("wrapped link does not show hand cursor")
			}
		} else {
			ordinary++
			if block.Cursor() != desktop.TextCursor {
				t.Fatal("ordinary text retained hand cursor")
			}
		}
	}
	block.MouseOut()
	if links < 2 || ordinary < 1 || block.Cursor() != desktop.TextCursor {
		t.Fatal("cursor did not track link boundaries")
	}
}
