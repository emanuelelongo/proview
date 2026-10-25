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
	frame := g.window.Canvas().Capture()
	position := g.app.Driver().AbsolutePositionForObject(view.scroll)
	region, err := markdownViewportRegion(frame, g.window.Canvas().Size(), position, view.scroll.Size())
	if err != nil {
		t.Fatal(err)
	}
	g.startCrop()
	if !view.crop.active || !view.scroll.Visible() || g.cropAction.button.Importance != widget.HighImportance {
		t.Fatal("Markdown Crop did not activate selection before capture")
	}
	start, end := fyne.NewPos(40, 30), fyne.NewPos(240, 180)
	selected, ok := view.crop.selectionImageRect(start.X, start.Y, end.X, end.Y)
	if !ok {
		t.Fatal("selection cannot map to capture pixels")
	}
	view.crop.beginDrag(start)
	view.crop.finishDrag(end)
	view.crop.applySelection()
	driver.drain()
	for g.working {
		driver.runNext(t)
	}
	if view.crop.active || !view.scroll.Visible() {
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
	want := color.NRGBAModel.Convert(frame.At(region.Min.X+selected.Min.X, region.Min.Y+selected.Min.Y))
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
	if v.crop.active {
		t.Fatal("zoom retained a stale capture")
	}
	settleMarkdownDiagrams(t, g, driver)
	g.startCrop()
	g.window.Resize(fyne.NewSize(900, 700))
	if v.crop.active {
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
	frame     *markdownSlowCapture
	crop      *cropOverlay
	onCapture func()
}

func (c *markdownCropCaptureCanvas) Capture() image.Image {
	c.frame.captures++
	c.frame.capturedWhileHidden = c.crop != nil && !c.crop.active && !c.crop.Visible()
	if c.onCapture != nil {
		c.onCapture()
	}
	return c.frame
}

type markdownCropCaptureWindow struct {
	fyne.Window
	capture fyne.Canvas
}

func (w markdownCropCaptureWindow) Canvas() fyne.Canvas { return w.capture }

type markdownSlowCapture struct {
	image.Image
	entered             chan struct{}
	release             chan struct{}
	once                sync.Once
	capturedWhileHidden bool
	captures            int
}

func (s *markdownSlowCapture) At(x, y int) color.Color {
	s.once.Do(func() { close(s.entered); <-s.release })
	return s.Image.At(x, y)
}
func slowMarkdownCropCapture(t *testing.T, g *Gui) *markdownSlowCapture {
	t.Helper()
	frame := &markdownSlowCapture{Image: image.NewRGBA(image.Rect(0, 0, 1000, 740)), entered: make(chan struct{}), release: make(chan struct{})}
	g.window = markdownCropCaptureWindow{Window: g.window, capture: &markdownCropCaptureCanvas{Canvas: g.window.Canvas(), frame: frame, crop: g.markdownView.crop}}
	return frame
}

func TestMarkdownCropSelectsBeforeCaptureAndCopiesPixelsInWorker(t *testing.T) {
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
	if !view.crop.active || frame.captures != 0 {
		t.Fatal("crop captured the canvas before confirmation")
	}
	view.crop.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(240, 180)}, Dragged: fyne.Delta{DX: 200, DY: 150}})
	view.crop.DragEnd()
	if !view.crop.hasSelection {
		t.Fatal("first drag was lost while preparing the snapshot")
	}
	updated := false
	view.updateDiagramLayout(func() { updated = true })
	capture := g.window.(markdownCropCaptureWindow).capture.(*markdownCropCaptureCanvas)
	capture.onCapture = func() {
		if updated {
			t.Fatal("diagram layout changed before capture")
		}
	}
	view.crop.applySelection()
	if updated {
		t.Fatal("confirmation resumed diagram layout before capture")
	}
	driver.runNext(t)
	if !updated || view.cropCapturing {
		t.Fatal("capture did not resume diagram layout")
	}
	if !frame.capturedWhileHidden {
		t.Fatal("canvas capture included the active crop overlay")
	}
	if !g.working {
		t.Fatal("early confirmation was lost")
	}
	select {
	case <-frame.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("crop pixel acquisition did not start after confirmation")
	}
	release.Do(func() { close(frame.release) })
	for g.working {
		driver.runNext(t)
	}
	driver.drain()
	if g.document == nil || g.showingMarkdown() || len(g.documents) != 2 {
		t.Fatal("early confirmation did not create an independent image")
	}
	g.closeWindow()
}

func TestMarkdownCropCancelResumesDiagramLayout(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	openTestMermaid(t, g, "flowchart LR\n A --> B")
	settleMarkdownDiagrams(t, g, driver)
	view := g.markdownView
	driver.drain()
	_ = slowMarkdownCropCapture(t, g)
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
	if view.crop.active || !view.scroll.Visible() {
		t.Fatal("cancel retained crop state")
	}
	g.closeWindow()
}

func TestMarkdownCropDiscardsCaptureAfterResizeOrSwitch(t *testing.T) {
	for _, action := range []string{"resize", "switch", "close", "restart"} {
		t.Run(action, func(t *testing.T) {
			g, driver := newDropTestGui(t)
			g.window.Resize(fyne.NewSize(1000, 740))
			g.window.Show()
			openTestMermaid(t, g, "flowchart LR\n A --> B")
			settleMarkdownDiagrams(t, g, driver)
			driver.drain()
			view := g.markdownView
			frame := slowMarkdownCropCapture(t, g)
			close(frame.release)
			g.startCrop()
			view.crop.beginDrag(fyne.NewPos(40, 30))
			view.crop.finishDrag(fyne.NewPos(240, 180))
			view.crop.applySelection()
			switch action {
			case "resize":
				g.window.Resize(fyne.NewSize(900, 700))
			case "switch":
				other, err := markdown.Load("other.md", "", strings.NewReader("# Other"))
				if err != nil {
					t.Fatal(err)
				}
				g.addMarkdown(other, nil)
			case "close":
				g.closeWindow()
			case "restart":
				view.cancelCrop()
				g.startCrop()
			}
			captures := frame.captures // Switching may capture the new document thumbnail.
			driver.runNext(t)
			if frame.captures != captures || g.working {
				t.Fatal("obsolete confirmation captured the canvas")
			}
			if action == "restart" && !view.crop.active {
				t.Fatal("obsolete confirmation canceled the new selection")
			}
			if action != "close" {
				g.closeWindow()
			}
		})
	}
}
