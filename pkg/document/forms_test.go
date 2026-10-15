package document

import (
	"bytes"
	"context"
	"os"
	"slices"
	"testing"

	"emanuelelongo/proview/internal/pdfengine"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func formDocument(t *testing.T) *Document {
	t.Helper()
	data, err := os.ReadFile("../../internal/pdfengine/testdata/forms.pdf")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Load("forms.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d
}
func TestFormHistorySnapshotAndEditableExport(t *testing.T) {
	d := formDocument(t)
	fields, err := d.FormFields(0)
	if err != nil {
		t.Fatal(err)
	}
	if !d.HasFormFields() || len(fields) != 3 || d.Dirty() {
		t.Fatalf("fields=%+v dirty=%v", fields, d.Dirty())
	}
	initial := d.RenderingSnapshot()
	if err = d.SetFormValue(0, fields[0].ID, "Caffè"); err != nil {
		t.Fatal(err)
	}
	changed := d.RenderingSnapshot()
	if !d.Dirty() || !d.CanUndo() {
		t.Fatal("field edit not tracked")
	}
	if !d.Undo() || d.Dirty() {
		t.Fatal("undo failed")
	}
	if f, _ := initial.FormFields(0); f[0].Value != "Initial" {
		t.Fatal("snapshot mutated")
	}
	if f, _ := changed.FormFields(0); f[0].Value != "Caffè" {
		t.Fatal("edit snapshot lost value")
	}
	if !d.Redo() {
		t.Fatal("redo failed")
	}
	output, err := d.Export(ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := pdfengine.ReadForms(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened) != 3 || reopened[0].Value != "Caffè" {
		t.Fatalf("exported fields=%+v", reopened)
	}
	d.MarkSaved()
	if d.Dirty() {
		t.Fatal("mark saved failed")
	}
	if err = d.SetFormValue(0, fields[0].ID, "Edited again"); err != nil {
		t.Fatal(err)
	}
	if !d.Dirty() {
		t.Fatal("second edit not tracked")
	}
}
func TestFormsSurvivePageRotationAndOverlay(t *testing.T) {
	d := formDocument(t)
	fields, _ := d.FormFields(0)
	if err := d.SetFormValue(0, fields[0].ID, "Filled"); err != nil {
		t.Fatal(err)
	}
	if err := d.TransformPDFPage(0, "right"); err != nil {
		t.Fatal(err)
	}
	rotated, err := d.FormFields(0)
	if err != nil {
		t.Fatal(err)
	}
	if rotated[0].Rect != transformRect(fields[0].Rect, "right") {
		t.Fatal("field not rotated with page")
	}
	if _, err = d.AddSignature(0, NormalizedRect{X: .7, Y: .7, Width: .1, Height: .1}, opaqueSignaturePNG(t)); err != nil {
		t.Fatal(err)
	}
	output, err := d.Export(ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := pdfengine.ReadForms(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened) != 3 || reopened[0].Value != "Filled" {
		t.Fatalf("fields lost after page composition: %+v", reopened)
	}
}

func extendedFormDocument(t *testing.T) *Document {
	t.Helper()
	data, err := os.ReadFile("../../internal/pdfengine/testdata/forms-extended.pdf")
	if err != nil {
		t.Fatal(err)
	}
	source, err := Load("extended.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(doc.Close)
	return doc
}
func namedFormField(t *testing.T, d *Document, page int, name string) FormField {
	t.Helper()
	fields, err := d.FormFields(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("missing field %q", name)
	return FormField{}
}
func TestMultiValuesHistoryAndSnapshotIsolation(t *testing.T) {
	d := extendedFormDocument(t)
	field := namedFormField(t, d, 0, "colors")
	values := []string{"b", "g", "g"}
	if err := d.SetFormValues(0, field.ID, values); err != nil {
		t.Fatal(err)
	}
	values[0] = "r"
	snapshot := d.RenderingSnapshot()
	linked := namedFormField(t, d, 1, "colors")
	if !slices.Equal(linked.Values, []string{"g", "b"}) {
		t.Fatalf("linked values: %v", linked.Values)
	}
	linked.Values[0] = "r"
	if got := namedFormField(t, d, 0, "colors").Values; !slices.Equal(got, []string{"g", "b"}) {
		t.Fatal("caller mutated editor values")
	}
	if !d.Undo() || !slices.Equal(namedFormField(t, d, 0, "colors").Values, []string{"r", "b"}) {
		t.Fatal("undo lost original choices")
	}
	if !slices.Equal(namedFormField(t, snapshot, 0, "colors").Values, []string{"g", "b"}) {
		t.Fatal("undo mutated snapshot")
	}
	if !d.Redo() {
		t.Fatal("redo failed")
	}
	output, err := d.Export(ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	source, err := Load("saved.pdf", bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !slices.Equal(namedFormField(t, reopened, 1, "colors").Values, []string{"g", "b"}) {
		t.Fatal("saved multi values lost")
	}
	if err := d.SetFormValues(0, field.ID, nil); err != nil {
		t.Fatal(err)
	}
	if len(namedFormField(t, d, 1, "colors").Values) != 0 {
		t.Fatal("cannot clear multi field")
	}
}
func TestResetFormScopesAndDefaults(t *testing.T) {
	for _, test := range []struct {
		name, password, nameValue, note string
		colors                          []string
	}{
		{"reset_all", "default", "", "Default", []string{"g"}},
		{"reset_subset", "default", "", "Default", []string{"r", "b"}},
		{"reset_exclude", "default", "Existing", "Edited note", []string{"g"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := extendedFormDocument(t)
			button := namedFormField(t, d, 0, test.name)
			before := d.RenderingSnapshot()
			if err := d.ResetForm(0, button.ID); err != nil {
				t.Fatal(err)
			}
			if namedFormField(t, d, 0, "password").Value != test.password || namedFormField(t, d, 0, "profile.name").Value != test.nameValue || namedFormField(t, d, 0, "profile.note").Value != test.note || !slices.Equal(namedFormField(t, d, 0, "colors").Values, test.colors) {
				t.Fatal("reset affected wrong fields/defaults")
			}
			if !d.Undo() || d.CanUndo() || d.Dirty() {
				t.Fatal("reset is not exactly one undo step")
			}
			if !slices.Equal(namedFormField(t, d, 0, "colors").Values, namedFormField(t, before, 0, "colors").Values) {
				t.Fatal("reset undo changed source values")
			}
			if !d.Redo() {
				t.Fatal("reset redo failed")
			}
			output, err := d.Export(ExportOptions{})
			if err != nil {
				t.Fatal(err)
			}
			ctx, err := api.ReadContext(context.Background(), bytes.NewReader(output), nil)
			if err != nil {
				t.Fatal(err)
			}
			for id, want := range map[int]string{17: "Locked", 20: "Hidden"} {
				dict := ctx.Table[id].Object.(types.Dict)
				value, err := ctx.DereferenceStringOrHexLiteral(dict["V"], 0, nil)
				if err != nil {
					t.Fatal(err)
				}
				if value != want {
					t.Fatalf("protected field %d reset to %q", id, value)
				}
			}
		})
	}
}
func TestFormDeclarativeValidationAllowsDrafts(t *testing.T) {
	d := extendedFormDocument(t)
	name := namedFormField(t, d, 0, "profile.name")
	for _, value := range []string{"too many characters", "one\ntwo", "nul\x00byte"} {
		if err := d.SetFormValue(0, name.ID, value); err == nil {
			t.Fatalf("accepted invalid text %q", value)
		}
	}
	if err := d.SetFormValues(0, name.ID, []string{"one", "two"}); err == nil {
		t.Fatal("accepted array for text")
	}
	colors := namedFormField(t, d, 0, "colors")
	if err := d.SetFormValues(0, colors.ID, []string{"unknown"}); err == nil {
		t.Fatal("accepted unknown choice")
	}
	if err := d.SetFormValue(0, name.ID, ""); err != nil {
		t.Fatal("cannot leave a required field incomplete")
	}
	if got := d.FormValidationIssues(); len(got) != 2 {
		t.Fatalf("required issues=%v, want name and agreement", got)
	}
	if _, err := d.Export(ExportOptions{}); err != nil {
		t.Fatalf("draft cannot be saved: %v", err)
	}
}
