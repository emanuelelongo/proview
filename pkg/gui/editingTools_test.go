package gui

import (
	"bytes"
	"context"
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/document"
	"emanuelelongo/proview/pkg/signatures"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"golang.design/x/clipboard"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"testing"
)

func TestSelectedTextResizeHandlesStayOutsideArtwork(t *testing.T) {
	data := annotations.Default(annotations.Text)
	widget := &signatureWidget{
		selected:   true,
		annotation: &data,
		image:      canvas.NewImageFromImage(image.NewNRGBA(image.Rect(0, 0, 100, 30))),
		border:     canvas.NewRectangle(color.Transparent),
	}
	widget.handles = []*signatureResizeHandle{
		newSignatureResizeHandle(widget, signatureTopLeft),
		newSignatureResizeHandle(widget, signatureTopRight),
		newSignatureResizeHandle(widget, signatureBottomLeft),
		newSignatureResizeHandle(widget, signatureBottomRight),
	}
	renderer := widget.CreateRenderer()
	renderer.Layout(fyne.NewSize(124, 54))
	if widget.image.Position() != fyne.NewPos(12, 12) || widget.image.Size() != fyne.NewSize(100, 30) {
		t.Fatalf("text artwork layout = %v %v", widget.image.Position(), widget.image.Size())
	}
	if widget.handles[0].Position().X+widget.handles[0].Size().Width > widget.image.Position().X ||
		widget.handles[1].Position().X < widget.image.Position().X+widget.image.Size().Width {
		t.Fatal("horizontal resize handles overlap the text artwork")
	}
}

type memoryImageClipboard struct {
	data          []byte
	objectData    []byte
	reads, writes int
}

