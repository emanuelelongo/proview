//go:build cgo && !nocgo && !extlib

package pdfengine

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFillFormsPreservesWidgetsAndAppearances(t *testing.T) {
	input, err := os.ReadFile("testdata/forms.pdf")
	if err != nil {
		t.Fatal(err)
	}
	fields, err := ReadForms(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 {
		t.Fatalf("editable fields = %d, want 3", len(fields))
	}
	if fields[0].Name != "name" || fields[0].MaxLen != 30 || fields[1].OnValue != "Yes" {
		t.Fatalf("fields: %+v", fields)
	}
	output, err := FillForms(input, map[int]string{fields[0].ID: "Caffè compilato", fields[1].ID: "Yes", fields[2].ID: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err = api.Validate(context.Background(), bytes.NewReader(output), nil, nil); err != nil {
		t.Fatal(err)
	}
	reopened, err := ReadForms(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened) != 3 || reopened[0].Value != "Caffè compilato" || reopened[1].Value != "Yes" || reopened[2].Value != "b" {
		t.Fatalf("reopened: %+v", reopened)
	}

	preview, err := RenderFormPage(output, 0, 72)
	if err != nil {
		t.Fatal(err)
	}
	if !hasFormInk(preview, image.Rect(20, 35, 180, 60)) {
		t.Fatal("text field appearance is blank")
	}
	output, err = FillForms(output, map[int]string{reopened[0].ID: "Edited again"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderFormPage(output, 0, 72)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(preview, second) {
		t.Fatal("second edit did not update rendered appearance")
	}
}

func TestPersistentFormRendererMatchesOneShotAcrossStridesAndDPI(t *testing.T) {
	input, err := os.ReadFile("testdata/forms.pdf")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewFormRenderer(input)
	if err != nil {
		t.Fatal(err)
	}
	defer renderer.Close()
	baseline, err := renderer.RenderPage(0, 72)
	if err != nil {
		t.Fatal(err)
	}
	for _, dpi := range []float64{72, 144, 216, 288} {
		got, err := renderer.RenderPage(0, dpi)
		if err != nil {
			t.Fatalf("persistent render at %g DPI: %v", dpi, err)
		}
		want, err := RenderFormPage(input, 0, dpi)
		if err != nil {
			t.Fatalf("one-shot render at %g DPI: %v", dpi, err)
		}
		if got.Bounds() != want.Bounds() {
			t.Fatalf("persistent render at %g DPI bounds = %v, want %v", dpi, got.Bounds(), want.Bounds())
		}
		for y := got.Bounds().Min.Y; y < got.Bounds().Max.Y; y++ {
			for x := got.Bounds().Min.X; x < got.Bounds().Max.X; x++ {
				if got.RGBAAt(x, y) != color.RGBAModel.Convert(want.At(x, y)).(color.RGBA) {
					t.Fatalf("persistent render at %g DPI differs at (%d,%d)", dpi, x, y)
				}
			}
		}
	}
	if !hasFormInk(baseline, image.Rect(20, 35, 180, 60)) {
		t.Fatal("persistent form renderer omitted widget appearance")
	}
	var workers sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 2 {
				if _, err := renderer.RenderPage(0, 72); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	renderer.Close()
	if _, err := renderer.RenderPage(0, 72); err == nil {
		t.Fatal("render after Close succeeded")
	}
}

func hasFormInk(img image.Image, rect image.Rectangle) bool {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a > 0 && r < 30000 && g < 30000 && b < 30000 {
				return true
			}
		}
	}
	return false
}

func TestListSelectionRemainsVisibleInPDFPreview(t *testing.T) {
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] /DA (/Helv 12 Tf 0 g) /DR << /Font << /Helv 4 0 R >> >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << >> /Annots [5 0 R] >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Annot /Subtype /Widget /FT /Ch /T (list) /Rect [20 60 180 140] /P 3 0 R /F 4 /Opt [[(a) (Alpha)] [(b) (Beta)] [(c) (Gamma)]] /V (a) /I [0] /TI 0 >>",
	}, "")
	fields, err := ReadForms(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"b", "c", "a"} {
		output, err := FillForms(input, map[int]string{fields[0].ID: value})
		if err != nil {
			t.Fatal(err)
		}
		if err = api.Validate(context.Background(), bytes.NewReader(output), nil, nil); err != nil {
			t.Fatal(err)
		}
		reopened, err := ReadForms(output)
		if err != nil {
			t.Fatal(err)
		}
		if reopened[0].Value != value || reopened[0].Kind != FormList {
			t.Fatalf("saved field: %+v", reopened[0])
		}
		preview, err := RenderFormPage(output, 0, 72)
		if err != nil {
			t.Fatal(err)
		}
		r, g, b, a := preview.At(170, 65).RGBA()
		if a == 0 || b <= r+5000 || g <= r+2000 {
			t.Fatalf("selected row is not highlighted: %v", preview.At(170, 65))
		}
		r, g, b, _ = preview.At(170, 88).RGBA()
		if b > r+5000 && g > r+2000 {
			t.Fatal("unselected row also highlighted")
		}
		label := map[string]string{"a": "Alpha", "b": "Beta", "c": "Gamma"}[value]
		if !strings.Contains(decodedObjects(t, output), "("+label+") Tj") {
			t.Fatalf("selected label %q missing from saved appearance", label)
		}
		if !hasFormInk(preview, image.Rect(20, 60, 110, 80)) {
			t.Fatal("highlight obscures the selected label")
		}
		input = output
		fields = reopened
	}
}

func TestMultiSelectionPasswordAndResetMetadata(t *testing.T) {
	input, err := os.ReadFile("testdata/forms-extended.pdf")
	if err != nil {
		t.Fatal(err)
	}
	fields, err := ReadForms(input)
	if err != nil {
		t.Fatal(err)
	}
	find := func(name string) FormField {
		t.Helper()
		for _, f := range fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("missing field %s", name)
		return FormField{}
	}
	if len(fields) != 9 {
		t.Fatalf("editable widgets=%d, want 9", len(fields))
	}
	initial, err := RenderFormPage(input, 0, 72)
	if err != nil {
		t.Fatal(err)
	}
	for _, y := range []int{105, 132} {
		r, g, b, _ := initial.At(170, y).RGBA()
		if b <= r+4000 || g <= r+1500 {
			t.Fatalf("initial selected row y=%d missing from synthesized appearance", y)
		}
	}
	colors := find("colors")
	if !reflect.DeepEqual(colors.Values, []string{"r", "b"}) || !reflect.DeepEqual(colors.DefaultValues, []string{"g"}) {
		t.Fatalf("multi values/defaults: %+v", colors)
	}
	button := find("reset_subset")
	if button.Kind != FormReset || button.Caption != "Reset profile" || !reflect.DeepEqual(button.ResetFields, []string{"password", "profile"}) {
		t.Fatalf("reset action: %+v", button)
	}
	if !find("reset_exclude").ResetExclude {
		t.Fatal("exclude flag missing")
	}
	password := find("password")
	output, err := FillFormValues(input, map[int][]string{colors.ID: {"r", "g"}, password.ID: {"sècret"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = api.Validate(context.Background(), bytes.NewReader(output), nil, nil); err != nil {
		t.Fatal(err)
	}
	fields, err = ReadForms(output)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(find("colors").Values, []string{"r", "g"}) || find("password").Value != "sècret" {
		t.Fatal("logical field values did not survive save")
	}
	ctx, err := api.ReadContext(context.Background(), bytes.NewReader(output), nil)
	if err != nil {
		t.Fatal(err)
	}
	dict := ctx.Table[password.ID].Object.(types.Dict)
	ap, err := ctx.DereferenceDict(dict["AP"])
	if err != nil {
		t.Fatal(err)
	}
	normal, _, err := ctx.DereferenceStreamDict(ap["N"])
	if err != nil {
		t.Fatal(err)
	}
	if err = normal.Decode(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(normal.Content, []byte("secret")) || bytes.Contains(normal.Content, []byte("sècret")) || !bytes.Contains(normal.Content, []byte("******")) {
		t.Fatalf("password appearance is not masked: %s", normal.Content)
	}
	preview, err := RenderFormPage(output, 0, 72)
	if err != nil {
		t.Fatal(err)
	}
	for _, y := range []int{105, 118} {
		r, g, b, _ := preview.At(170, y).RGBA()
		if b <= r+4000 || g <= r+1500 {
			t.Fatalf("selected row at y=%d not highlighted: %v", y, preview.At(170, y))
		}
	}
	// The linked list widget on page two needs the same values and appearance.
	second, err := RenderFormPage(output, 1, 72)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.At(170, 105), second.At(170, 105)) {
		t.Fatal("linked list appearance differs")
	}
	output, err = FillFormValues(output, map[int][]string{colors.ID: nil, password.ID: {""}})
	if err != nil {
		t.Fatal(err)
	}
	fields, err = ReadForms(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(find("colors").Values) != 0 || find("password").Value != "" {
		t.Fatal("cannot clear fields")
	}
}
