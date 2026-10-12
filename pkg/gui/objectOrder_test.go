package gui

import (
	"emanuelelongo/proview/pkg/document"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestObjectContextMenuReordersAndKeepsSelection(t *testing.T) {
	for _, tool := range []annotationTool{toolSelect, toolText, toolShapes} {
		t.Run(string(rune('0'+tool)), func(t *testing.T) {
			app := fyneTest.NewApp()
			defer app.Quit()
			g := newGui(app)
			g.showDocument(testDocument(t, 100, 100))
			var ids []document.OverlayID
			for range 3 {
				o, err := g.document.AddSignature(0, document.NormalizedRect{X: .1, Y: .1, Width: .6, Height: .6}, testSignature(t, 2, 1))
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, o.ID)
			}
			g.document.MarkSaved()
			layer := g.imageSignatureLayer
			layer.reload()
			layer.Resize(fyne.NewSize(100, 100))
			g.setAnnotationTool(tool)
			layer.Resize(fyne.NewSize(100, 100))
			position, size := layer.widgetRect(layer.widgets[2].rect)
			center := position.Add(fyne.NewPos(size.Width/2, size.Height/2))
			event := &fyne.PointEvent{Position: center, AbsolutePosition: center}
			if tool == toolSelect {
				layer.widgets[2].TappedSecondary(event)
			} else {
				layer.input.TappedSecondary(event)
			}
			if g.objectOrderMenu == nil || g.selectedSignature != ids[2] {
				t.Fatal("right-click did not select the topmost object and open its menu")
			}
			menu := g.objectOrderMenu
			if len(menu.Items) != 4 {
				t.Fatal("object menu did not offer four ordering actions")
			}
			revision := g.document.Revision()
			fyneTest.Tap(menu.Items[0].(fyne.Tappable))
			fyneTest.Tap(menu.Items[1].(fyne.Tappable))
			if g.document.Revision() != revision {
				t.Fatal("frontmost object allowed forward moves")
			}
			fyneTest.Tap(menu.Items[2].(fyne.Tappable))
			assertOrder := func(want []document.OverlayID) {
				t.Helper()
				var got []document.OverlayID
				for _, w := range layer.widgets {
					got = append(got, w.id)
				}
				if !slices.Equal(got, want) {
					t.Fatalf("visible order = %v, want %v", got, want)
				}
			}
			assertOrder([]document.OverlayID{ids[0], ids[2], ids[1]})
			if !g.hasSelectedSignature || g.selectedSignature != ids[2] || !g.document.Dirty() || g.objectOrderMenu != nil {
				t.Fatal("reorder lost selection, dirty state, or left its menu open")
			}
			layer.widgets[1].TappedSecondary(event)
			fyneTest.Tap(g.objectOrderMenu.Items[3].(fyne.Tappable))
			assertOrder([]document.OverlayID{ids[2], ids[0], ids[1]})
			g.undo()
			assertOrder([]document.OverlayID{ids[0], ids[2], ids[1]})
			g.redo()
			assertOrder([]document.OverlayID{ids[2], ids[0], ids[1]})
			layer.widgets[0].TappedSecondary(event)
			revision = g.document.Revision()
			fyneTest.Tap(g.objectOrderMenu.Items[2].(fyne.Tappable))
			fyneTest.Tap(g.objectOrderMenu.Items[3].(fyne.Tappable))
			if g.document.Revision() != revision {
				t.Fatal("backmost object allowed backward moves")
			}
			if !g.cancelCurrentOperation() || g.objectOrderMenu != nil {
				t.Fatal("Escape did not close the object menu")
			}
		})
	}
}

func TestObjectContextMenuRejectsReplacedDocument(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.showDocument(testDocument(t, 100, 100))
	for range 2 {
		if _, err := g.document.AddSignature(0, document.NormalizedRect{X: .1, Y: .1, Width: .6, Height: .6}, testSignature(t, 2, 1)); err != nil {
			t.Fatal(err)
		}
	}
	layer := g.imageSignatureLayer
	layer.reload()
	layer.widgets[0].TappedSecondary(&fyne.PointEvent{})
	item := g.objectOrderMenu.Items[0].(fyne.Tappable)
	replacement := testDocument(t, 100, 100)
	g.showDocument(replacement)
	revision := replacement.Revision()
	fyneTest.Tap(item)
	if replacement.Revision() != revision || len(replacement.Overlays()) != 0 {
		t.Fatal("obsolete menu changed the replacement document")
	}
}
