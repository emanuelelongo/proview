package gui

import (
	"testing"

	"emanuelelongo/proview/pkg/annotations"
	"fyne.io/fyne/v2"
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