func (c *memoryImageClipboard) Read(context.Context) ([]byte, error) {
	c.reads++
	return bytes.Clone(c.data), nil
}
func (c *memoryImageClipboard) Write(_ context.Context, data []byte) error {
	c.writes++
	c.objectData = nil
	c.data = bytes.Clone(data)
	return nil
}
func TestAreaCopyPasteAndTextClipboardIsolation(t *testing.T) {
	g, layer := annotationTestGui(t)
	mem := &memoryImageClipboard{}
	previous := desktopImageClipboard
	desktopImageClipboard = mem
	t.Cleanup(func() { desktopImageClipboard = previous })
	layer.MouseDown(mouseAt(100, 200))
	layer.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(400, 400)}})
	layer.MouseUp(mouseAt(400, 400))
	if g.areaSelection == nil || g.areaSelection.active || g.document.Dirty() {
		t.Fatal("area selection changes document")
	}
	g.copyArea()
	finishAnnotationJobs(t, g)
	p, e := png.Decode(bytes.NewReader(mem.data))
	if e != nil {
		t.Fatal(e)
	}
	if p.Bounds().Dx() != 300 || p.Bounds().Dy() != 200 || mem.writes != 1 {
		t.Fatalf("copied bounds: %v", p.Bounds())
	}
	g.pasteImage()
	finishAnnotationJobs(t, g)
	if len(g.document.Overlays()) != 1 || !g.hasSelectedSignature || g.document.Overlays()[0].ObjectKind != "image" {
		t.Fatal("paste did not create selected image")
	}
	if g.areaSelection != nil {
		t.Fatal("paste left stale selection")
	}
	if !g.document.Undo() || len(g.document.Overlays()) != 0 {
		t.Fatal("paste Undo failed")
	}
	g.setAnnotationTool(toolText)
	g.beginTextPlacement(g.imageSignatureLayer, annotations.Point{X: 100, Y: 100})
	reads, writes := mem.reads, mem.writes
	g.copyArea()
	g.pasteImage()
	if mem.reads != reads || mem.writes != writes {
		t.Fatal("text shortcuts accessed image clipboard")
	}
}
func TestPDFAreaCopyPasteWithoutObjectFormat(t *testing.T) {
	g, _ := redactTestGui(t)
	mem := &memoryImageClipboard{}
	previous := desktopImageClipboard
	desktopImageClipboard = mem
	t.Cleanup(func() { desktopImageClipboard = previous })
	g.areaSelection = &areaSelection{page: 0, rect: document.NormalizedRect{X: .1, Y: .1, Width: .4, Height: .3}}
	g.copyArea()
	finishAnnotationJobs(t, g)
	if len(mem.data) == 0 || len(mem.objectData) != 0 || g.document.Dirty() {
		t.Fatal("PDF area copy must publish only an image without changing the document")
	}
	g.pasteImage()
	finishAnnotationJobs(t, g)
	overlays := g.document.Overlays()
	if len(overlays) != 1 || overlays[0].ObjectKind != "image" || overlays[0].Page != 0 || !g.hasSelectedSignature {
		t.Fatal("PDF area paste did not create a selected image on the current page")
	}
	if !g.document.Undo() || len(g.document.Overlays()) != 0 || g.document.Dirty() {
		t.Fatal("PDF area paste must be a single Undo step")
	}
	if !g.document.Redo() || len(g.document.Overlays()) != 1 {
		t.Fatal("PDF area paste Redo failed")
	}
}
func TestTextInsertionClearsEntryAndDefaults(t *testing.T) {
	g, l := annotationTestGui(t)
	g.setAnnotationTool(toolText)
	placeTestText(t, g, l, fyne.NewPos(100, 250), "First")
	if g.annotationTool != toolText || g.hasSelectedSignature || g.textEntry != nil || g.annotationDefaults[annotations.Text].Text != "" {
		t.Fatal("text insertion did not reset the text control")
	}
	placeTestText(t, g, l, fyne.NewPos(350, 250), "Second")
	if len(g.document.Overlays()) != 2 {
		t.Fatal("next text missing")
	}
	d, _ := g.document.Overlays()[1].Annotation()
	if d.Text != "Second" {
		t.Fatal("stale text defaults")
	}
}
func TestShapesPreviewAndIndependentResize(t *testing.T) {
	for _, name := range annotations.Shapes {
		t.Run(name, func(t *testing.T) {
			g, l := annotationTestGui(t)
			t.Cleanup(func() { g.closed = true })
			g.setAnnotationTool(toolShapes)
			d := g.annotationDefaults[annotations.Shape]
			d.Shape = annotations.ShapeKind(name)
			g.annotationDefaults[annotations.Shape] = d
			p := l.input
			p.MouseDown(mouseAt(100, 250))
			p.MouseMoved(mouseAt(300, 400))
			if p.live == nil || p.liveRect == nil || len(g.document.Overlays()) != 0 {
				t.Fatal("shape missing live preview")
			}
			p.MouseUp(mouseAt(300, 400))
			finishAnnotationJobs(t, g)
			if len(g.document.Overlays()) != 1 {
				t.Fatal("shape missing")
			}
			o := g.document.Overlays()[0]
			if o.ObjectKind != "shape" {
				t.Fatal("shape type lost")
			}
			data, _ := o.Annotation()
			if data.Shape == annotations.Magnifier {
				if o.Rect.Width*1000-o.Rect.Height*500 > 1 {
					t.Fatal("magnifier is not circular")
				}
				return
			}
			g.setAnnotationTool(toolSelect)
			g.selectSignature(l, o.ID)
			w := l.widgets[0]
			old := w.rect
			w.resize(signatureBottomRight, fyne.Delta{DX: 60, DY: 0})
			w.finishChange()
			finishAnnotationJobs(t, g)
			newRect := g.document.Overlays()[0].Rect
			if newRect.Width <= old.Width || math.Abs(newRect.Height-old.Height) > 1e-6 {
				t.Fatalf("shape axes could not resize independently: old=%+v new=%+v", old, newRect)
			}
			if !g.document.Undo() || g.document.Overlays()[0].Rect != old {
				t.Fatal("shape resize Undo failed")
			}
		})
	}
}
func TestAltDragOverObjectCreatesAreaSelection(t *testing.T) {
	g, l := annotationTestGui(t)
	o, e := g.document.AddSignature(0, document.NormalizedRect{X: .2, Y: .2, Width: .3, Height: .3}, testSignature(t, 2, 1))
	if e != nil {
		t.Fatal(e)
	}
	l.reload()
	w := l.widgets[0]
	w.MouseDown(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(5, 5)}, Button: desktop.MouseButtonPrimary, Modifier: fyne.KeyModifierAlt})
	w.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(40, 50)}})
	w.DragEnd()
	if g.areaSelection == nil || g.areaSelection.active || g.document.Overlays()[0].Rect != o.Rect {
		t.Fatal("Alt drag moved object instead of selecting area")
	}
}
func TestNativeImageClipboardBackend(t *testing.T) {
	if os.Getenv("PROVIEW_CLIPBOARD_PROBE") != "1" {
		t.Skip("set PROVIEW_CLIPBOARD_PROBE=1 to initialize and read the native image clipboard")
	}
	if e := initImageClipboard(); e != nil {
		t.Fatal(e)
	}
	_, e := desktopImageClipboard.Read(context.Background())
	if e != nil {
		t.Fatal(e)
	}
}
func TestDrawnSignaturePropertiesPreserveLibraryAsset(t *testing.T) {
	g, l := annotationTestGui(t)
	a, e := signatures.FromDrawing(signatures.Drawing{Version: 1, Width: 4, Smooth: 0, Strokes: [][]signatures.Point{{{X: 100, Y: 100}, {X: 200, Y: 150}}}})
	if e != nil {
		t.Fatal(e)
	}
	p, e := a.Prepare()
	if e != nil {
		t.Fatal(e)
	}
	o, e := g.document.AddSignatureAsset(0, document.NormalizedRect{X: .1, Y: .1, Width: .3, Height: .2}, p)
	if e != nil {
		t.Fatal(e)
	}
	l.reload()
	g.selectSignature(l, o.ID)
	g.replaceSignatureStyle(o, func(a signatures.Asset) (signatures.Asset, error) { return a.WithColor(color.NRGBA{R: 200, A: 255}) })
	finishAnnotationJobs(t, g)
	if bytes.Equal(g.document.Overlays()[0].Asset().Data, a.Data) {
		t.Fatal("signature color unchanged")
	}
	if !g.document.Undo() || !bytes.Equal(g.document.Overlays()[0].Asset().Data, a.Data) {
		t.Fatal("signature style Undo lost original")
	}
}

