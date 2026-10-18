package gui

import (
	"bytes"
	"context"
	"emanuelelongo/proview/internal/model3d"
	"errors"
	"fyne.io/fyne/v2/widget"
	"image"
	"image/color"
	"math"
	"os"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/storage"
	fyneTest "fyne.io/fyne/v2/test"
)

func testSTLMesh(t *testing.T) *model3d.Mesh {
	t.Helper()
	data, err := os.ReadFile("../../internal/model3d/testdata/cube.stl")
	if err != nil {
		t.Fatal(err)
	}
	m, err := model3d.Load("cube.stl", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestModelModeDisablesEditingAndClosesWithoutClosingWindow(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.window.Show()
	// Keep camera/command tests independent of the asynchronous frame worker.
	g.modelViewport.running = true
	g.showDocument(testDocument(t, 40, 20))
	g.showModel(testSTLMesh(t))
	if g.document != nil || !g.showingModel() || !g.hasOpenContent() || g.window.Title() != "cube.stl — Proview" {
		t.Fatal("model mode was not activated")
	}
	if !g.saveItem.Disabled || !g.saveAsItem.Disabled || !g.signatureItem.Disabled || g.cropAction.button.Disabled() {
		t.Fatal("image editing enabled for STL")
	}
	for _, item := range g.menuDocumentItems {
		enabled := item.Label == "Crop" || item.Label == "Close" || item.Label == "Zoom In" || item.Label == "Zoom Out" || item.Label == "Fit to Window"
		if item.Disabled == enabled {
			t.Fatalf("wrong model command availability: %s", item.Label)
		}
	}
	baseline := len(app.Driver().AllWindows())
	g.requestCloseDocument()
	if g.hasOpenContent() || g.closed || len(app.Driver().AllWindows()) != baseline || g.modelPanel.Visible() {
		t.Fatal("Close did not clear the model and preserve the window")
	}
	g.showDocument(testDocument(t, 40, 20))
	if g.showingModel() || g.saveItem.Disabled || g.cropAction.button.Disabled() {
		t.Fatal("image mode was not restored")
	}
	g.requestCloseDocument()
}
func TestModelNavigationAndNewImagePreserveMesh(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.window.Show()
	g.modelViewport.running = true
	g.showModel(testSTLMesh(t))
	v := g.modelViewport
	original := v.camera
	v.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DX: 30, DY: 10}})
	v.DragEnd()
	if v.camera.Yaw == original.Yaw {
		t.Fatal("drag did not orbit")
	}
	yaw := v.camera.Yaw
	g.handlePanKey(fyne.KeySpace, true)
	v.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DX: 10, DY: 10}})
	v.DragEnd()
	g.handlePanKey(fyne.KeySpace, false)
	if v.camera.Yaw != yaw || v.camera.PanX == 0 {
		t.Fatal("Space drag did not pan independently of orbit")
	}
	g.zoomIn()
	if v.camera.Zoom <= 1 {
		t.Fatal("zoom toolbar did not affect the model")
	}
	g.resetZoom()
	if v.camera.Zoom != 1 || v.camera.PanX != 0 {
		t.Fatal("Fit did not reset scale and pan")
	}
	g.openNewImage(testDocument(t, 20, 10))
	if !g.showingModel() {
		t.Fatal("New image replaced the model")
	}
	g.closeWindow()
}

type trackedModelReader struct {
	*bytes.Reader
	closed   bool
	closeErr error
}

func (r *trackedModelReader) URI() fyne.URI { return storage.NewFileURI("sample.STL") }
func (r *trackedModelReader) Close() error  { r.closed = true; return r.closeErr }
func TestModelLoaderClosesReadersOnSuccessAndFailure(t *testing.T) {
	data, err := os.ReadFile("../../internal/model3d/testdata/cube.stl")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range [][]byte{data, []byte("invalid")} {
		r := &trackedModelReader{Reader: bytes.NewReader(input)}
		m, err := loadModelReader(r)
		if !r.closed {
			t.Fatal("reader leaked")
		}
		if bytes.Equal(input, data) && (err != nil || len(m.Triangles) != 12) {
			t.Fatalf("valid STL: %v", err)
		}
		if !bytes.Equal(input, data) && err == nil {
			t.Fatal("bad STL accepted")
		}
	}
	r := &trackedModelReader{Reader: bytes.NewReader(data), closeErr: errors.New("close failed")}
	if _, err := loadModelReader(r); err == nil {
		t.Fatal("close failure ignored")
	}
	if !isModelFile("example.STL") || isModelFile("example.png") {
		t.Fatal("incorrect loader routing")
	}
}

