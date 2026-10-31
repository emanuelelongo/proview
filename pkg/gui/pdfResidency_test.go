package gui

import (
	"image"
	"testing"

	"emanuelelongo/proview/pkg/annotations"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestPDFResidencyReleasesDistantPagesAndRestoresOverlays(t *testing.T) {
	g, driver := newDropTestGui(t)
	doc := workspacePDF(t, 60)
	g.addDocument(doc, nil, nil)
	g.window.Resize(fyne.NewSize(500, 400))
	g.sidebarSplit.Resize(fyne.NewSize(500, 400))
	g.sidebarSplit.Refresh()
	layer := g.signatureLayers[0]
	data := annotations.Default(annotations.Shape)
	data.Shape, data.PageWidth, data.PageHeight = annotations.Rectangle, 72, 72
	data.Points = []annotations.Point{{X: 10, Y: 10}, {X: 30, Y: 30}}
	g.addPageAnnotation(layer, data, 0, 0, nil)
	for g.annotationPending > 0 {
		driver.runNext(t)
	}
	before := g.pdfPageTop(50)
	g.pdfScroll.ScrollToOffset(fyne.NewPos(0, before))
	g.loadVisiblePDFPages()
	for !g.pdfPageLoaded[50] {
		driver.runNext(t)
	}
	if g.pdfPageImages[0].Image != nil || g.pdfPageLoaded[0] || layer.Visible() {
		t.Fatal("distant page retained editor pixels or widgets")
	}
	if len(g.pdfResident) > 10 {
		t.Fatalf("too many resident pages: %d", len(g.pdfResident))
	}
	if g.pdfPageTop(50) != before || len(doc.Overlays()) != 1 || !doc.CanUndo() {
		t.Fatal("eviction changed geometry or history")
	}
	g.pdfScroll.ScrollToOffset(fyne.NewPos(0, 0))
	g.loadVisiblePDFPages()
	for !g.pdfPageLoaded[0] {
		driver.runNext(t)
	}
	if !layer.Visible() || len(layer.widgets) != 1 || g.pdfPageImages[0].Image == nil {
		t.Fatal("returning to page did not restore artwork")
	}
	// A selected object pins its page while its controls remain active.
	g.selectSignature(layer, doc.Overlays()[0].ID)
	g.pdfScroll.ScrollToOffset(fyne.NewPos(0, before))
	g.loadVisiblePDFPages()
	if g.pdfPageImages[0].Image == nil {
		t.Fatal("selection page was evicted during editing")
	}
	g.closeWindow()
}

func TestPDFResidencyReleasesSidebarRastersIndependently(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.addDocument(workspacePDF(t, 60), nil, nil)
	g.window.Resize(fyne.NewSize(500, 400))
	g.sidebarSplit.Resize(fyne.NewSize(500, 400))
	g.sidebarSplit.Refresh()
	top := float32(0)
	for i := 0; i < 50; i++ {
		top += g.thumbList.Objects[i].MinSize().Height + theme.Padding()
	}
	g.sidebar.container.ScrollToOffset(fyne.NewPos(0, top))
	g.loadVisiblePDFThumbnails()
	for !g.pdfThumbLoaded[50] {
		driver.runNext(t)
	}
	if g.pdfThumbImages[0].Image != nil || g.pdfThumbLoaded[0] {
		t.Fatal("distant sidebar raster remained resident")
	}
	if g.pdfPageImages[0].Image == nil {
		t.Fatal("sidebar scrolling evicted the visible editor page")
	}
	g.sidebar.container.ScrollToOffset(fyne.NewPos(0, 0))
	g.loadVisiblePDFThumbnails()
	for !g.pdfThumbLoaded[0] {
		driver.runNext(t)
	}
	if g.pdfThumbImages[0].Image.Bounds().Dx() != 72 {
		t.Fatal("returning thumbnail did not use sidebar density")
	}
	g.closeWindow()
}

func TestPDFResidencyOversizedPages(t *testing.T) {
	g := &Gui{
		pdfPageSizes: []image.Point{
			image.Pt(5000, 3000), // 15 MP (oversized)
			image.Pt(1000, 1000), // 1 MP (normal)
			image.Pt(5000, 3000), // 15 MP (oversized)
		},
	}

	if !g.isOversizedPDFPage(0) {
		t.Fatal("expected page 0 to be oversized")
	}
	if g.isOversizedPDFPage(1) {
		t.Fatal("expected page 1 to be normal")
	}
	if !g.isOversizedPDFPage(2) {
		t.Fatal("expected page 2 to be oversized")
	}

	// Viewport is on page 1: [1, 2). Normal keep window is [0, 3).
	g.pdfVisibleStart, g.pdfVisibleEnd = 1, 2
	g.pdfKeepStart, g.pdfKeepEnd = 0, 3

	// Normal page 1 is wanted (both visible and in keep window).
	if !g.pdfPageWanted(1) {
		t.Fatal("expected visible normal page 1 to be wanted")
	}

	// Oversized page 0 is within keep window [0, 3) but outside visible range [1, 2).
	// It must NOT be wanted!
	if g.pdfPageWanted(0) {
		t.Fatal("expected offscreen oversized page 0 to NOT be wanted")
	}

	// Oversized page 2 is also offscreen and outside visible range [1, 2).
	if g.pdfPageWanted(2) {
		t.Fatal("expected offscreen oversized page 2 to NOT be wanted")
	}

	// When visible [0, 1), oversized page 0 IS wanted.
	g.pdfVisibleStart, g.pdfVisibleEnd = 0, 1
	if !g.pdfPageWanted(0) {
		t.Fatal("expected visible oversized page 0 to be wanted")
	}
}

func TestDetachDocumentViewClearsPDFImageReferences(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	preview := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	page := canvas.NewImageFromImage(preview)
	thumbnail := canvas.NewImageFromImage(preview)
	g.pdfPageImages = []*canvas.Image{page}
	g.pdfThumbImages = []*canvas.Image{thumbnail}
	g.pdfPages.Add(page)
	g.thumbList.Add(thumbnail)
	g.detachDocumentView()
	if page.Image != nil || thumbnail.Image != nil {
		t.Fatal("detaching the PDF view retained raster pixels")
	}
}

func TestSidebarThumbnailRangeUsesIndexedGeometry(t *testing.T) {
	list := container.NewVBox()
	heights := []float32{24, 48, 36, 60, 30}
	for _, height := range heights {
		card := canvas.NewRectangle(nil)
		card.SetMinSize(fyne.NewSize(100, height))
		list.Add(card)
	}
	scroll := container.NewVScroll(list)
	scroll.Resize(fyne.NewSize(180, 90))
	g := &Gui{pdfGeneration: 3, thumbList: list, sidebar: &Sidebar{container: scroll}}
	defer g.clearSidebarThumbnailIndex()
	for _, viewport := range [][2]float32{{0, 40}, {32, 90}, {120, 200}, {500, 600}} {
		top := float32(0)
		first, end := len(heights), len(heights)
		for i, height := range heights {
			next := top + height + theme.Padding()
			if next-theme.Padding() > viewport[0] && first == len(heights) {
				first = i
			}
			if top >= viewport[1] {
				end = i
				break
			}
			top = next
		}
		if end < first {
			end = first
		}
		gotFirst, gotEnd := g.sidebarThumbnailRange(viewport[0], viewport[1])
		if gotFirst != first || gotEnd != end {
			t.Fatalf("range for %v = [%d,%d), want [%d,%d)", viewport, gotFirst, gotEnd, first, end)
		}
	}
}