func TestImportedSignatureThicknessControlAndUndo(t *testing.T) {
	g, l := annotationTestGui(t)
	a, err := signatures.Import("signature.svg", []byte(`<svg viewBox="0 0 100 50"><path d="M10 10L90 40" stroke="black" fill="none"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := a.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	o, err := g.document.AddSignatureAsset(0, document.NormalizedRect{X: .1, Y: .1, Width: .3, Height: .2}, p)
	if err != nil {
		t.Fatal(err)
	}
	l.reload()
	g.selectSignature(l, o.ID)
	row := g.annotationControls.Objects[0].(*fyne.Container)
	if len(row.Objects) != 4 {
		t.Fatal("imported signature has no thickness control next to color")
	}
	g.replaceSignatureStyle(o, func(a signatures.Asset) (signatures.Asset, error) { return a.WithThickness(4) })
	finishAnnotationJobs(t, g)
	if g.document.Overlays()[0].Asset().Thickness != 4 || a.Thickness != 0 {
		t.Fatal("thickness was not applied to the document copy")
	}
	if !g.document.Undo() || g.document.Overlays()[0].Asset().Thickness != 0 {
		t.Fatal("thickness Undo did not restore original")
	}
}

func (c *memoryImageClipboard) ReadObject(context.Context) ([]byte, error) {
	if len(c.objectData) == 0 {
		return nil, clipboard.ErrNoData
	}
	return bytes.Clone(c.objectData), nil
}
func (c *memoryImageClipboard) WriteObject(_ context.Context, pngData, objectData []byte) error {
	c.writes++
	c.data = bytes.Clone(pngData)
	c.objectData = bytes.Clone(objectData)
	return nil
}

func TestObjectClipboardDuplicatesEditableObjectsAcrossWindows(t *testing.T) {
	g, l := annotationTestGui(t)
	mem := &memoryImageClipboard{}
	previous := desktopImageClipboard
	desktopImageClipboard = mem
	t.Cleanup(func() { desktopImageClipboard = previous })
	g.setAnnotationTool(toolText)
	placeTestText(t, g, l, fyne.NewPos(100, 250), "Original text")
	g.setAnnotationTool(toolSelect)
	original := g.document.Overlays()[0]
	g.selectSignature(l, original.ID)
	if e := g.document.TransformOverlay(original.ID, "right"); e != nil {
		t.Fatal(e)
	}
	l.reload()
	g.selectSignature(l, original.ID)
	original = g.document.Overlays()[0]
	g.copyArea()
	finishAnnotationJobs(t, g)
	if len(mem.objectData) == 0 || len(mem.data) == 0 {
		t.Fatal("copy did not publish object and PNG together")
	}
	if g.document.Overlays()[0].Rect != original.Rect {
		t.Fatal("copy moved original")
	}
	g.pasteImage()
	finishAnnotationJobs(t, g)
	objects := g.document.Overlays()
	if len(objects) != 2 {
		t.Fatal("paste missing")
	}
	copy := objects[1]
	data, ok := copy.Annotation()
	if !ok || data.Text != "Original text" || copy.Transform != original.Transform || copy.ID == original.ID || copy.Rect == original.Rect {
		t.Fatal("paste lost editable text, transform or independent identity")
	}
	firstRect := copy.Rect
	g.pasteImage()
	finishAnnotationJobs(t, g)
	if len(g.document.Overlays()) != 3 || g.document.Overlays()[2].Rect == firstRect {
		t.Fatal("repeated paste overlaps preceding duplicate")
	}
	if !g.document.Undo() || len(g.document.Overlays()) != 2 || !g.document.Redo() || len(g.document.Overlays()) != 3 {
		t.Fatal("paste not one Undo step")
	}
	g2, l2 := annotationTestGui(t)
	g2.pasteImage()
	finishAnnotationJobs(t, g2)
	if len(g2.document.Overlays()) != 1 {
		t.Fatal("cross-window paste missing")
	}
	pasted := g2.document.Overlays()[0]
	data, _ = pasted.Annotation()
	data.Text = "Independent copy"
	asset, _, e := data.Artwork()
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := asset.Prepare()
	if e != nil {
		t.Fatal(e)
	}
	if e = g2.document.ReplaceAnnotation(pasted.ID, pasted.Rect, data, prepared); e != nil {
		t.Fatal(e)
	}
	l2.reload()
	sourceData, _ := g.document.Overlays()[0].Annotation()
	if sourceData.Text != "Original text" {
		t.Fatal("editing copy changed original")
	}
	// An external image replaces both representations; no stale object survives.
	mem.Write(context.Background(), testSignature(t, 4, 2))
	g.pasteImage()
	finishAnnotationJobs(t, g)
	last := g.document.Overlays()[len(g.document.Overlays())-1]
	if last.ObjectKind != "image" {
		t.Fatal("external image pasted stale object")
	}
}

func TestTextPlacementRequiresFreshEntry(t *testing.T) {
	g, l := annotationTestGui(t)
	g.setAnnotationTool(toolText)
	placeTestText(t, g, l, fyne.NewPos(100, 250), "First")
	l.input.Tapped(&fyne.PointEvent{Position: fyne.NewPos(350, 250)})
	g.textEntry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	finishAnnotationJobs(t, g)
	if len(g.document.Overlays()) != 1 {
		t.Fatal("an empty entry duplicated the previous text")
	}
	placeTestText(t, g, l, fyne.NewPos(350, 250), "Updated before next placement")
	if len(g.document.Overlays()) != 2 || g.annotationTool != toolText {
		t.Fatal("fresh text was not placed")
	}
	d, _ := g.document.Overlays()[1].Annotation()
	if d.Text != "Updated before next placement" {
		t.Fatal("placement used stale text")
	}
}
