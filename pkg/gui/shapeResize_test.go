package gui

import (
	"bytes"
	"math"
	"testing"

	"emanuelelongo/proview/pkg/annotations"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

func TestHorizontalLineResizeKeepsItsDirectionAndThickness(t *testing.T) {
	g, layer := annotationTestGui(t)
	data := annotations.Default(annotations.Shape)
	data.Shape = annotations.Line
	data.PageWidth, data.PageHeight = 1000, 500
	data.Points = []annotations.Point{{X: 100, Y: 100}, {X: 250, Y: 100}}
	g.addPageAnnotation(layer, data, 0, 0, nil)
	finishAnnotationJobs(t, g)
	g.setAnnotationTool(toolSelect)
	original := g.document.Overlays()[0]
	g.selectSignature(layer, original.ID)
	w := layer.widgets[0]
	w.resize(signatureBottomRight, fyne.NewDelta(60, 0))
	w.finishChange()
	finishAnnotationJobs(t, g)
	actual := g.document.Overlays()[0]
	resized, _ := actual.Annotation()
	if actual.Rect.Width <= original.Rect.Width || math.Abs(actual.Rect.Height-original.Rect.Height) > 1e-6 || math.Abs(resized.Points[1].Y-resized.Points[0].Y) > .001 || resized.Width != data.Width {
		t.Fatal("length resize changed horizontal line direction/thickness")
	}
}

func TestShapeResizeCoalescesPointerUpdatesAndSaveWaitsForGeometry(t *testing.T) {
	g, layer := annotationTestGui(t)
	data := annotations.Default(annotations.Shape)
	data.Shape = annotations.Arrow
	data.PageWidth, data.PageHeight = 1000, 500
	data.Points = []annotations.Point{{X: 100, Y: 100}, {X: 250, Y: 180}}
	g.addPageAnnotation(layer, data, 0, 0, nil)
	finishAnnotationJobs(t, g)
	g.document.MarkSaved()
	g.setAnnotationTool(toolSelect)
	original := g.document.Overlays()[0]
	g.selectSignature(layer, original.ID)
	w := layer.widgets[0]
	w.resize(signatureBottomRight, fyne.NewDelta(30, 0))
	w.resize(signatureBottomRight, fyne.NewDelta(40, 20))
	if g.annotationPending != 1 || len(g.annotationWork) != 0 {
		t.Fatal("resize queued every pointer update")
	}
	latest := w.rect
	completed := false
	if !g.waitAnnotationChanges(func() { completed = true }) || completed {
		t.Fatal("save did not wait for resized geometry")
	}
	finishAnnotationJobs(t, g)
	actual := g.document.Overlays()[0]
	newData, _ := actual.Annotation()
	if !completed || w.editActive || actual.Rect != latest || bytes.Equal(original.Asset().Data, actual.Asset().Data) || newData.Width != data.Width {
		t.Fatal("final resize did not publish latest regenerated artwork")
	}
	if !g.document.Undo() || g.document.Dirty() || !bytes.Equal(g.document.Overlays()[0].Asset().Data, original.Asset().Data) {
		t.Fatal("coalesced resize was not one complete Undo")
	}
}

func TestCanceledShapeResizeIgnoresPendingWorker(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.image = canvas.NewImageFromImage(nil)
	g.imageContent = container.NewWithoutLayout(g.image, layer)
	g.zoom = 1
	data := annotations.Default(annotations.Shape)
	data.PageWidth, data.PageHeight = 1000, 500
	data.Points = []annotations.Point{{X: 100, Y: 100}, {X: 250, Y: 180}}
	g.addPageAnnotation(layer, data, 0, 0, nil)
	finishAnnotationJobs(t, g)
	g.document.MarkSaved()
	g.setAnnotationTool(toolSelect)
	original := g.document.Overlays()[0]
	g.selectSignature(layer, original.ID)
	w := layer.widgets[0]
	w.resize(signatureBottomRight, fyne.NewDelta(30, 20))
	if !g.cancelCurrentOperation() || w.editActive {
		t.Fatal("Escape did not cancel resize")
	}
	finishAnnotationJobs(t, g)
	actual := g.document.Overlays()[0]
	if g.document.Dirty() || actual.Rect != original.Rect || !bytes.Equal(actual.Asset().Data, original.Asset().Data) {
		t.Fatal("stale shape worker changed canceled geometry")
	}
}
