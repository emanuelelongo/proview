package gui

import (
	"bytes"
	"emanuelelongo/proview/pkg/document"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestDefaultSignatureRectPreservesAspectRatio(t *testing.T) {
	tests := []struct {
		name                            string
		pageWidth, pageHeight           int
		signatureWidth, signatureHeight int
	}{
		{name: "landscape signature", pageWidth: 1000, pageHeight: 1500, signatureWidth: 600, signatureHeight: 200},
		{name: "portrait signature", pageWidth: 1500, pageHeight: 1000, signatureWidth: 200, signatureHeight: 600},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rect, err := defaultSignatureRect(test.pageWidth, test.pageHeight, test.signatureWidth, test.signatureHeight)
			if err != nil {
				t.Fatal(err)
			}
			if rect.Width > .3 || rect.Height > .2 || !rect.Valid() {
				t.Fatalf("invalid default rect: %#v", rect)
			}
			if math.Abs(rect.X-(1-rect.Width)/2) > 1e-12 || math.Abs(rect.Y-(1-rect.Height)/2) > 1e-12 {
				t.Fatalf("rect is not centered: %#v", rect)
			}
			displayAspect := rect.Width * float64(test.pageWidth) / (rect.Height * float64(test.pageHeight))
			wantAspect := float64(test.signatureWidth) / float64(test.signatureHeight)
			if math.Abs(displayAspect-wantAspect) > 1e-12 {
				t.Fatalf("display aspect = %f, want %f", displayAspect, wantAspect)
			}
		})
	}
}

func TestSignatureLayerLoadsMovesAndResizesOverlay(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()

	doc := testDocument(t, 1000, 500)
	initialRect := document.NormalizedRect{X: .2, Y: .2, Width: .3, Height: .3}
	signature, err := doc.AddSignature(0, initialRect, testSignature(t, 2, 1))
	if err != nil {
		t.Fatal(err)
	}
	changed := 0
	layer := newSignatureLayer(doc, 0, image.Pt(1000, 500), nil, nil, func(int) { changed++ })
	layer.Resize(fyne.NewSize(800, 800))
	renderer := fyneTest.WidgetRenderer(layer)
	renderer.Layout(layer.Size())
	if len(renderer.Objects()) != 1 || len(layer.widgets) != 1 {
		t.Fatalf("layer objects = %d, widgets = %d", len(renderer.Objects()), len(layer.widgets))
	}

	widget := layer.widgets[0]
	assertPosition(t, widget.Position(), fyne.NewPos(160, 280))
	assertSize(t, widget.Size(), fyne.NewSize(240, 120))

	layer.selectSignature(signature.ID)
	widget.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(800, 800)})
	widget.DragEnd()
	if changed != 1 {
		t.Fatalf("change callbacks = %d, want 1", changed)
	}
	moved := doc.Signatures()[0].Rect
	if math.Abs(moved.X-.7) > 1e-6 || math.Abs(moved.Y-.7) > 1e-6 {
		t.Fatalf("moved rect = %#v, want bottom-right constrained", moved)
	}

	if err := doc.UpdateSignature(signature.ID, 0, initialRect); err != nil {
		t.Fatal(err)
	}
	widget.rect = initialRect
	widget.resize(signatureBottomRight, fyne.NewDelta(80, 0))
	widget.finishChange()
	resized := doc.Signatures()[0].Rect
	if math.Abs(resized.Width-.4) > 1e-6 || math.Abs(resized.Height-.4) > 1e-6 {
		t.Fatalf("resized rect = %#v, want 0.4 x 0.4", resized)
	}
}

func TestSignatureLayerReloadUpdatesRendererObjects(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()

	doc := testDocument(t, 100, 100)
	layer := newSignatureLayer(doc, 0, image.Pt(100, 100), nil, nil, nil)
	renderer := fyneTest.WidgetRenderer(layer)
	if len(renderer.Objects()) != 0 {
		t.Fatal("new layer unexpectedly contains signatures")
	}
	if _, err := doc.AddSignature(0, document.NormalizedRect{X: .2, Y: .2, Width: .4, Height: .2}, testSignature(t, 2, 1)); err != nil {
		t.Fatal(err)
	}
	layer.reload()
	if len(renderer.Objects()) != 1 {
		t.Fatalf("renderer objects after reload = %d, want 1", len(renderer.Objects()))
	}
}

func TestInactiveCropOverlayDoesNotInterceptSignatureDrag(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()

	doc := testDocument(t, 400, 200)
	initialRect := document.NormalizedRect{X: .2, Y: .2, Width: .3, Height: .3}
	signature, err := doc.AddSignature(0, initialRect, testSignature(t, 2, 1))
	if err != nil {
		t.Fatal(err)
	}
	layer := newSignatureLayer(doc, 0, image.Pt(400, 200), nil, nil, nil)
	layer.selectSignature(signature.ID)
	crop := newCropOverlay()
	if crop.Visible() {
		t.Fatal("inactive crop overlay is visible")
	}
	base := canvas.NewRectangle(color.White)
	base.SetMinSize(fyne.NewSize(400, 200))
	content := container.NewStack(base, layer, crop)
	window := fyneTest.NewWindow(content)
	defer window.Close()
	window.Resize(fyne.NewSize(400, 200))
	window.Show()

	renderer := fyneTest.WidgetRenderer(layer)
	renderer.Layout(layer.Size())
	widget := layer.widgets[0]
	start := widget.Position().Add(fyne.NewPos(widget.Size().Width/2, widget.Size().Height/2))
	fyneTest.Drag(window.Canvas(), start, 40, 20)

	moved := doc.Signatures()[0].Rect
	if moved == initialRect {
		t.Fatal("canvas drag did not reach the signature widget")
	}
	if !doc.Undo() || doc.Signatures()[0].Rect != initialRect {
		t.Fatal("one undo did not revert the complete canvas drag")
	}
}

func testDocument(t *testing.T, width, height int) *document.Document {
	t.Helper()
	base := image.NewNRGBA(image.Rect(0, 0, width, height))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, base); err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("image.png", bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func testSignature(t *testing.T, width, height int) []byte {
	t.Helper()
	signature := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			signature.SetNRGBA(x, y, color.NRGBA{R: 20, G: 30, B: 40, A: 180})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, signature); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func assertPosition(t *testing.T, got, want fyne.Position) {
	t.Helper()
	if math.Abs(float64(got.X-want.X)) > 1e-4 || math.Abs(float64(got.Y-want.Y)) > 1e-4 {
		t.Fatalf("position = %v, want %v", got, want)
	}
}

func assertSize(t *testing.T, got, want fyne.Size) {
	t.Helper()
	if math.Abs(float64(got.Width-want.Width)) > 1e-4 || math.Abs(float64(got.Height-want.Height)) > 1e-4 {
		t.Fatalf("size = %v, want %v", got, want)
	}
}
