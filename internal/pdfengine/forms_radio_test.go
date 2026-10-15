//go:build cgo && !nocgo && !extlib

package pdfengine

import (
	"bytes"
	"context"
	"image"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestRadioWidgetsShareValueAndUpdateAppearance(t *testing.T) {
	input := fixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Annots [5 0 R 6 0 R] >>",
		"<< /FT /Btn /Ff 32768 /T (radio) /V /A /Kids [5 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 4 0 R /P 3 0 R /Rect [20 150 40 170] /F 4 /AS /A /AP << /N << /Off 7 0 R /A 8 0 R >> >> >>",
		"<< /Type /Annot /Subtype /Widget /Parent 4 0 R /P 3 0 R /Rect [60 150 80 170] /F 4 /AS /Off /AP << /N << /Off 7 0 R /B 8 0 R >> >> >>",
		fixtureStream("", "/Type /XObject /Subtype /Form /BBox [0 0 20 20] /Resources << >>"),
		fixtureStream("0 0 0 rg 2 2 16 16 re f\n", "/Type /XObject /Subtype /Form /BBox [0 0 20 20] /Resources << >>"),
	}, "")
	fields, err := ReadForms(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || fields[0].OnValue != "A" || fields[1].OnValue != "B" {
		t.Fatalf("radio fields=%+v", fields)
	}
	output, err := FillForms(input, map[int]string{fields[1].ID: "B"})
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
	if fields[0].Value != "B" || fields[1].Value != "B" {
		t.Fatalf("radio values=%+v", fields)
	}
	preview, err := RenderFormPage(output, 0, 72)
	if err != nil {
		t.Fatal(err)
	}
	if hasFormInk(preview, image.Rect(20, 30, 40, 50)) || !hasFormInk(preview, image.Rect(60, 30, 80, 50)) {
		t.Fatal("radio appearances do not match the selected value")
	}
}
