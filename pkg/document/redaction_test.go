package document

import (
	"bytes"
	"emanuelelongo/proview/pkg/annotations"
	"github.com/gen2brain/go-fitz"
	"strings"
	"testing"
)

func redactionDocument(t *testing.T) *Document {
	t.Helper()
	source, err := Load("redact.pdf", bytes.NewReader(textPDF(t, "PUBLIC SECRET KEEP")))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { doc.Close() })
	return doc
}
func selectedWord(t *testing.T, doc *Document, word string) []TextQuad {
	t.Helper()
	layout, err := doc.TextGeometry(0)
	if err != nil {
		t.Fatal(err)
	}
	runes := make([]rune, len(layout.Glyphs))
	for i, g := range layout.Glyphs {
		runes[i] = g.Rune
	}
	start := strings.Index(string(runes), word)
	if start < 0 {
		t.Fatalf("missing word %s in %s", word, string(runes))
	}
	return layout.Selection(start, start+len(word)-1)
}

// DuplicatePage gives the copy its own redaction ID and page ID while reusing
// the geometry, so rotating one page must not move the other's marks. Both the
// duplicate and the history entry share the quad slice until a transform
// replaces it.
func TestDuplicatePageKeepsRedactionGeometryIndependent(t *testing.T) {
	doc := redactionDocument(t)
	if _, err := doc.AddRedaction(0, selectedWord(t, doc, "SECRET")); err != nil {
		t.Fatal(err)
	}
	source := doc.Redactions()[0]
	if err := doc.DuplicatePage(0); err != nil {
		t.Fatal(err)
	}
	marks := doc.Redactions()
	if len(marks) != 2 {
		t.Fatalf("redactions = %d, want 2", len(marks))
	}
	copied := marks[1].Quads[0]
	if marks[1].ID == source.ID {
		t.Fatal("duplicate redaction reused the source ID")
	}
	if marks[1].PageID == source.PageID {
		t.Fatal("duplicate redaction kept the source page ID")
	}
	if copied != source.Quads[0] {
		t.Fatal("duplicate redaction changed geometry")
	}

	if err := doc.TransformPDFPage(0, "right"); err != nil {
		t.Fatal(err)
	}
	marks = doc.Redactions()
	if marks[1].Quads[0] != copied {
		t.Fatal("rotating the source page moved the duplicate redaction")
	}
	if marks[0].Quads[0] == copied {
		t.Fatal("rotating the source page did not rotate its own redaction")
	}
	if !doc.Undo() {
		t.Fatal("rotation was not undoable")
	}
	marks = doc.Redactions()
	if marks[0].Quads[0] != copied || marks[1].Quads[0] != copied {
		t.Fatal("undo did not restore both redactions")
	}
	for doc.CanUndo() {
		if !doc.Undo() {
			t.Fatal("undo failed")
		}
	}
	if len(doc.Redactions()) != 0 {
		t.Fatalf("redactions after undo = %d, want 0", len(doc.Redactions()))
	}
	if doc.Dirty() {
		t.Fatal("undoing every edit must leave the document clean")
	}
}

func TestRedactionHistorySnapshotsAndExport(t *testing.T) {
	doc := redactionDocument(t)
	quads := selectedWord(t, doc, "SECRET")
	if len(quads) != 1 {
		t.Fatalf("quads = %d", len(quads))
	}
	mark, err := doc.AddRedaction(0, quads)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := doc.RenderingSnapshot()
	quads[0].UL.X = 0
	if doc.Redactions()[0].Quads[0].UL.X == 0 {
		t.Fatal("shared geometry")
	}
	// Export also removes content from overlays added after the mark.
	data := annotations.Default(annotations.Text)
	data.Text = "OVERLAY_SECRET"
	x0, y0, x1, y1 := mark.Quads[0].Bounds()
	_, err = doc.AddAnnotation(0, NormalizedRect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, data, prepareAnnotation(t, data))
	if err != nil {
		t.Fatal(err)
	}
	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := fitz.NewFromMemory(output)
	if err != nil {
		t.Fatal(err)
	}
	defer renderer.Close()
	text, err := renderer.Text(0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "SECRET") || !strings.Contains(text, "PUBLIC") || !strings.Contains(text, "KEEP") {
		t.Fatalf("exported text: %q", text)
	}
	doc.MarkSaved()
	if !doc.Undo() || !doc.Undo() || doc.HasRedactions() || !doc.Dirty() {
		t.Fatal("save discarded history")
	}
	if !snapshot.HasRedactions() {
		t.Fatal("snapshot followed Undo")
	}
	if !doc.Redo() || !doc.HasRedactions() {
		t.Fatal("mark not restored")
	}
	if err := doc.RemoveRedaction(mark.ID); err != nil {
		t.Fatal(err)
	}
	if !doc.Undo() || len(doc.Redactions()) != 1 {
		t.Fatal("delete Undo failed")
	}
}
func TestRedactionsFollowPageIdentityAndTransfers(t *testing.T) {
	doc := redactionDocument(t)
	other := redactionDocument(t)
	quads := selectedWord(t, other, "SECRET")
	if _, err := other.AddRedaction(0, quads); err != nil {
		t.Fatal(err)
	}
	if err := doc.AppendDocumentPages(other); err != nil {
		t.Fatal(err)
	}
	if len(doc.RedactionsForPage(1)) != 1 || len(doc.RedactionsForPage(0)) != 0 {
		t.Fatal("transferred mark lost page identity")
	}
	keys := doc.RedactionKeys()
	if err := doc.SetPageOrder([]int{1, 0}); err != nil {
		t.Fatal(err)
	}
	if len(doc.RedactionsForPage(0)) != 1 || doc.RedactionKeys()[0] != keys[0] {
		t.Fatal("reorder lost mark")
	}
	if err := doc.SetPageOrder([]int{1}); err != nil {
		t.Fatal(err)
	}
	if doc.HasRedactions() || !doc.Undo() || len(doc.RedactionsForPage(0)) != 1 {
		t.Fatal("page deletion Undo lost mark")
	}
}
