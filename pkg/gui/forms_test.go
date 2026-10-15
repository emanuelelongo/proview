package gui

import (
	"bytes"
	"image/color"
	"os"
	"slices"
	"testing"

	"emanuelelongo/proview/internal/pdfengine"
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestFormTypographyTracksPageZoomWithoutReplacingFocusedEntry(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	data, err := os.ReadFile("../../internal/pdfengine/testdata/forms.pdf")
	if err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("forms.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	g.document = doc
	g.formsActive = true
	layer := newFormLayer(g, 0)
	g.window.SetContent(layer)
	pageSize, _ := doc.PageSize(0)
	layer.Resize(fyne.NewSize(float32(pageSize.X), float32(pageSize.Y)))
	entry := layer.controls[0].(*widget.Entry)
	g.window.Canvas().Focus(entry)
	entry.SetText("Zoom test")
	base := theme.CurrentForWidget(entry).Size(theme.SizeNameText)
	for _, factor := range []float32{2, .5, 1} {
		layer.Resize(fyne.NewSize(float32(pageSize.X)*factor, float32(pageSize.Y)*factor))
		if got := theme.CurrentForWidget(entry).Size(theme.SizeNameText); got != base*factor {
			t.Fatalf("text size=%g, want %g", got, base*factor)
		}
		if layer.controls[0] != entry || g.window.Canvas().Focused() != entry || entry.Text != "Zoom test" {
			t.Fatal("zoom replaced or unfocused the entry")
		}
	}
}

func TestFormToolbarInlineEditingAndSave(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	if !g.formAction.button.Disabled() {
		t.Fatal("form button enabled without document")
	}
	sourceWithoutFields, err := document.Load("plain.pdf", bytes.NewReader(scrollTestPDF(1)))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := document.New(sourceWithoutFields)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	g.document = plain
	g.updateHistoryActions()
	if !g.formAction.button.Disabled() {
		t.Fatal("form button enabled for PDF without fields")
	}
	g.showDocument(testDocument(t, 100, 100))
	if !g.formAction.button.Disabled() {
		t.Fatal("form button enabled for image")
	}
	data, err := os.ReadFile("../../internal/pdfengine/testdata/forms.pdf")
	if err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("forms.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	// Use a single layer to exercise editing without asynchronous page loading.
	g.document = doc
	layer := newFormLayer(g, 0)
	g.formLayers = map[int]*formLayer{0: layer}
	g.window.SetContent(container.NewStack(layer))
	layer.Resize(fyne.NewSize(600, 400))
	g.updateHistoryActions()
	if g.formAction.button.Disabled() || layer.Visible() {
		t.Fatal("incorrect initial form state")
	}
	fyneTest.Tap(g.formAction.button)
	if !g.formsActive || !layer.Visible() {
		t.Fatal("toolbar did not activate fields")
	}
	entry := layer.controls[0].(*widget.Entry)
	fyneTest.Type(entry, "Mario Rossi")
	if fields, _ := doc.FormFields(0); fields[0].Value != entry.Text || !doc.Dirty() {
		t.Fatal("typing did not update the document")
	}
	choice := layer.controls[2].(*widget.Select)
	if choice.Selected != "Alpha" {
		t.Fatalf("choice label=%q", choice.Selected)
	}
	choice.SetSelected("Beta")
	if err := doc.SetFormValue(0, layer.fields[2].ID, ""); err != nil {
		t.Fatal(err)
	}
	g.syncFormValues()
	if choice.Selected != "" {
		t.Fatal("clearing a choice retained its old display label")
	}
	choice.SetSelected("Beta")
	fyneTest.Tap(layer.controls[1].(*formToggle))
	output, err := doc.Export(document.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	fields, err := pdfengine.ReadForms(output)
	if err != nil {
		t.Fatal(err)
	}
	if fields[0].Value != entry.Text || fields[1].Value != "Yes" || fields[2].Value != "b" {
		t.Fatalf("saved fields: %+v", fields)
	}
	if !g.cancelCurrentOperation() || g.formsActive || layer.Visible() {
		t.Fatal("Escape did not deactivate form editing")
	}
	g.toggleForms()
	g.setAnnotationTool(toolDraw)
	if g.formsActive {
		t.Fatal("drawing left form editing active")
	}
}

// The PDF preview below a control still contains the original field appearance.
// Its pixels must never show through idle, hovered or focused form controls.
func TestFormControlHidesPDFValueInEveryInteractionState(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	data, err := os.ReadFile("../../internal/pdfengine/testdata/forms.pdf")
	if err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("forms.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	g.document = doc
	g.formsActive = true
	layer := newFormLayer(g, 0)
	g.formLayers = map[int]*formLayer{0: layer}
	background := canvas.NewRectangle(color.White)
	g.window.SetContent(container.NewStack(background, layer))
	g.window.Resize(fyne.NewSize(600, 400))
	choice := layer.controls[2].(*widget.Select)
	choice.SetSelected("Beta")
	sample := func() color.NRGBA {
		position := choice.Position()
		img := g.window.Canvas().Capture()
		x, y := int(position.X+20), int(position.Y+choice.Size().Height-4)
		return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	}
	for _, state := range []string{"idle", "hover", "focus"} {
		t.Run(state, func(t *testing.T) {
			switch state {
			case "hover":
				choice.MouseIn(&desktop.MouseEvent{})
			case "focus":
				choice.FocusGained()
			}
			background.FillColor = color.NRGBA{R: 255, A: 255}
			background.Refresh()
			red := sample()
			background.FillColor = color.NRGBA{B: 255, A: 255}
			background.Refresh()
			blue := sample()
			if red != blue {
				t.Fatalf("PDF appearance visible through %s control: over red=%v, over blue=%v", state, red, blue)
			}
			choice.MouseOut()
			choice.FocusLost()
			fields, err := doc.FormFields(0)
			if err != nil {
				t.Fatal(err)
			}
			if choice.Selected != "Beta" || fields[2].Value != "b" {
				t.Fatalf("%s changed selection: %q / %+v", state, choice.Selected, fields[2])
			}
		})
	}
}

func TestPDFSingleSelectionListUsesRowsAndPreservesHistory(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	data, err := os.ReadFile("../../internal/pdfengine/testdata/forms.pdf")
	if err != nil {
		t.Fatal(err)
	}
	// Keep the fixture's offsets intact while changing its combo into a list.
	data = bytes.ReplaceAll(data, []byte("/Ff 131072"), []byte("/Ff 000000"))
	source, err := document.Load("list.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	g.document = doc
	g.formsActive = true
	layer := newFormLayer(g, 0)
	g.formLayers = map[int]*formLayer{0: layer}
	g.window.SetContent(container.NewStack(layer))
	g.window.Resize(fyne.NewSize(600, 800))
	list, ok := layer.controls[2].(*widget.List)
	if !ok {
		t.Fatalf("PDF list uses %T, want rows", layer.controls[2])
	}
	fyneTest.TapCanvas(g.window.Canvas(), list.Position().Add(fyne.NewPos(20, 55)))
	fields, err := doc.FormFields(0)
	if err != nil {
		t.Fatal(err)
	}
	if fields[2].Value != "b" {
		t.Fatalf("clicking second row selected %q, want export value b", fields[2].Value)
	}
	revision := doc.Revision()
	// Hovering another row must not select it or add an undo entry.
	fyneTest.MoveMouse(g.window.Canvas(), list.Position().Add(fyne.NewPos(20, 12)))
	fields, _ = doc.FormFields(0)
	if fields[2].Value != "b" || doc.Revision() != revision {
		t.Fatal("hover changed list selection")
	}
	if !doc.Undo() {
		t.Fatal("cannot undo row selection")
	}
	g.syncFormValues()
	if doc.Revision() != revision+1 {
		t.Fatal("syncing undo recorded another edit")
	}
	output, err := doc.Export(document.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := pdfengine.ReadForms(output)
	if err != nil {
		t.Fatal(err)
	}
	if saved[2].Value != "a" {
		t.Fatalf("saved list value after undo: %q", saved[2].Value)
	}
	list.Select(1)
	fields, _ = doc.FormFields(0)
	if fields[2].Value != "b" {
		t.Fatal("cannot reselect row after undo")
	}
}

func TestExtendedFormEditingResetAndValidation(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	data, err := os.ReadFile("../../internal/pdfengine/testdata/forms-extended.pdf")
	if err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("extended.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	g.document = doc
	g.formsActive = true
	first, second := newFormLayer(g, 0), newFormLayer(g, 1)
	g.formLayers = map[int]*formLayer{0: first, 1: second}
	g.window.SetContent(container.NewStack(first))
	g.window.Resize(fyne.NewSize(800, 1000))
	control := func(layer *formLayer, name string) fyne.CanvasObject {
		t.Helper()
		for i, f := range layer.fields {
			if f.Name == name {
				return layer.controls[i]
			}
		}
		t.Fatalf("missing control %s", name)
		return nil
	}
	password := control(first, "password").(*widget.Entry)
	if !password.Password || password.Text != "secret" {
		t.Fatal("password input not masked or missing value")
	}
	password.SetText("changed")
	multi := control(first, "colors").(*formMultiList)
	linked := control(second, "colors").(*formMultiList)
	multi.Select(1)
	if !slices.Equal(multi.values, []string{"r", "g", "b"}) || !slices.Equal(linked.values, multi.values) {
		t.Fatalf("multi selection not synchronized: %v / %v", multi.values, linked.values)
	}
	revision := doc.Revision()
	multi.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	if doc.Revision() != revision {
		t.Fatal("arrow navigation changed selected values")
	}
	multi.TypedRune(' ')
	if !slices.Equal(multi.values, []string{"r", "g"}) {
		t.Fatalf("space did not toggle the focused choice: %v", multi.values)
	}
	beforeReset := doc.Revision()
	fyneTest.Tap(control(first, "reset_all").(*widget.Button))
	if doc.Revision() != beforeReset+1 || password.Text != "default" || !slices.Equal(multi.values, []string{"g"}) {
		t.Fatal("Reset did not restore all controls in one edit")
	}
	name := control(first, "profile.name").(*widget.Entry)
	if name.Text != "" || name.Validate() == nil {
		t.Fatal("required field not indicated after reset")
	}
	renderer := fyneTest.WidgetRenderer(first).(*formLayerRenderer)
	first.Refresh()
	for i, f := range first.fields {
		if f.Name == "profile.name" && renderer.validationBorders[i].StrokeWidth == 0 {
			t.Fatal("missing required-field border")
		}
	}
	output, err := doc.Export(document.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := pdfengine.ReadForms(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range saved {
		if f.Name == "colors" && !slices.Equal(f.Values, []string{"g"}) {
			t.Fatal("reset choices did not survive saving")
		}
	}
	if !doc.Undo() {
		t.Fatal("cannot undo reset")
	}
	g.syncFormValues()
	if password.Text != "changed" || !slices.Equal(multi.values, []string{"r", "g"}) {
		t.Fatal("undo reset did not restore controls")
	}
}

func TestFormPulseStopsOnEditingAndIgnoresObsoleteTicks(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	data, err := os.ReadFile("../../internal/pdfengine/testdata/forms.pdf")
	if err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("forms.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	g.setDocument(doc)
	if g.formPulse == nil {
		t.Fatal("opening fillable PDF did not start pulse")
	}
	animation := g.formPulse
	alpha := func() uint32 { _, _, _, a := g.formAction.pulseBackground.FillColor.RGBA(); return a }
	for _, progress := range []float32{.25, .75} {
		animation.Tick(progress)
		if alpha() == 0 {
			t.Fatal("pulse peak missing")
		}
	}
	animation.Tick(1)
	if alpha() != 0 {
		t.Fatal("pulse does not finish transparent")
	}
	g.toggleForms()
	if g.formPulse != nil || alpha() != 0 {
		t.Fatal("entering edit mode did not stop pulse")
	}
	animation.Tick(.25)
	if alpha() != 0 {
		t.Fatal("obsolete animation tick changed toolbar")
	}
	g.setDocument(testDocument(t, 100, 100))
	if g.formPulse != nil {
		t.Fatal("image started a form pulse")
	}
}
