package gui

import (
	"emanuelelongo/proview/pkg/annotations"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

func TestTemporarySelectionMovesAndResizesNewObjects(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		tool  annotationTool
		shape annotations.ShapeKind
		pdf   bool
	}{
		{name: "text", tool: toolText},
		{name: "drawing", tool: toolDraw},
		{name: "highlight", tool: toolHighlight},
		{name: "shape", tool: toolShapes, shape: annotations.Rectangle},
		{name: "magnifier", tool: toolShapes, shape: annotations.Magnifier},
		{name: "PDF shape", tool: toolShapes, shape: annotations.Rectangle, pdf: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			g, layer := annotationTestGui(t)
			if scenario.pdf {
				g, layer = redactTestGui(t)
			}
			g.annotationActions = map[annotationTool]*toggleToolbarAction{
				toolSelect:    newToggleToolbarAction(tablerIcon("pointer"), nil),
				scenario.tool: newToggleToolbarAction(tablerIcon("text-outline"), nil),
			}
			g.setAnnotationTool(scenario.tool)
			data := g.annotationDefaults[scenario.tool.kind()].Clone()
			data.Text = "Example"
			data.PageWidth, data.PageHeight = g.annotationPageSize(layer)
			data.Points = []annotations.Point{{X: 100, Y: 100}, {X: 160, Y: 160}}
			if scenario.tool == toolHighlight {
				data.Points = nil
				data.Quads = []annotations.Quad{{{X: 100, Y: 100}, {X: 160, Y: 100}, {X: 160, Y: 130}, {X: 100, Y: 130}}}
			}
			if scenario.tool == toolShapes {
				data.Shape = scenario.shape
			}
			g.addPageAnnotation(layer, data, 100, 100, nil)
			finishAnnotationJobs(t, g)
			original := g.document.Overlays()[0].Rect
			g.handleSelectionShift(desktop.KeyShiftLeft, true)
			if g.effectiveAnnotationTool() != toolSelect || layer.input.Visible() || g.annotationTool != scenario.tool {
				t.Fatal("Shift did not temporarily expose selection")
			}
			if g.annotationActions[toolSelect].button.Importance != widget.HighImportance {
				t.Fatal("temporary selection icon is not active")
			}
			object := layer.widgets[0]
			object.Tapped(&fyne.PointEvent{})
			object.resize(signatureBottomRight, fyne.Delta{DX: 15, DY: 15})
			object.finishChange()
			finishAnnotationJobs(t, g)
			resized := g.document.Overlays()[0].Rect
			if scenario.tool == toolHighlight {
				if resized != original || !object.selected || !object.border.Visible() {
					t.Fatal("Highlight should remain selected without resizing")
				}
				for _, handle := range object.handles {
					if handle.Visible() {
						t.Fatal("Highlight exposes a resize handle")
					}
				}
			} else if resized == original {
				t.Fatal("temporary selection did not resize the object")
			}
			object.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DX: 20, DY: 10}})
			g.handleSelectionShift(desktop.KeyShiftRight, true)
			g.handleSelectionShift(desktop.KeyShiftLeft, false)
			if g.effectiveAnnotationTool() != toolSelect {
				t.Fatal("selection ended while the other Shift key was held")
			}
			g.handleSelectionShift(desktop.KeyShiftRight, false)
			object.DragEnd()
			inputVisible := layer.input.Visible()
			if scenario.tool == toolHighlight {
				inputVisible = layer.highlightInput.Visible()
			}
			if g.effectiveAnnotationTool() != scenario.tool || !inputVisible || len(g.document.Overlays()) != 1 {
				t.Fatal("releasing Shift did not restore the insertion tool")
			}
			if g.hasSelectedSignature || object.selected {
				t.Fatal("releasing Shift left temporary resize handles visible")
			}
			if g.annotationActions[toolSelect].button.Importance != widget.LowImportance || g.annotationActions[scenario.tool].button.Importance != widget.HighImportance {
				t.Fatal("tool icons did not return to the insertion tool")
			}
			if scenario.tool == toolHighlight {
				if g.document.Overlays()[0].Rect != original || object.editActive {
					t.Fatal("highlight moved or started a history edit")
				}
				// The next undo must remove creation, not undo a blocked drag.
				if !g.document.Undo() || len(g.document.Overlays()) != 0 {
					t.Fatal("highlight drag added an Undo step")
				}
				return
			}
			if g.document.Overlays()[0].Rect == resized || !g.document.Undo() || g.document.Overlays()[0].Rect != resized {
				t.Fatal("move did not create one Undo step")
			}
			if scenario.tool != toolHighlight && (!g.document.Undo() || g.document.Overlays()[0].Rect != original) {
				t.Fatal("resize did not create one Undo step")
			}
		})
	}
}

