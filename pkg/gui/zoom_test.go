package gui

import (
	"math"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestZoomStartsAtFitAndDoesNotAccumulateBelowMinimum(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	doc := testDocument(t, 100, 50)
	preview, err := doc.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	page := canvas.NewImageFromImage(preview)
	layer := newSignatureLayer(doc, 0, preview.Bounds().Size(), nil, nil, nil)
	crop := newCropOverlay()
	content := container.New(&imageViewportLayout{image: page}, page, layer, crop)
	scroll := container.NewScroll(content)
	scroll.Resize(fyne.NewSize(200, 100))
	g := &Gui{
		document:            doc,
		image:               page,
		imageContent:        content,
		imageScroll:         scroll,
		imageSignatureLayer: layer,
		cropOverlay:         crop,
		zoom:                1,
		fitZoom:             1,
	}

	g.resetZoom()
	fit := g.zoom
	if g.zoomLevel != 0 || fit <= 0 || fit >= 2 {
		t.Fatalf("initial zoom = %f at level %d, want fit below 2 at level 0", fit, g.zoomLevel)
	}
	for range 100 {
		g.zoomOut()
	}
	minimum := fit * float32(math.Pow(float64(zoomStep), minZoomLevel))
	if math.Abs(float64(g.zoom-minimum)) > 1e-6 || g.zoomLevel != minZoomLevel {
		t.Fatalf("minimum zoom = %f at level %d", g.zoom, g.zoomLevel)
	}
	if page.Size() != page.MinSize() || page.Position().X <= 0 || page.Position().Y <= 0 {
		t.Fatalf("image not centered at actual zoom: size %v min %v position %v", page.Size(), page.MinSize(), page.Position())
	}
	g.resetZoom()
	g.zoomIn()
	if g.zoomLevel != 1 || math.Abs(float64(g.zoom-fit*zoomStep)) > 1e-6 {
		t.Fatalf("first zoom in = %f at level %d, want %f at level 1", g.zoom, g.zoomLevel, fit*zoomStep)
	}
}

func TestStartingCropPreservesZoomAndOverlayHasNoMinimumSize(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	doc := testDocument(t, 100, 50)
	preview, err := doc.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	page := canvas.NewImageFromImage(preview)
	page.SetMinSize(fyne.NewSize(150, 75))
	layer := newSignatureLayer(doc, 0, preview.Bounds().Size(), nil, nil, nil)
	crop := newCropOverlay()
	content := container.New(&imageViewportLayout{image: page}, page, layer, crop)
	g := &Gui{
		document:            doc,
		image:               page,
		imageContent:        content,
		imageSignatureLayer: layer,
		cropOverlay:         crop,
		signatureLayers:     map[int]*signatureLayer{0: layer},
		zoom:                1.5,
		fitZoom:             1,
		zoomLevel:           2,
	}

	g.startCrop()
	if g.zoom != 1.5 || g.zoomLevel != 2 {
		t.Fatalf("starting crop changed zoom to %f at level %d", g.zoom, g.zoomLevel)
	}
	if got := crop.CreateRenderer().MinSize(); got != (fyne.Size{}) {
		t.Fatalf("crop overlay minimum size = %v, want zero", got)
	}
}
