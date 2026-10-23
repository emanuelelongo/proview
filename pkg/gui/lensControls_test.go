package gui

import (
	"testing"

	"emanuelelongo/proview/pkg/annotations"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestNewLensMagnificationChangesWithoutMoving(t *testing.T) {
	g, layer := annotationTestGui(t)
	t.Cleanup(func() { g.closed = true })
	g.setAnnotationTool(toolShapes)
	data := g.annotationDefaults[annotations.Shape]
	data.Shape = annotations.Magnifier
	g.annotationDefaults[annotations.Shape] = data
	input := layer.input
	input.MouseDown(mouseAt(100, 250))
	input.MouseMoved(mouseAt(300, 450))
	input.MouseUp(mouseAt(300, 450))
	finishAnnotationJobs(t, g)
	original := g.document.Overlays()[0]
	if !g.hasSelectedSignature || g.selectedSignature != original.ID || g.annotationTool != toolShapes {
		t.Fatal("new lens is not connected to its controls")
	}
	for _, object := range fyneTest.LaidOutObjects(g.annotationControls) {
		if button, ok := object.(*iconButton); ok && button.tooltip == "Magnification" {
			fyneTest.Tap(button)
			break
		}
	}
	var slider *widget.Slider
	for _, object := range fyneTest.LaidOutObjects(g.window.Canvas().Overlays().Top()) {
		if s, ok := object.(*widget.Slider); ok {
			slider = s
			break
		}
	}
	if slider == nil {
		t.Fatal("magnification slider missing")
	}
	slider.SetValue(4)
	slider.OnChangeEnded(4)
	finishAnnotationJobs(t, g)
	updated := g.document.Overlays()[0]
	actual, _ := updated.Annotation()
	if actual.Magnification != 4 || updated.Rect != original.Rect || updated.ID != original.ID {
		t.Fatalf("slider: magnification=%v rect=%+v original=%+v ID=%v originalID=%v", actual.Magnification, updated.Rect, original.Rect, updated.ID, original.ID)
	}
	if g.annotationDefaults[annotations.Shape].Magnification != data.Magnification {
		t.Fatal("object editing changed insertion defaults")
	}
	if !g.document.Undo() {
		t.Fatal("magnification cannot undo")
	}
	actual, _ = g.document.Overlays()[0].Annotation()
	if actual.Magnification != data.Magnification {
		t.Fatal("magnification did not undo in one step")
	}
}