func TestTemporarySelectionPreservesTypingAndDrawingGesture(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolText)
	g.beginTextPlacement(layer, annotations.Point{X: 100, Y: 100})
	g.handleSelectionShift(desktop.KeyShiftLeft, true)
	if g.temporarySelection || g.window.Canvas().Focused() != g.textEntry {
		t.Fatal("Shift interrupted text entry")
	}
	g.handleSelectionShift(desktop.KeyShiftLeft, false)
	g.window.Canvas().Focus(nil)
	g.setAnnotationTool(toolShapes)
	layer.input.MouseDown(mouseAt(100, 250))
	layer.input.MouseMoved(mouseAt(200, 350))
	g.handleSelectionShift(desktop.KeyShiftLeft, true)
	if g.temporarySelection || !layer.input.active {
		t.Fatal("Shift interrupted shape drawing")
	}
	layer.input.MouseUp(mouseAt(200, 350))
	finishAnnotationJobs(t, g)
	if !g.temporarySelection || len(g.document.Overlays()) != 1 {
		t.Fatal("held Shift did not enable selection after the drawing gesture")
	}
	g.setAnnotationTool(toolDraw)
	g.handleSelectionShift(desktop.KeyShiftLeft, false)
	if g.effectiveAnnotationTool() != toolDraw {
		t.Fatal("Shift release overrode an explicit tool change")
	}
}

func TestTemporarySelectionKeepsPropertyRowDisabled(t *testing.T) {
	g, _ := annotationTestGui(t)
	g.newToolPropertyPanel()
	g.setAnnotationTool(toolDraw)
	if !g.annotationControls.Visible() || g.annotationPropertyScroll == nil || !g.annotationPropertyScroll.Visible() {
		t.Fatal("drawing property row is not visible")
	}
	row := g.annotationControls.Objects[0]
	height := g.annotationPropertyScroll.MinSize().Height
	g.handleSelectionShift(desktop.KeyShiftLeft, true)
	if g.currentToolIndicator.icon.Resource.Name() != tablerIcon("pointer").Name() {
		t.Fatal("temporary Select is missing from the tool indicator")
	}
	if !g.annotationControls.Visible() || !g.annotationPropertyScroll.Visible() || g.annotationControls.Objects[0] != row || g.annotationPropertyScroll.MinSize().Height != height {
		t.Fatal("temporary selection removed or resized the property row")
	}
	disabled := 0
	var inspect func(fyne.CanvasObject)
	inspect = func(object fyne.CanvasObject) {
		if group, ok := object.(*fyne.Container); ok {
			for _, child := range group.Objects {
				inspect(child)
			}
		}
		if control, ok := object.(fyne.Disableable); ok {
			if !control.Disabled() {
				t.Fatalf("%T remained enabled during temporary selection", object)
			}
			disabled++
		}
	}
	inspect(g.annotationControls)
	if disabled == 0 {
		t.Fatal("property row has no disabled controls")
	}
	g.handleSelectionShift(desktop.KeyShiftLeft, false)
	if g.currentToolIndicator.icon.Resource.Name() != tablerIcon("sketching").Name() {
		t.Fatal("tool indicator did not restore Draw after releasing Shift")
	}
	if !g.annotationControls.Visible() || !g.annotationPropertyScroll.Visible() {
		t.Fatal("property row did not remain visible after releasing Shift")
	}
}

func TestControlConstraintCombinesWithTemporarySelection(t *testing.T) {
	if constrainShapeProportions(fyne.KeyModifierShift) {
		t.Fatal("Shift still constrains shape proportions")
	}
	if !constrainShapeProportions(fyne.KeyModifierControl) || !constrainShapeProportions(fyne.KeyModifierShift|fyne.KeyModifierControl) {
		t.Fatal("Control does not constrain shape proportions")
	}
	if !allowsTemporarySelection(fyne.KeyModifierControl) || !allowsTemporarySelection(fyne.KeyModifierShift|fyne.KeyModifierControl) {
		t.Fatal("Control prevents temporary selection")
	}
	if allowsTemporarySelection(fyne.KeyModifierAlt) || allowsTemporarySelection(fyne.KeyModifierSuper) {
		t.Fatal("unrelated modifiers allow temporary selection")
	}
}
