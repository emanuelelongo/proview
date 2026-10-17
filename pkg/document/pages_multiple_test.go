package document

import (
	"bytes"
	"context"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestDuplicatePagesKeepsObjectsIndependentAndIsAtomic(t *testing.T) {
	doc := redactionDocument(t)
	for range 3 {
		if err := doc.DuplicatePage(0); err != nil {
			t.Fatal(err)
		}
	}
	for _, page := range []int{0, 2} {
		if _, err := doc.AddSignature(page, NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, opaqueSignaturePNG(t)); err != nil {
			t.Fatal(err)
		}
		q := TextQuad{UL: TextPoint{X: .1, Y: .1}, UR: TextPoint{X: .3, Y: .1}, LL: TextPoint{X: .1, Y: .2}, LR: TextPoint{X: .3, Y: .2}}
		if _, err := doc.AddRedaction(page, []TextQuad{q}); err != nil {
			t.Fatal(err)
		}
	}
	doc.MarkSaved()
	original := doc.PageIDs()
	snapshot := doc.RenderingSnapshot()
	revision := doc.Revision()
	for _, invalid := range [][]int{nil, {0, 0}, {-1}, {4}} {
		if doc.DuplicatePages(invalid) == nil || doc.Revision() != revision || doc.Dirty() {
			t.Fatal("invalid selection mutated the document", invalid)
		}
	}
	if err := doc.DuplicatePages([]int{2, 0}); err != nil {
		t.Fatal(err)
	}
	ids := doc.PageIDs()
	if len(ids) != 6 || !slices.Equal(ids[:3], original[:3]) || ids[5] != original[3] || !slices.Equal(snapshot.PageIDs(), original) {
		t.Fatal("copy group misplaced originals or changed a snapshot")
	}
	unique := map[uint64]bool{}
	for _, id := range ids {
		if unique[id] {
			t.Fatal("copied page reused an identity")
		}
		unique[id] = true
	}
	overlays := doc.Overlays()
	if len(overlays) != 4 || overlays[2].Page != 3 || overlays[3].Page != 4 || overlays[0].ID == overlays[2].ID || overlays[1].ID == overlays[3].ID {
		t.Fatal("copy group lost overlay identity or placement", overlays)
	}
	marks := doc.Redactions()
	if len(marks) != 4 || marks[2].PageID != ids[3] || marks[3].PageID != ids[4] || marks[2].ID == marks[0].ID || marks[3].ID == marks[1].ID {
		t.Fatal("copy group lost redactions")
	}
	if err := doc.TransformPDFPage(3, "right"); err != nil {
		t.Fatal(err)
	}
	if doc.Redactions()[0].Quads[0] != marks[0].Quads[0] {
		t.Fatal("transforming a copy changed its original")
	}
	if !doc.Undo() || !doc.Undo() || doc.Dirty() || !slices.Equal(doc.PageIDs(), original) || len(doc.Overlays()) != 2 || len(doc.Redactions()) != 2 {
		t.Fatal("one Undo did not restore the complete copy group")
	}
	if !doc.Redo() || !slices.Equal(doc.PageIDs(), ids) || len(doc.Overlays()) != 4 || len(doc.Redactions()) != 4 {
		t.Fatal("Redo did not restore the complete copy group")
	}
}

func TestDuplicatePageGroupExportsNativeContentInOrder(t *testing.T) {
	var sources []*Source
	for _, name := range []string{"Alpha", "Bravo", "Charlie", "Delta"} {
		source, err := Load(name+".pdf", bytes.NewReader(textPDF(t, name)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(source.Close)
		sources = append(sources, source)
	}
	doc, _ := New(sources[0])
	if err := doc.AppendPDFs(sources[1:]...); err != nil {
		t.Fatal(err)
	}
	if err := doc.DuplicatePages([]int{2, 0}); err != nil {
		t.Fatal(err)
	}
	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	contents := map[int]string{}
	err = api.ExtractContent(context.Background(), bytes.NewReader(output), nil, func(r io.Reader, page int) error {
		data, err := io.ReadAll(r)
		contents[page] += string(data)
		return err
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index, name := range []string{"Alpha", "Bravo", "Charlie", "Alpha", "Charlie", "Delta"} {
		if !strings.Contains(contents[index+1], name) || !strings.Contains(contents[index+1], "0 0 m 72 72 l S") {
			t.Fatal("copy export lost searchable text or vectors", index, contents[index+1])
		}
	}
}
