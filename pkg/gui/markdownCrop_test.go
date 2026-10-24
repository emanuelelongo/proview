package gui

import (
	"bytes"
	"context"
	"emanuelelongo/proview/internal/markdown"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestMarkdownCropCreatesIndependentImageAndPreservesSource(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	openTestMermaid(t, g, "flowchart LR\n A[Markdown] --> B[Preview]")
	settleMarkdownDiagrams(t, g, driver)
	view := g.markdownView
	original := bytes.Clone(view.doc.Source)
	g.startCrop()
	if !view.crop.active {
		t.Fatal("crop did not activate before snapshot preparation")
	}
	for view.cropSnapshot.Image == nil {
		driver.runNext(t)
	}
	if !view.crop.active || !view.cropSnapshot.Visible() || view.scroll.Visible() || g.cropAction.button.Importance != widget.HighImportance {
		t.Fatal("Markdown Crop did not freeze its viewport")
	}
	frame := view.cropSnapshot.Image
	start, end := fyne.NewPos(40, 30), fyne.NewPos(240, 180)
	selected, ok := view.crop.selectionImageRect(start.X, start.Y, end.X, end.Y)
	if !ok {
		t.Fatal("selection cannot map to capture pixels")
	}
	view.crop.beginDrag(start)
	view.crop.finishDrag(end)
	view.crop.applySelection()
	for g.working {
		driver.runNext(t)
	}
	if view.crop.active || view.cropSnapshot.Visible() || !view.scroll.Visible() {
		t.Fatal("completed Crop did not restore reading")
	}
	if g.showingMarkdown() || !bytes.Equal(view.doc.Source, original) || len(g.editors.windows) != 1 || len(g.documents) != 2 {
		t.Fatal("Crop did not retain the Markdown document in the same workspace")
	}
	editor := g
	if editor.document == nil || !editor.document.Dirty() || editor.saveTarget != nil || editor.document.Name() != "diagrams-crop.png" {
		t.Fatal("crop did not create an independent unsaved image")
	}
	preview, err := editor.document.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Bounds().Size() != selected.Size() {
		t.Fatalf("wrong crop dimensions: %v want %v", preview.Bounds().Size(), selected.Size())
	}
	want := color.NRGBAModel.Convert(frame.At(selected.Min.X, selected.Min.Y))
	if color.NRGBAModel.Convert(preview.At(0, 0)) != want {
		t.Fatal("crop captured the wrong viewport pixels")
	}
	g.selectDocument(g.documents[0])
	settleMarkdownDiagrams(t, g, driver)
	if !g.showingMarkdown() || !bytes.Equal(g.markdownView.doc.Source, original) {
		t.Fatal("Crop lost the original Markdown")
	}
	g.closeWindow()
}
func TestMarkdownCropCancelsOnEscapeZoomResizeAndSwitch(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	openTestMermaid(t, g, "flowchart LR\n A --> B")
	settleMarkdownDiagrams(t, g, driver)
	v := g.markdownView
	g.startCrop()
	if !g.cancelCurrentOperation() || v.crop.active {
		t.Fatal("Escape did not cancel Markdown crop")
	}
	g.startCrop()
	g.zoomIn()
	if v.crop.active || v.cropSnapshot.Visible() {
		t.Fatal("zoom retained a stale capture")
	}
	settleMarkdownDiagrams(t, g, driver)
	g.startCrop()
	g.window.Resize(fyne.NewSize(900, 700))
	if v.crop.active || v.cropSnapshot.Visible() {
		t.Fatal("resize retained a stale capture")
	}
	settleMarkdownDiagrams(t, g, driver)
	g.startCrop()
	other, err := markdown.Load("other.md", "", strings.NewReader("# Another document"))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(other, nil)
	if v.crop.active {
		t.Fatal("document switch retained the crop")
	}
	g.requestCloseDocument()
	settleMarkdownDiagrams(t, g, driver)
	g.requestCloseDocument()
}
func TestMarkdownViewportSnapshotMapsDevicePixels(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			frame.SetRGBA(x, y, color.RGBA{uint8(x % 255), uint8(y % 255), 123, 255})
		}
	}
	snapshot, err := markdownViewportSnapshot(frame, fyne.NewSize(400, 300), fyne.NewPos(40, 30), fyne.NewSize(200, 150))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Bounds().Size() != image.Pt(400, 300) || snapshot.RGBAAt(0, 0) != frame.RGBAAt(80, 60) {
		t.Fatal("logical viewport did not map to 2x capture pixels")
	}
	expected := snapshot.RGBAAt(0, 0)
	frame.SetRGBA(80, 60, color.RGBA{})
	if snapshot.RGBAAt(0, 0) != expected {
		t.Fatal("snapshot shares its source pixels")
	}
}

type markdownTestClipboard struct{ written chan []byte }

