package gui

import (
	"bytes"
	"os"
	"testing"

	"emanuelelongo/proview/pkg/document"
)

func TestBasePreviewsRenderOnlyChangedPagesAndCoalesceFieldEdits(t *testing.T) {
	g, driver := newDropTestGui(t)
	raw, err := os.ReadFile("../../internal/pdfengine/testdata/forms-extended.pdf")
	if err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("forms.pdf", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	g.addDocument(doc, nil, nil)
	g.zoom = .5
	g.applyZoom()
	g.loadPDFPage(1)
	for !g.pdfPageLoaded[1] {
		driver.runNext(t)
	}
	first, second := g.pdfPageImages[0].Image, g.pdfPageImages[1].Image
	fields, err := doc.FormFields(0)
	if err != nil {
		t.Fatal(err)
	}
	var name, colors int
	for _, field := range fields {
		if field.Name == "profile.name" {
			name = field.ID
		}
		if field.Name == "colors" {
			colors = field.ID
		}
	}
	if name == 0 || colors == 0 {
		t.Fatal("fixture fields missing")
	}
	if err := doc.SetFormValue(0, name, "First value"); err != nil {
		t.Fatal(err)
	}
	g.refreshFormPreviews()
	state := g.pdfBasePreviews
	if state == nil || state.timer == nil {
		t.Fatal("field change did not schedule a coalesced preview")
	}
	state.timer.Stop()
	state.timer = nil
	g.renderPDFBasePreviews(state)
	// A newer value must survive an older native render completing late.
	if err := doc.SetFormValue(0, name, "Final value"); err != nil {
		t.Fatal(err)
	}
	g.refreshFormPreviews()
	for g.pdfBaseKeys[0] != doc.BasePageKey(0) {
		driver.runNext(t)
	}
	if g.pdfPageImages[0].Image == first || g.pdfPageImages[1].Image != second {
		t.Fatal("field preview rerendered an unrelated page or lost latest value")
	}
	if err := doc.SetFormValues(0, colors, []string{"g", "b"}); err != nil {
		t.Fatal(err)
	}
	g.refreshFormPreviews()
	for g.pdfBaseKeys[0] != doc.BasePageKey(0) || g.pdfBaseKeys[1] != doc.BasePageKey(1) {
		driver.runNext(t)
	}
	if g.pdfPageImages[1].Image == second {
		t.Fatal("linked field did not refresh its other page")
	}
	if !doc.Undo() {
		t.Fatal("undo failed")
	}
	g.refreshFormPreviews()
	for g.pdfBaseKeys[1] != doc.BasePageKey(1) {
		driver.runNext(t)
	}
	g.closeWindow()
}
