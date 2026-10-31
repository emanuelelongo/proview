package gui

import (
	"path/filepath"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/storage"
)

func TestDocumentThumbnailDragPreservesStateAndSessionOrder(t *testing.T) {
	g, _ := newDropTestGui(t)
	directory := t.TempDir()
	for _, name := range []string{"first.png", "second.png", "third.png"} {
		g.addDocument(testDocument(t, 40, 30), nil, storage.NewFileURI(filepath.Join(directory, name)))
	}
	g.window.Resize(fyne.NewSize(800, 600))
	g.window.Show()
	original := slices.Clone(g.documents)
	active, doc, target := g.activeDocument, g.document, g.saveTarget
	revision := doc.Revision()
	g.singleDocumentSelection(original[0])
	g.refreshDocumentSidebar()
	first, last := g.documentThumbs[0], g.documentThumbs[2]
	tap := first.card.Objects[2].(*thumbnailTap)
	origin := g.app.Driver().AbsolutePositionForObject(g.sidebar.documents)
	destination := origin.Add(fyne.NewPos(20, last.card.Position().Y+last.card.Size().Height-2))
	tap.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	tap.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: destination}})
	if g.documentDrag == nil || !last.marker.Visible() || first.preview.Translucency != .5 {
		t.Fatal("drag feedback missing")
	}
	tap.MouseUp(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary, PointEvent: fyne.PointEvent{AbsolutePosition: destination}})
	tap.DragEnd()
	expected := []*editorDocument{original[1], original[2], original[0]}
	if !slices.Equal(g.documents, expected) {
		t.Fatal("file order did not change")
	}
	if g.activeDocument != active || g.document != doc || g.saveTarget != target || doc.Revision() != revision || !g.documentSelection[original[0]] {
		t.Fatal("organizational move changed document state")
	}
	if first.preview.Translucency != 0 || g.documentDrag != nil {
		t.Fatal("drag feedback was not cleared")
	}
	g.saveSession()
	paths := g.launchPaths(nil)
	want := []string{filepath.Join(directory, "second.png"), filepath.Join(directory, "third.png"), filepath.Join(directory, "first.png")}
	if !slices.Equal(paths, want) {
		t.Fatalf("session paths = %v, want %v", paths, want)
	}
}

func TestDocumentSidebarRetainsWidgetsAcrossDocumentEdits(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.addDocument(testDocument(t, 40, 30), nil, nil)
	g.addDocument(testDocument(t, 40, 30), nil, nil)
	entry := g.documents[0]
	before := g.documentThumbs[0]
	tap := before.card.Objects[2]
	entry.doc.MarkUnsaved()
	g.refreshDocumentSidebar()
	after := g.documentThumbs[0]
	if before.card != after.card || before.preview != after.preview || before.marker != after.marker {
		t.Fatal("document edit recreated its persistent sidebar card")
	}
	if after.card.Objects[2] != tap {
		t.Fatal("document edit replaced the card's input widget")
	}
	if button, ok := after.card.Objects[0].(*canvas.Image); ok {
		if button.Image != entry.thumbnail {
			t.Fatal("sidebar preview does not use the retained document thumbnail")
		}
	} else {
		t.Fatalf("preview card starts with %T", after.card.Objects[0])
	}
}

func TestDocumentThumbnailDragGroupsAndCancellation(t *testing.T) {
	g, _ := newDropTestGui(t)
	for range 4 {
		g.addDocument(testDocument(t, 40, 30), nil, nil)
	}
	g.window.Resize(fyne.NewSize(800, 600))
	g.window.Show()
	original := slices.Clone(g.documents)
	g.documentSelection = map[*editorDocument]bool{original[0]: true, original[2]: true}
	g.refreshDocumentSidebar()
	origin := g.app.Driver().AbsolutePositionForObject(g.sidebar.documents)
	g.dragDocument(original[0], origin.Add(fyne.NewPos(20, 20)))
	if !g.cancelThumbnailDrag() || g.documentDrag != nil || !slices.Equal(g.documents, original) {
		t.Fatal("Escape did not cancel organizational drag")
	}
	g.dragDocument(original[0], origin.Subtract(fyne.NewPos(20, 20)))
	g.finishDocumentDrag()
	if !slices.Equal(g.documents, original) {
		t.Fatal("outside drop reordered files")
	}
	g.dragDocument(original[0], origin.Add(fyne.NewPos(20, 20)))
	// Move the selected group to the final insertion gap.
	g.documentDrag.insertion = len(g.documents)
	g.finishDocumentDrag()
	if !slices.Equal(g.documents, []*editorDocument{original[1], original[3], original[0], original[2]}) {
		t.Fatal("selected documents did not retain their relative order")
	}
	g.dragDocument(original[0], origin.Add(fyne.NewPos(20, 20)))
	g.refreshDocumentSidebar()
	if g.documentDrag != nil {
		t.Fatal("sidebar rebuild retained an obsolete gesture")
	}
}
