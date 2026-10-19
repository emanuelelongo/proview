package gui

import (
	"image"
	"testing"
	"time"

	"fyne.io/fyne/v2/canvas"
)

func TestThumbnailRefreshDefersCoalescesAndUsesSidebarDensity(t *testing.T) {
	g, driver := newDropTestGui(t)
	doc := workspacePDF(t, 2)
	g.document = doc
	g.pdfThumbImages = []*canvas.Image{canvas.NewImageFromImage(image.NewNRGBA(image.Rect(0, 0, 1, 1))), canvas.NewImageFromImage(nil)}
	g.pdfThumbLoaded = make([]bool, 2)
	t.Cleanup(func() { g.clearThumbnailRefresh(); g.closed = true })
	original := g.pdfThumbImages[0].Image
	for range 10 {
		g.refreshPagePreview(0)
	}
	g.refreshPagePreview(1)
	s := g.thumbnailRefresh
	if s == nil || len(s.pending) != 2 || s.running || g.pdfThumbImages[0].Image != original {
		t.Fatal("thumbnail edits rendered eagerly or were not coalesced")
	}
	if s.timer == nil {
		t.Fatal("deferred updates have no wakeup")
	}
	s.timer.Stop()
	s.timer = nil
	// Even an explicit early wakeup must leave existing thumbnails visible.
	g.renderThumbnailRefresh(s)
	if s.running || g.pdfThumbImages[0].Image != original {
		t.Fatal("thumbnail rendered before idle deadline")
	}
	s.timer.Stop()
	s.timer = nil
	// Holding an editing gesture keeps the worker idle after its deadline.
	s.after = time.Time{}
	g.working = true
	g.renderThumbnailRefresh(s)
	if s.running || !s.after.After(time.Now()) {
		t.Fatal("editing did not defer thumbnail work")
	}
	g.working = false
	s.timer.Stop()
	s.timer = nil
	s.after = time.Time{}
	g.renderThumbnailRefresh(s)
	for s.running {
		driver.runNext(t)
	}
	if !g.pdfThumbLoaded[0] {
		t.Fatal("completed thumbnail did not publish")
	}
	size, err := doc.PageSize(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.pdfThumbImages[0].Image.Bounds().Size(); got != image.Pt(size.X/2, size.Y/2) {
		t.Fatalf("thumbnail rendered at page density: %v", got)
	}
	if len(s.pending) != 1 {
		t.Fatal("worker dropped updates for another page")
	}
	s.timer.Stop()
	s.timer = nil
	g.renderThumbnailRefresh(s)
	for s.running {
		driver.runNext(t)
	}
	if !g.pdfThumbLoaded[1] || len(s.pending) != 0 {
		t.Fatal("remaining page did not update")
	}
}

func TestThumbnailRefreshRejectsObsoleteRevisionAndDocument(t *testing.T) {
	g, driver := newDropTestGui(t)
	doc := workspacePDF(t, 1)
	g.document = doc
	original := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	g.pdfThumbImages = []*canvas.Image{canvas.NewImageFromImage(original)}
	g.pdfThumbLoaded = make([]bool, 1)
	t.Cleanup(func() { g.clearThumbnailRefresh(); g.closed = true })
	g.refreshPagePreview(0)
	s := g.thumbnailRefresh
	s.timer.Stop()
	s.timer = nil
	s.after = time.Time{}
	g.renderThumbnailRefresh(s)
	if err := doc.TransformPDFPage(0, "right"); err != nil {
		t.Fatal(err)
	}
	for s.running {
		driver.runNext(t)
	}
	if g.pdfThumbImages[0].Image != original || len(s.pending) != 1 {
		t.Fatal("obsolete revision published or lost its retry")
	}
	s.timer.Stop()
	s.timer = nil
	s.after = time.Time{}
	g.renderThumbnailRefresh(s)
	g.clearThumbnailRefresh()
	for s.running {
		driver.runNext(t)
	}
	if g.pdfThumbImages[0].Image != original {
		t.Fatal("obsolete view received a thumbnail")
	}
}
