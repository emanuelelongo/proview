package document

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"emanuelelongo/proview/internal/pdfengine"
	"github.com/gen2brain/go-fitz"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func correctionRange(t *testing.T, d *Document, page int, word string) (int, int) {
	t.Helper()
	l, err := d.RenderingSnapshot().CorrectionTextGeometry(page)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, g := range l.Glyphs {
		b.WriteRune(g.Rune)
	}
	index := strings.Index(b.String(), word)
	if index < 0 {
		t.Fatalf("missing %q in %q", word, b.String())
	}
	return len([]rune(b.String()[:index])), len([]rune(word))
}
func correctedPageText(t *testing.T, d *Document, page int) string {
	t.Helper()
	data, err := d.Export(ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := fitz.NewFromMemory(data)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	value, err := r.Text(page)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func TestTextCorrectionHistoryDuplicationAndSnapshots(t *testing.T) {
	d := redactionDocument(t)
	if err := d.DuplicatePage(0); err != nil {
		t.Fatal(err)
	}
	d.MarkSaved()
	ids := d.PageIDs()
	original := d.Original()
	before := d.RenderingSnapshot()
	start, n := correctionRange(t, d, 0, "SECRET")
	prepared, err := d.RenderingSnapshot().PrepareTextCorrection(0, start, n, "EDIT", 12, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if d.Dirty() {
		t.Fatal("preparation modified document")
	}
	if err := d.ApplyTextCorrection(prepared); err != nil {
		t.Fatal(err)
	}
	if !d.Dirty() || !bytes.Equal(original, d.Original()) {
		t.Fatal("correction changed original bytes or lacks dirty state")
	}
	if got := d.PageIDs(); len(got) != 2 || got[0] != ids[0] || got[1] != ids[1] {
		t.Fatal("page identity changed")
	}
	if text := correctedPageText(t, d, 0); strings.Contains(text, "SECRET") || !strings.Contains(text, "EDIT") {
		t.Fatal(text)
	}
	if !strings.Contains(correctedPageText(t, d, 1), "SECRET") {
		t.Fatal("duplicate page changed")
	}
	if !strings.Contains(correctedPageText(t, before, 0), "SECRET") {
		t.Fatal("snapshot followed correction")
	}
	changed := d.RenderingSnapshot()
	if !d.Undo() || d.Dirty() || !strings.Contains(correctedPageText(t, d, 0), "SECRET") {
		t.Fatal("one undo did not restore state")
	}
	if !strings.Contains(correctedPageText(t, changed, 0), "EDIT") {
		t.Fatal("corrected snapshot followed undo")
	}
	if !d.Redo() {
		t.Fatal("redo failed")
	}
	start, n = correctionRange(t, d, 0, "EDIT")
	second, err := d.RenderingSnapshot().PrepareTextCorrection(0, start, n, "AGAIN", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := d.ApplyTextCorrection(second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(correctedPageText(t, d, 0), "AGAIN") {
		t.Fatal("second correction failed")
	}
}

func TestTextCorrectionPreservesPendingFormsAndOverlays(t *testing.T) {
	c := context.Background()
	ctx, err := api.ReadValidateAndOptimize(c, bytes.NewReader(textPDF(t, "PUBLIC SECRET KEEP")), model.NewDefaultConfiguration(), nil)
	if err != nil {
		t.Fatal(err)
	}
	page, ref, _, err := ctx.PageDict(c, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	font, err := ctx.IndRefForNewObject(types.Dict{"Type": types.Name("Font"), "Subtype": types.Name("Type1"), "BaseFont": types.Name("Helvetica")})
	if err != nil {
		t.Fatal(err)
	}
	field, err := ctx.IndRefForNewObject(types.Dict{"Type": types.Name("Annot"), "Subtype": types.Name("Widget"), "FT": types.Name("Tx"), "T": types.StringLiteral("name"), "V": types.StringLiteral("Initial"), "Rect": types.Array{types.Integer(20), types.Integer(20), types.Integer(160), types.Integer(45)}, "P": *ref, "F": types.Integer(4)})
	if err != nil {
		t.Fatal(err)
	}
	page["Annots"] = types.Array{*field}
	catalog, err := ctx.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	catalog["AcroForm"] = types.Dict{"Fields": types.Array{*field}, "DA": types.StringLiteral("/Helv 12 Tf 0 g"), "DR": types.Dict{"Font": types.Dict{"Helv": *font}}}
	var b bytes.Buffer
	if err := api.WriteContext(c, ctx, &b); err != nil {
		t.Fatal(err)
	}
	s, err := Load("form-text.pdf", bytes.NewReader(b.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(s)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	fields, err := d.FormFields(0)
	if err != nil || len(fields) != 1 {
		t.Fatalf("fields: %v %v", fields, err)
	}
	if err := d.SetFormValue(0, fields[0].ID, "Pending value"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.AddSignature(0, NormalizedRect{X: .7, Y: .7, Width: .1, Height: .1}, opaqueSignaturePNG(t)); err != nil {
		t.Fatal(err)
	}
	start, n := correctionRange(t, d, 0, "SECRET")
	prepared, err := d.RenderingSnapshot().PrepareTextCorrection(0, start, n, "EDIT", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := d.ApplyTextCorrection(prepared); err != nil {
		t.Fatal(err)
	}
	fields, err = d.FormFields(0)
	if err != nil || len(fields) != 1 || fields[0].Value != "Pending value" || len(d.Overlays()) != 1 {
		t.Fatalf("lost form/overlay: %v %v", fields, err)
	}
	if err := d.SetFormValue(0, fields[0].ID, "Edited again"); err != nil {
		t.Fatal(err)
	}
	data, err := d.Export(ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := pdfengine.ReadForms(data)
	if err != nil || len(reopened) != 1 || reopened[0].Value != "Edited again" {
		t.Fatalf("saved fields: %v %v", reopened, err)
	}
	if !d.Undo() || !d.Undo() {
		t.Fatal("undo form/correction failed")
	}
	fields, _ = d.FormFields(0)
	if fields[0].Value != "Pending value" {
		t.Fatal("undo lost pending form edits")
	}
}

func TestTextCorrectionRejectsStalePreparationAndRedactions(t *testing.T) {
	d := redactionDocument(t)
	start, n := correctionRange(t, d, 0, "SECRET")
	p, err := d.RenderingSnapshot().PrepareTextCorrection(0, start, n, "EDIT", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := d.DuplicatePage(0); err != nil {
		t.Fatal(err)
	}
	if err := d.ApplyTextCorrection(p); err == nil {
		t.Fatal("stale correction accepted")
	}
	if _, err := d.AddRedaction(0, selectedWord(t, d, "SECRET")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RenderingSnapshot().CorrectionTextGeometry(0); err == nil {
		t.Fatal("redacted content exposed for correction")
	}
}

func TestTextDuplicationHistoryAndIndependentSource(t *testing.T) {
	d := redactionDocument(t)
	original := bytes.Clone(d.Original())
	start, count := correctionRange(t, d, 0, "SECRET")
	prepared, err := d.RenderingSnapshot().PrepareTextDuplication(0, start, count, "EDIT", 0, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if d.Dirty() {
		t.Fatal("duplication preparation changed document")
	}
	if err := d.ApplyTextCorrection(prepared); err != nil {
		t.Fatal(err)
	}
	text := correctedPageText(t, d, 0)
	if strings.Count(text, "SECRET") != 1 || strings.Count(text, "EDIT") != 1 || !bytes.Equal(original, d.Original()) {
		t.Fatal("duplication changed original text or bytes")
	}
	if !d.Undo() || d.Dirty() || d.CanUndo() {
		t.Fatal("duplication is not one Undo step")
	}
	if !d.Redo() || !strings.Contains(correctedPageText(t, d, 0), "EDIT") {
		t.Fatal("duplicate lost source after Redo")
	}
	d.MarkSaved()
	start, count = correctionRange(t, d, 0, "SECRET")
	failed, err := d.RenderingSnapshot().PrepareTextDuplication(0, start, count, "SECRET", 0, 0)
	if err == nil || failed != nil || d.Dirty() {
		t.Fatal("overlapping duplicate changed document")
	}
}
