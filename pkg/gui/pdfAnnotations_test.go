package gui

import (
	"bytes"
	"image"
	"testing"

	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

func TestReopenedPDFAnnotationSelectionAndLockedSignature(t *testing.T) {
	g, layer := redactTestGui(t)
	g.setAnnotationTool(toolText)
	for _, kind := range []annotations.Kind{annotations.Text, annotations.Highlight} {
		data := annotations.Default(kind)
		data.Text = "Editable after reopening"
		data.PageWidth, data.PageHeight = g.annotationPageSize(layer)
		if kind == annotations.Highlight {
			data.Quads = []annotations.Quad{{{X: 30, Y: 30}, {X: 100, Y: 30}, {X: 100, Y: 50}, {X: 30, Y: 50}}}
		}
		g.addPageAnnotation(layer, data, 40, 60, nil)
		finishAnnotationJobs(t, g)
	}
	if _, err := g.document.AddSignature(0, document.NormalizedRect{X: .6, Y: .6, Width: .2, Height: .2}, testSignature(t, 30, 30)); err != nil {
		t.Fatal(err)
	}
	raw, err := g.document.Export(document.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("saved.pdf", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { doc.Close() })
	g.document = doc
	layer.SetDocument(doc, 0, image.Pt(600, 400))
	g.setAnnotationTool(toolSelect)
	if len(layer.widgets) != 3 {
		t.Fatalf("objects=%d", len(layer.widgets))
	}
	for i, w := range layer.widgets {
		w.Tapped(&fyne.PointEvent{})
		if i == 2 {
			old := doc.Overlays()[i].Rect
			w.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(20, 10)})
			w.DragEnd()
			w.resize(signatureBottomRight, fyne.NewDelta(20, 10))
			if w.selected || !w.readOnly || doc.Overlays()[i].Rect != old {
				t.Fatal("reopened signature accepted editing")
			}
			continue
		}
		if !w.selected || w.annotation == nil {
			t.Fatal("reopened annotation cannot be selected")
		}
		if i == 0 && w.annotation.Text != "Editable after reopening" {
			t.Fatal("text properties lost")
		}
		if i == 1 {
			old := doc.Overlays()[i].Rect
			w.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(20, 10)})
			w.DragEnd()
			w.resize(signatureBottomRight, fyne.NewDelta(20, 10))
			if w.canResize() || doc.Overlays()[i].Rect != old {
				t.Fatal("reopened highlight accepted move or resize")
			}
			for _, h := range w.handles {
				if h.Visible() {
					t.Fatal("highlight exposes resize handles")
				}
			}
		}
	}
}

func TestObjectSelectsOnMouseDownWithoutWaitingForDoubleClick(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolSelect)
	data := annotations.Default(annotations.Text)
	data.Text = "Select immediately"
	g.addPageAnnotation(layer, data, 40, 60, nil)
	finishAnnotationJobs(t, g)
	g.clearSignatureSelection()
	w := layer.widgets[0]
	w.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	if !w.selected {
		t.Fatal("selection waits for delayed Tapped")
	}
	w.DoubleTapped(&fyne.PointEvent{})
	if g.textPlacement == nil {
		t.Fatal("immediate selection broke double click editing")
	}
}
