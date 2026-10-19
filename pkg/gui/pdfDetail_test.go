package gui

import (
	"emanuelelongo/proview/pkg/annotations"
	"image"
	"math"
	"testing"
)

func TestPDFDisplayResolutionMatchesDensityWithinLimits(t *testing.T) {
	size := image.Pt(1000, 1400)
	if pdfDisplayDPI(size, .5, 2) != 144 {
		t.Fatal("fit preview needlessly enlarged")
	}
	if pdfDisplayDPI(size, 1, 2) != 288 {
		t.Fatal("Retina density ignored")
	}
	if pdfDisplayDPI(size, 1.5, 2) != 432 {
		t.Fatal("zoom density ignored")
	}
	for _, size := range []image.Point{image.Pt(1200, 1600), image.Pt(1500, 2000), image.Pt(400, 10000)} {
		dpi := pdfDisplayDPI(size, 8, 2)
		scale := dpi / 144
		if dpi > 144 && (dpi > 600 || float64(size.X)*float64(size.Y)*scale*scale > 16_000_001 || float64(max(size.X, size.Y))*scale > 8192.01) {
			t.Fatalf("preview density exceeds bounds: %v dpi=%g", size, dpi)
		}
	}
	if dpi := pdfDisplayDPI(image.Pt(20000, 20000), 8, 2); dpi != 144 {
		t.Fatal("oversized page should keep its ordinary preview without another allocation")
	}
}

func TestPDFDetailChangesPixelsWithoutChangingPageGeometry(t *testing.T) {
	g, driver := newDropTestGui(t)
	doc := workspacePDF(t, 3)
	g.addDocument(doc, nil, nil)
	g.zoom = 2
	g.applyZoom()
	page := g.activePDFPage
	logical := g.pdfPageSizes[page]
	minimum := g.pdfPageImages[page].MinSize()
	offset := g.pdfScroll.Offset
	for g.pdfDetail == nil || g.pdfDetail.image == nil || g.pdfDetail.rendered.dpi != 288 {
		driver.runNext(t)
	}
	state := g.pdfDetail
	if state.image.Bounds().Dx() != logical.X*2 || state.image.Bounds().Dy() != logical.Y*2 {
		t.Fatal("detailed preview has wrong density")
	}
	if g.pdfPageSizes[page] != logical || g.pdfPageImages[page].MinSize() != minimum || g.pdfScroll.Offset != offset || g.signatureLayers[page].pageSize != logical {
		t.Fatal("pixel enhancement moved the page or changed overlay coordinates")
	}
	if doc.Dirty() {
		t.Fatal("display quality changed document history")
	}
	if dpi := g.pdfOverlayPreviewDPI(page); dpi != 288 {
		t.Fatal("highlight text retained ordinary pixel density")
	}
	// Multiple requests retain the visible image while the latest zoom is prepared.
	previous := state.image
	g.zoom = 2.5
	g.refreshPDFDetail()
	g.zoom = 3
	g.refreshPDFDetail()
	if g.pdfPageImages[page].Image != previous {
		t.Fatal("zoom discarded the last visible frame")
	}
	for state.rendered.dpi != 432 {
		driver.runNext(t)
	}
	if state != g.pdfDetail || math.Abs(state.rendered.dpi-432) > .01 {
		t.Fatal("zoom requests were not coalesced")
	}
	// Releasing detail restores the ordinary preview and releases the large image.
	fallback := state.fallback
	g.zoom = .5
	g.refreshPDFDetail()
	if state.image != nil || state.fallback != nil || g.pdfPageImages[page].Image != fallback {
		t.Fatal("ordinary zoom retained extra high resolution image")
	}
	g.closeWindow()
}

func TestPDFDetailSurvivesOverlayEditsWithoutAnotherRefresh(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.addDocument(workspacePDF(t, 1), nil, nil)
	g.zoom = 2
	g.applyZoom()
	for g.pdfDetail == nil || g.pdfDetail.image == nil {
		driver.runNext(t)
	}
	state, frame := g.pdfDetail, g.pdfDetail.image
	data := annotations.Default(annotations.Shape)
	data.Shape, data.PageWidth, data.PageHeight = annotations.Rectangle, 72, 72
	data.Points = []annotations.Point{{X: 10, Y: 10}, {X: 30, Y: 30}}
	g.addPageAnnotation(g.signatureLayers[0], data, 0, 0, nil)
	for g.annotationPending > 0 {
		driver.runNext(t)
	}
	if g.pdfDetail != state || state.image != frame || state.timer != nil || state.running {
		t.Fatal("overlay edit invalidated source detail")
	}
	g.undo()
	if state.image != frame || state.timer != nil {
		t.Fatal("overlay Undo invalidated source detail")
	}
	g.closeWindow()
}
