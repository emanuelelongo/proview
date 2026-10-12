package gui

import (
	"bytes"
	"emanuelelongo/proview/pkg/document"
	"fmt"
	"image"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestPDFScrollLoadsPagesAndThumbnails(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	source, err := document.Load("scroll.pdf", bytes.NewReader(scrollTestPDF(10)))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	g := &Gui{app: app, window: app.NewWindow("Test"), zoom: 1, fitZoom: 1}
	g.image = canvas.NewImageFromImage(nil)
	g.imageScroll = container.NewScroll(g.image)
	g.pdfPages = container.NewVBox()
	g.pdfScroll = container.NewScroll(g.pdfPages)
	g.pdfScroll.OnScrolled = func(fyne.Position) { g.loadVisiblePDFPages() }
	g.thumbList = container.NewVBox()
	g.sidebar = NewSidebar(g)
	g.sidebar.container.OnScrolled = func(fyne.Position) { g.loadVisiblePDFThumbnails() }
	g.cropOverlay = newCropOverlay()
	g.imageSignatureLayer = newSignatureLayer(nil, 0, image.Point{}, nil, nil, nil)
	g.sidebarSplit = newFixedSidebarSplit(g.sidebar.container, container.NewStack(g.imageScroll, g.pdfScroll), defaultSidebarWidth, nil)
	g.mainSplit = g.sidebarSplit
	g.window.SetContent(g.mainSplit)
	g.window.Resize(fyne.NewSize(500, 400))
	g.showPDF(doc)
	g.sidebarSplit.Resize(fyne.NewSize(500, 400))
	g.sidebarSplit.Refresh()

	const last = 9
	if g.pdfPageLoaded[last] || g.pdfPageLoading[last] || g.pdfThumbLoaded[last] || g.pdfThumbLoading[last] {
		t.Fatal("last page was loaded before scrolling")
	}
	g.pdfScroll.ScrollToOffset(fyne.NewPos(0, g.pdfPageTop(last)))
	if !g.pdfPageLoaded[last] && !g.pdfPageLoading[last] {
		t.Fatal("scrolling the document did not request the last page")
	}
	if g.activePDFPage != last || !g.pdfThumbBorders[last].Visible() {
		t.Fatal("last page was not selected after scrolling")
	}
	g.sidebar.container.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: -g.thumbList.MinSize().Height}})
	if !g.pdfThumbLoaded[last] && !g.pdfThumbLoading[last] {
		t.Fatalf("scrolling sidebar did not request last: size=%v offset=%v content=%v", g.sidebar.container.Size(), g.sidebar.container.Offset, g.thumbList.MinSize())
	}
}

func scrollTestPDF(pageCount int) []byte {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>"}
	var kids bytes.Buffer
	for page := 0; page < pageCount; page++ {
		fmt.Fprintf(&kids, "%d 0 R ", 3+page*2)
	}
	objects = append(objects, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), pageCount))
	for page := 0; page < pageCount; page++ {
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] /Contents %d 0 R >>", 4+page*2),
			"<< /Length 0 >>\nstream\n\nendstream",
		)
	}
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return pdf.Bytes()
}
