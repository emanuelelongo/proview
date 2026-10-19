package document

import (
	"testing"
	"time"
)

func TestSourceRevisionIgnoresOverlayHistoryAndInvalidatesSanitation(t *testing.T) {
	doc, id := previewLensDocument(t)
	sourceVersion := doc.SourceRevision()
	initialBase := doc.BasePageKey(0)
	quad := TextQuad{UL: TextPoint{X: .1, Y: .1}, UR: TextPoint{X: .2, Y: .1}, LL: TextPoint{X: .1, Y: .2}, LR: TextPoint{X: .2, Y: .2}}
	if _, err := doc.AddRedaction(0, []TextQuad{quad}); err != nil {
		t.Fatal(err)
	}
	key, version, revision := doc.TextSelectionKey(0), doc.SourceRevision(), doc.Revision()
	if version == sourceVersion || doc.BasePageKey(0) != initialBase {
		t.Fatal("redaction must invalidate recognition, but preserve source raster")
	}
	rect := doc.Overlays()[0].Rect
	rect.X += .1
	if err := doc.UpdateOverlay(id, 0, rect); err != nil {
		t.Fatal(err)
	}
	if doc.Revision() == revision || doc.SourceRevision() != version || doc.TextSelectionKey(0) != key {
		t.Fatal("overlay move invalidated source or sanitized recognition")
	}
	for _, step := range []func() bool{doc.Undo, doc.Redo} {
		if !step() || doc.SourceRevision() != version || doc.TextSelectionKey(0) != key {
			t.Fatal("overlay undo/redo invalidated source")
		}
	}
	snapshot := doc.RenderingSnapshot()
	if snapshot.SourceRevision() != version {
		t.Fatal("snapshot lost source identity")
	}
	if err := doc.TransformPDFPage(0, "right"); err != nil {
		t.Fatal(err)
	}
	if doc.SourceRevision() == version || doc.BasePageKey(0) == initialBase || doc.TextSelectionKey(0) == key {
		t.Fatal("rotation did not invalidate content")
	}
	if !doc.Undo() || doc.BasePageKey(0) != initialBase {
		t.Fatal("rotation undo did not restore source pixels")
	}
	if snapshot.TextSelectionKey(0) != key {
		t.Fatal("retained source snapshot changed")
	}
}

func TestBasePageKeysTrackOnlyAffectedFieldsIncludingLinkedWidgets(t *testing.T) {
	doc := extendedFormDocument(t)
	first, second := doc.BasePageKey(0), doc.BasePageKey(1)
	field := namedFormField(t, doc, 0, "profile.name")
	if err := doc.SetFormValue(0, field.ID, "A new name"); err != nil {
		t.Fatal(err)
	}
	if doc.BasePageKey(0) == first || doc.BasePageKey(1) != second {
		t.Fatal("unrelated page invalidated by a field edit")
	}
	if !doc.Undo() || doc.BasePageKey(0) != first {
		t.Fatal("field Undo did not restore base key")
	}
	linked := namedFormField(t, doc, 0, "colors")
	if err := doc.SetFormValues(0, linked.ID, []string{"g", "b"}); err != nil {
		t.Fatal(err)
	}
	if doc.BasePageKey(0) == first || doc.BasePageKey(1) == second {
		t.Fatal("linked widget did not invalidate both pages")
	}
}

func TestSourceMetadataDoesNotWaitForRasterWork(t *testing.T) {
	doc := extendedFormDocument(t)
	field := namedFormField(t, doc, 0, "profile.name")
	if err := doc.SetFormValue(0, field.ID, "Changed"); err != nil {
		t.Fatal(err)
	}
	source := doc.pages[0].source
	source.mu.Lock()
	defer source.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, err := doc.PageSize(0); _ = doc.BasePageKey(0); _ = doc.HasFormFields(); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("UI metadata queries waited for the native raster lock")
	}
}
