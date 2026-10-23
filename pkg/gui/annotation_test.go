package gui

import (
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/document"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// Run expensive jobs explicitly in tests, keeping all UI/model access on this
// goroutine while exercising the same queued completion callbacks as the app.
func finishAnnotationJobs(t *testing.T, g *Gui) {
	t.Helper()
	for g.annotationPending > 0 {
		select {
		case job := <-g.annotationJobs:
			done := job()
			done()
			g.annotationPending--
			g.annotationRunning = false
			g.dispatchAnnotationJob()
		default:
			t.Fatal("pending annotation without a queued job")
		}
	}
	g.working = false
	g.updateTemporarySelection()
	after := g.annotationAfterIdle
	g.annotationAfterIdle = nil
	for _, action := range after {
		action()
	}
}
func annotationTestGui(t *testing.T) (*Gui, *signatureLayer) {
	t.Helper()
	app := fyneTest.NewApp()
	t.Cleanup(app.Quit)
	doc := testDocument(t, 1000, 500)
	g := &Gui{app: app, window: app.NewWindow("Test"), document: doc, cropOverlay: newCropOverlay(), annotationControls: container.NewVBox(), annotationJobs: make(chan func() func(), 1)}
	layer := newSignatureLayer(doc, 0, image.Pt(1000, 500), g.selectSignature, nil, g.documentChanged)
	g.signatureLayers = map[int]*signatureLayer{0: layer}
	g.imageSignatureLayer = layer
	layer.Resize(fyne.NewSize(1000, 800))
	g.bindAnnotationLayer(layer)
	g.window.SetContent(layer)
	layer.Resize(fyne.NewSize(1000, 800))
	return g, layer
}
func mouseAt(x, y float32) *desktop.MouseEvent {
	return &desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(x, y)}, Button: desktop.MouseButtonPrimary}
}
func TestPageDrawingLivePreviewSeparateStrokesAndUndo(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolDraw)
	p := layer.input
	p.MouseDown(mouseAt(100, 250))
	p.MouseMoved(mouseAt(200, 300))
	if !p.active || p.live == nil || p.frame.NRGBAAt(300, 250).A == 0 {
		t.Fatal("stroke was invisible while mouse remained pressed")
	}
	if len(g.document.Overlays()) != 0 {
		t.Fatal("draft was committed before release")
	}
	p.MouseUp(mouseAt(220, 300))
	p.MouseDown(mouseAt(300, 350))
	p.MouseUp(mouseAt(300, 350))
	if g.annotationPending != 2 || len(p.pending) != 2 {
		t.Fatal("quick consecutive strokes were dropped")
	}
	finishAnnotationJobs(t, g)
	if len(g.document.Overlays()) != 2 || len(p.pending) != 0 {
		t.Fatal("pending strokes did not become independent objects")
	}
	first, _ := g.document.Overlays()[0].Annotation()
	second, _ := g.document.Overlays()[1].Annotation()
	if first.Points[0] != (annotations.Point{X: 100, Y: 100}) || len(second.Points) != 1 {
		t.Fatal("letterboxing coordinates or isolated dot were lost")
	}
	if !g.document.Undo() || len(g.document.Overlays()) != 1 {
		t.Fatal("undo did not remove only the last stroke")
	}
	p.MouseDown(mouseAt(50, 200))
	p.MouseMoved(mouseAt(80, 210))
	if !g.cancelCurrentOperation() || p.active || p.live != nil || len(g.document.Overlays()) != 1 {
		t.Fatal("Escape did not cancel the live draft")
	}
	if !g.cancelCurrentOperation() || g.annotationTool != toolSelect {
		t.Fatal("Escape did not return to selection")
	}
}
func TestPageTextInsertionEditingAndStableStacking(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolText)
	placeTestText(t, g, layer, fyne.NewPos(100, 250), "Sample text")
	if g.annotationTool != toolText || g.hasSelectedSignature || g.textEntry != nil || len(g.document.Overlays()) != 1 {
		t.Fatal("text insertion did not clear the entry for the next object")
	}
	o := g.document.Overlays()[0]
	g.selectSignature(layer, o.ID)
	original := o.Rect
	updated, _ := o.Annotation()
	updated.Size = 36
	updated.Strikethrough = true
	updated.Color = color.NRGBA{R: 200, A: 255}
	// Several previews followed by one final change must be one undo step.
	g.renderAnnotationProperties(o, updated, false)
	g.renderAnnotationProperties(o, updated, true)
	finishAnnotationJobs(t, g)
	current := g.document.Overlays()[0]
	data, _ := current.Annotation()
	if !data.Strikethrough || data.Size != 36 || current.Rect.Width <= original.Width {
		t.Fatal("text properties did not update geometry")
	}
	if !g.document.Undo() || g.document.Overlays()[0].Rect != original {
		t.Fatal("property gesture was not one undo operation")
	}
	layer.reload()
	other, _ := g.document.AddSignature(0, document.NormalizedRect{X: .1, Y: .1, Width: .2, Height: .1}, testSignature(t, 2, 1))
	layer.reload()
	layer.selectSignature(o.ID)
	if layer.widgets[len(layer.widgets)-1].id != other.ID {
		t.Fatal("selection reordered objects")
	}
}