func (*markdownTestClipboard) Read(context.Context) ([]byte, error) { return nil, nil }
func (c *markdownTestClipboard) Write(ctx context.Context, data []byte) error {
	select {
	case c.written <- bytes.Clone(data):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestMermaidContextMenuCopiesRenderedImage(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(900, 700))
	g.window.Show()
	openTestMermaid(t, g, "flowchart LR\n A[Copy] --> B[Image]")
	settleMarkdownDiagrams(t, g, driver)
	preview := g.markdownView.diagrams[0]
	clipboard := &markdownTestClipboard{written: make(chan []byte, 1)}
	previous := desktopImageClipboard
	desktopImageClipboard = clipboard
	defer func() { desktopImageClipboard = previous }()
	test.TapSecondary(preview)
	var popup *widget.PopUpMenu
	for _, object := range test.LaidOutObjects(g.window.Canvas().Overlays().Top()) {
		if menu, ok := object.(*widget.PopUpMenu); ok {
			popup = menu
			break
		}
	}
	if popup == nil || len(popup.Items) != 1 {
		t.Fatal("diagram context menu did not open")
	}
	test.Tap(popup.Items[0].(fyne.Tappable))
	select {
	case data := <-clipboard.written:
		copied, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if copied.Bounds() != preview.result.Image.Bounds() {
			t.Fatal("clipboard image lost rendered resolution")
		}
		want := color.NRGBAModel.Convert(preview.result.Image.At(0, 0))
		if color.NRGBAModel.Convert(copied.At(0, 0)) != want {
			t.Fatal("clipboard pixels do not match the diagram")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Copy image did not write the clipboard")
	}
	if !g.showingMarkdown() || g.document != nil {
		t.Fatal("Copy image changed the source document")
	}
	g.requestCloseDocument()
}

type markdownCropCaptureCanvas struct {
	fyne.Canvas
	frame image.Image
}

func (c markdownCropCaptureCanvas) Capture() image.Image { return c.frame }

type markdownCropCaptureWindow struct {
	fyne.Window
	capture fyne.Canvas
}

func (w markdownCropCaptureWindow) Canvas() fyne.Canvas { return w.capture }

type markdownSlowCapture struct {
	image.Image
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *markdownSlowCapture) At(x, y int) color.Color {
	s.once.Do(func() { close(s.entered); <-s.release })
	return s.Image.At(x, y)
}
func slowMarkdownCropCapture(t *testing.T, g *Gui) *markdownSlowCapture {
	t.Helper()
	frame := &markdownSlowCapture{Image: image.NewRGBA(image.Rect(0, 0, 1000, 740)), entered: make(chan struct{}), release: make(chan struct{})}
	g.window = markdownCropCaptureWindow{Window: g.window, capture: markdownCropCaptureCanvas{Canvas: g.window.Canvas(), frame: frame}}
	return frame
}

func TestMarkdownCropAcceptsAndConfirmsDragDuringSnapshotPreparation(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	openTestMermaid(t, g, "flowchart LR\n A --> B")
	settleMarkdownDiagrams(t, g, driver)
	view := g.markdownView
	driver.drain()
	frame := slowMarkdownCropCapture(t, g)
	var release sync.Once
	defer release.Do(func() { close(frame.release) })
	g.startCrop()
	select {
	case <-frame.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("snapshot worker did not start")
	}
	if !view.crop.active || view.cropSnapshot.Visible() {
		t.Fatal("crop waited for the slow copy")
	}
	view.crop.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(240, 180)}, Dragged: fyne.Delta{DX: 200, DY: 150}})
	view.crop.DragEnd()
	if !view.crop.hasSelection {
		t.Fatal("first drag was lost while preparing the snapshot")
	}
	view.crop.applySelection()
	if !g.working {
		t.Fatal("early confirmation was lost")
	}
	release.Do(func() { close(frame.release) })
	for g.working {
		driver.runNext(t)
	}
	driver.drain()
	if g.document == nil || g.showingMarkdown() || len(g.documents) != 2 || view.cropSnapshot.Visible() {
		t.Fatal("early confirmation did not create an independent image")
	}
	g.closeWindow()
}

func TestMarkdownCropIgnoresCanceledSnapshotAndDefersDiagramLayout(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	openTestMermaid(t, g, "flowchart LR\n A --> B")
	settleMarkdownDiagrams(t, g, driver)
	view := g.markdownView
	driver.drain()
	frame := slowMarkdownCropCapture(t, g)
	var release sync.Once
	defer release.Do(func() { close(frame.release) })
	g.startCrop()
	updated := false
	view.updateDiagramLayout(func() { updated = true })
	if updated {
		t.Fatal("late diagram changed the view during crop")
	}
	if view.captureThumbnail() {
		t.Fatal("thumbnail captured the crop overlay")
	}
	g.cancelCurrentOperation()
	if !updated {
		t.Fatal("cancel did not resume diagram updates")
	}
	release.Do(func() { close(frame.release) })
	driver.runNext(t)
	if view.crop.active || view.cropSnapshot.Visible() || view.cropSnapshot.Image != nil || !view.scroll.Visible() {
		t.Fatal("late copy reactivated a canceled crop")
	}
	g.closeWindow()
}