func TestModelViewKeysAndConditionalMenu(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.window.Show()
	g.modelViewport.running = true
	if g.handleModelKey(fyne.Key1) {
		t.Fatal("model key active without STL")
	}
	g.showModel(testSTLMesh(t))
	if len(g.modelMenu.Items) != 12 {
		t.Fatal("STL views missing from View")
	}
	expected := [][2]float64{{math.Pi / 4, math.Pi / 6}, {0, math.Pi / 2}, {0, -math.Pi / 2}, {0, 0}, {math.Pi, 0}, {-math.Pi / 2, 0}, {math.Pi / 2, 0}}
	g.window.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.Key0})
	if !g.modelViewport.camera.Wireframe || !g.modelWireframeItem.Checked {
		t.Fatal("0 did not enable wireframe")
	}
	for i, angles := range expected {
		g.modelViewport.camera.Zoom = 2
		g.modelViewport.camera.PanX = .2
		g.window.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyName(string(rune('1' + i)))})
		c := g.modelViewport.camera
		if c.Yaw != angles[0] || c.Pitch != angles[1] || c.Zoom != 1 || c.PanX != 0 || !c.Wireframe {
			t.Fatalf("incorrect view %s: %+v", modelViews[i], c)
		}
	}
	g.resetZoom()
	if !g.modelViewport.camera.Wireframe {
		t.Fatal("Fit toggled wireframe")
	}
	entry := widget.NewEntry()
	originalContent := g.window.Content()
	g.window.SetContent(container.NewStack(originalContent, entry))
	g.window.Canvas().Focus(entry)
	if g.handleModelKey(fyne.Key0) {
		t.Fatal("numeric key intercepted from text field")
	}
	g.window.Canvas().Focus(nil)
	g.window.SetContent(originalContent)
	g.handleModelKey(fyne.Key0)
	if g.modelViewport.camera.Wireframe || g.modelWireframeItem.Checked {
		t.Fatal("0 did not disable wireframe")
	}
	g.requestCloseDocument()
	// macOS retains callback items and hides their native counterparts; other
	// platforms remove the section from the menu's item list.
	for _, item := range g.modelMenu.Items[3:] {
		if !item.Disabled {
			t.Fatal("STL command active after Close")
		}
	}
	g.showDocument(testDocument(t, 40, 20))
	if g.handleModelKey(fyne.Key0) {
		t.Fatal("STL key active for image")
	}
	g.requestCloseDocument()
}

func TestModelCropCreatesIndependentEditableImage(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.window.Show()
	g.modelViewport.running = true
	mesh := testSTLMesh(t)
	g.showModel(mesh)
	frame, err := model3d.RenderHighQuality(context.Background(), mesh, g.modelViewport.camera, 200, 160)
	if err != nil {
		t.Fatal(err)
	}
	g.modelViewport.image.Image = frame
	originalCamera := g.modelViewport.camera
	g.startCrop()
	if !g.modelCrop.active || g.cropAction.button.Importance != widget.HighImportance {
		t.Fatal("STL Crop did not activate")
	}
	if !g.cancelCurrentOperation() || g.modelCrop.active {
		t.Fatal("Escape did not cancel STL Crop")
	}
	g.startCrop()
	g.zoomIn()
	if g.modelCrop.active {
		t.Fatal("camera change retained stale crop selection")
	}
	g.modelViewport.camera = originalCamera
	rect := image.Rect(35, 20, 150, 100)
	doc, err := modelSnapshotDocument(frame, rect, "cube-crop.png")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := doc.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Bounds().Size() != rect.Size() || !doc.Dirty() {
		t.Fatal("snapshot has wrong dimensions or saved state")
	}
	want := color.NRGBAModel.Convert(frame.At(rect.Min.X, rect.Min.Y))
	if color.NRGBAModel.Convert(preview.At(0, 0)) != want {
		t.Fatal("snapshot selection mapped to incorrect pixels")
	}
	frame.SetRGBA(rect.Min.X, rect.Min.Y, color.RGBA{255, 0, 0, 255})
	if color.NRGBAModel.Convert(preview.At(0, 0)) != want {
		t.Fatal("snapshot shares mutable pixels with viewport")
	}
	windows := len(app.Driver().AllWindows())
	g.openImageWindow(doc)
	if len(app.Driver().AllWindows()) != windows+1 || g.modelViewport.mesh != mesh || g.modelViewport.camera != originalCamera || g.document != nil {
		t.Fatal("snapshot did not preserve STL window")
	}
	g.startCrop()
	g.requestCloseDocument()
	if g.modelCrop.active {
		t.Fatal("Close retained model Crop")
	}
}

func TestSTLDocumentThumbnailIsStaticAcrossNavigationAndSwitches(t *testing.T) {
	g, driver := newDropTestGui(t)
	// Exercise the thumbnail worker independently of the interactive renderer.
	g.modelViewport.running = true
	g.addDocument(nil, testSTLMesh(t), nil)
	entry := g.activeDocument
	g.addDocument(testDocument(t, 40, 30), nil, nil)
	for entry.thumbnail == nil {
		driver.runNext(t)
	}
	thumbnail := entry.thumbnail
	if thumbnail.Bounds().Dx() != 320 || thumbnail.Bounds().Dy() != 240 {
		t.Fatal("unbounded or missing model thumbnail")
	}
	found := false
	for _, obj := range fyneTest.LaidOutObjects(g.documentList) {
		if img, ok := obj.(*canvas.Image); ok && img.Image == thumbnail {
			found = true
		}
		if b, ok := obj.(*widget.Button); ok && b.Text == entry.mesh.Name {
			t.Fatal("STL still shows a filename")
		}
	}
	if !found {
		t.Fatal("retained STL thumbnail missing from sidebar")
	}
	g.selectDocument(entry)
	g.modelViewport.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(20, 10)})
	g.modelViewport.DragEnd()
	g.captureDocument()
	g.refreshDocumentSidebar()
	if entry.thumbnail != thumbnail || entry.thumbnailCancel != nil {
		t.Fatal("camera navigation regenerated static thumbnail")
	}
	g.closeWindow()
}
