package gui

import (
	"bytes"
	"fmt"
	"image"
	"strings"
	"testing"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func BenchmarkLongPDFSourceKey(b *testing.B) {
	source, err := document.Load("pages.pdf", bytes.NewReader(scrollTestPDF(1000)))
	if err != nil {
		b.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		b.Fatal(err)
	}
	defer doc.Close()
	g := &Gui{document: doc}
	for _, legacy := range []bool{true, false} {
		b.Run(fmt.Sprintf("scanAllPages=%v", legacy), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var key string
				if legacy {
					var builder strings.Builder
					for page := 0; page < doc.PageCount(); page++ {
						builder.WriteString(doc.TextSelectionKey(page))
						builder.WriteByte('|')
					}
					key = builder.String()
				} else {
					key = g.searchKey()
				}
				if len(key) == 0 {
					b.Fatal("missing source key")
				}
			}
		})
	}
}

func BenchmarkLongPDFViewportLookup(b *testing.B) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := &Gui{}
	for range 1000 {
		object := canvas.NewRectangle(nil)
		object.SetMinSize(fyne.NewSize(100, 140))
		g.pdfPageContainers = append(g.pdfPageContainers, object)
	}
	g.updatePDFPageTops()
	for _, legacy := range []bool{true, false} {
		b.Run(fmt.Sprintf("scanAllPages=%v", legacy), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				top, bottom := float32(70000), float32(70500)
				first, end := -1, 0
				if legacy {
					y := float32(0)
					for page, object := range g.pdfPageContainers {
						next := y + object.MinSize().Height
						if next > top && y < bottom {
							if first < 0 {
								first = page
							}
							end = page + 1
						}
						y = next + theme.Padding()
					}
				} else {
					first, end = g.visiblePDFPageRange(top, bottom)
				}
				if first < 0 || end <= first {
					b.Fatal("missing visible pages")
				}
			}
		})
	}
}

func BenchmarkLongPDFZoomLayout(b *testing.B) {
	app := fyneTest.NewApp()
	defer app.Quit()
	source, err := document.Load("pages.pdf", bytes.NewReader(scrollTestPDF(1000)))
	if err != nil {
		b.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		b.Fatal(err)
	}
	defer doc.Close()
	g := &Gui{document: doc, signatureLayers: map[int]*signatureLayer{}, pdfPages: container.NewVBox(), zoom: 1}
	for page := 0; page < 1000; page++ {
		picture := canvas.NewImageFromImage(nil)
		picture.SetMinSize(fyne.NewSize(144, 144))
		layer := newSignatureLayer(doc, page, image.Pt(144, 144), nil, nil, nil)
		object := container.NewCenter(container.NewStack(picture, layer))
		g.pdfPageImages = append(g.pdfPageImages, picture)
		g.pdfPageSizes = append(g.pdfPageSizes, image.Pt(144, 144))
		g.pdfPageContainers = append(g.pdfPageContainers, object)
		g.pdfPages.Add(object)
		g.signatureLayers[page] = layer
	}
	for _, eager := range []bool{true, false} {
		b.Run(fmt.Sprintf("eagerLayers=%v", eager), func(b *testing.B) {
			for page, layer := range g.signatureLayers {
				layer.pageLayoutOwned = !eager
				if eager || page < 5 {
					layer.Show()
				} else {
					layer.Hide()
				}
			}
			g.layoutZoom()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				g.zoom = 1 + float32(i%2)*.25
				g.layoutZoom()
			}
		})
	}
}