func TestDrawingClipsExitBeforeAnotherPageAndKeepsZoomIndependentWidth(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolDraw)
	p := layer.input
	p.MouseDown(mouseAt(900, 250))
	p.MouseOut()
	p.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(1200, 450)}})
	if p.active || g.annotationPending != 1 {
		t.Fatal("leaving page did not commit the clipped stroke")
	}
	p.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(800, 400)}})
	finishAnnotationJobs(t, g)
	d, _ := g.document.Overlays()[0].Annotation()
	last := d.Points[len(d.Points)-1]
	if math.Abs(last.X-1000) > .001 || math.Abs(last.Y-(100+200./3)) > .001 {
		t.Fatalf("edge intersection = %v", last)
	}
	layer.Resize(fyne.NewSize(2000, 1000))
	p.MouseDown(mouseAt(200, 200))
	p.MouseUp(mouseAt(400, 400))
	finishAnnotationJobs(t, g)
	zoomed, _ := g.document.Overlays()[1].Annotation()
	if zoomed.Width != 2 || zoomed.Points[0] != (annotations.Point{X: 100, Y: 100}) {
		t.Fatal("zoom changed physical coordinates or brush width")
	}
}

func TestAnnotationPropertyControlsFitDefaultWindow(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.newToolPropertyPanel()
	toolbar := widget.NewLabel("Toolbar")
	viewport := container.NewStack(canvas.NewRectangle(color.White), layer)
	g.window.SetContent(container.NewBorder(toolbar, nil, nil, g.toolPropertyPanel, viewport))
	g.window.Resize(fyne.NewSize(1000, 700))
	if !g.toolPropertyPanel.Visible() {
		t.Fatal("property rail must always reserve its space")
	}
	g.setAnnotationTool(toolText)
	panelWidth := g.toolPropertyPanel.MinSize().Width
	if panelWidth > 52 {
		t.Fatalf("property rail is wider than a padded icon column: %v", panelWidth)
	}
	for _, size := range []fyne.Size{fyne.NewSize(1000, 700), fyne.NewSize(800, 300)} {
		g.window.Resize(size)
		if !g.toolPropertyPanel.Visible() || g.toolPropertyPanel.Position().X < viewport.Position().X+viewport.Size().Width {
			t.Fatal("properties are not on the right of the document")
		}
		if g.toolPropertyPanel.Size().Width != panelWidth || g.toolPropertyPanel.Position().Y != viewport.Position().Y {
			t.Fatal("property panel moved or changed width after resizing")
		}
		if g.annotationControls.MinSize().Width > g.annotationPropertyScroll.Size().Width {
			t.Fatal("text controls exceed the panel width")
		}
		column := g.annotationControls.Objects[0].(*fyne.Container)
		for i := 1; i < len(column.Objects); i++ {
			previous, current := column.Objects[i-1], column.Objects[i]
			if current.Position().Y < previous.Position().Y+previous.Size().Height {
				t.Fatal("property controls are not stacked vertically")
			}
		}
	}
	if g.annotationPropertyScroll.Direction != container.ScrollVerticalOnly || g.annotationPropertyScroll.MinSize().Height >= g.annotationControls.MinSize().Height {
		t.Fatal("tall property lists should scroll without increasing the window minimum height")
	}
	beforePosition, beforeSize := viewport.Position(), viewport.Size()
	for _, tool := range []annotationTool{toolSelect, toolDraw, toolHighlight, toolShapes, toolText} {
		g.setAnnotationTool(tool)
		if !g.toolPropertyPanel.Visible() || viewport.Position() != beforePosition || viewport.Size() != beforeSize {
			t.Fatal("switching tools moved or resized the document viewport")
		}
		icon := map[annotationTool]string{toolSelect: "pointer", toolDraw: "sketching", toolHighlight: "highlight", toolShapes: "circle-square", toolText: "text-outline"}[tool]
		if tool == toolSelect {
			if g.toolIndicatorHeader.Visible() {
				t.Fatal("basic Select should have no tool heading")
			}
		} else if !g.toolIndicatorHeader.Visible() || g.currentToolIndicator.icon.Resource.Name() != tablerIcon(icon).Name() {
			t.Fatal("property header does not identify the current tool")
		}
	}
	g.setAnnotationTool(toolText)
	g.window.Resize(fyne.NewSize(1000, 700))
	if path := os.Getenv("PROVIEW_ANNOTATION_PREVIEW"); path != "" {
		g.window.Show()
		origin, _ := layer.displayRect()
		layer.input.Tapped(&fyne.PointEvent{Position: origin.Add(fyne.NewPos(120, 120))})
		g.textEntry.SetText("Write directly on the page")
		finishAnnotationJobs(t, g)
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, g.window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}
}
