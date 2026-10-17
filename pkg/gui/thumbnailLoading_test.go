package gui

import (
	"bytes"
	"testing"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func TestThumbnailViewportStartsLoadingAfterLayoutAndOnResize(t *testing.T) {
	g, driver := newDropTestGui(t)
	source, err := document.Load("pages.pdf", bytes.NewReader(scrollTestPDF(10)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.closed = true; source.Close() })
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	g.document = doc
	g.pdfThumbLoaded, g.pdfThumbLoading = make([]bool, 10), make([]bool, 10)
	for range 10 {
		picture := canvas.NewImageFromImage(nil)
		picture.SetMinSize(fyne.NewSize(100, 100))
		placeholder := widget.NewLabel("Loading…")
		g.pdfThumbImages = append(g.pdfThumbImages, picture)
		g.pdfThumbPlaceholders = append(g.pdfThumbPlaceholders, placeholder)
		g.thumbList.Add(container.NewStack(picture, placeholder))
	}
	g.sidebar.container.Show()
	g.loadVisiblePDFThumbnails()
	if g.pdfThumbLoading[0] {
		t.Fatal("thumbnail started before viewport layout")
	}
	// Opening with no usable sidebar size must recover without any scrolling.
	g.sidebar.viewport.Resize(fyne.NewSize(140, 110))
	driver.runNext(t)
	if !g.pdfThumbLoading[0] || !g.pdfThumbLoading[1] || !g.pdfThumbLoading[2] || g.pdfThumbLoading[3] {
		t.Fatal("first layout did not load visible pages plus one lookahead")
	}
	for !g.pdfThumbLoaded[0] || !g.pdfThumbLoaded[1] || !g.pdfThumbLoaded[2] {
		driver.runNext(t)
	}
	for page := range 3 {
		if g.pdfThumbImages[page].Image == nil || g.pdfThumbPlaceholders[page].Visible() {
			t.Fatal("completed thumbnail still shows loading placeholder")
		}
	}
	if g.pdfThumbImages[9].Image != nil || !g.pdfThumbPlaceholders[9].Visible() {
		t.Fatal("offscreen page rendered eagerly or lost its loading state")
	}
	// A larger window reveals new pages with the scroll offset still at zero.
	g.sidebar.viewport.Resize(fyne.NewSize(140, 320))
	g.sidebar.viewport.Resize(fyne.NewSize(140, 430))
	if len(driver.callbacks) != 1 {
		t.Fatal("layout callbacks were not coalesced")
	}
	driver.runNext(t)
	if !g.pdfThumbLoading[4] || !g.pdfThumbLoading[5] || g.pdfThumbLoading[6] {
		t.Fatal("resize did not use the latest visible range")
	}
	for !g.pdfThumbLoaded[3] || !g.pdfThumbLoaded[4] || !g.pdfThumbLoaded[5] {
		driver.runNext(t)
	}
}

func TestClosedWindowIgnoresDeferredThumbnailLayout(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.sidebar.viewport.Resize(fyne.NewSize(140, 400))
	g.closed = true
	driver.runNext(t)
}
