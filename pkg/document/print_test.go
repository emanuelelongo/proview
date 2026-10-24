package document

import (
	"bytes"
	"context"
	"image"
	"io"
	"strings"
	"testing"

	"emanuelelongo/proview/internal/pdfengine"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestPrintPDFIncludesUnsavedEditsAndPreservesVectorPages(t *testing.T) {
	load := func(name string) *Source {
		source, err := Load(name+".pdf", bytes.NewReader(textPDF(t, name)))
		if err != nil {
			t.Fatal(err)
		}
		return source
	}
	doc, err := New(load("Alpha"))
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	if err := doc.AppendPDFs(load("Bravo"), load("Charlie")); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddSignature(1, NormalizedRect{X: .1, Y: .1, Width: .3, Height: .3}, opaqueSignaturePNG(t)); err != nil {
		t.Fatal(err)
	}
	original, revision := bytes.Clone(doc.Original()), doc.Revision()
	data, err := doc.RenderingSnapshot().PrintPDF(context.Background(), []int{1})
	if err != nil {
		t.Fatal(err)
	}
	var contents strings.Builder
	if err := api.ExtractContent(context.Background(), bytes.NewReader(data), nil, func(r io.Reader, _ int) error {
		b, err := io.ReadAll(r)
		contents.Write(b)
		return err
	}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(contents.String(), "Bravo") || !strings.Contains(contents.String(), "0 0 m 72 72 l S") || strings.Contains(contents.String(), "Alpha") {
		t.Fatal("print selection lost native text/vector content or included the wrong page")
	}
	printedSource, err := Load("printed.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	printed, err := New(printedSource)
	if err != nil {
		t.Fatal(err)
	}
	defer printed.Close()
	preview, err := pdfengine.RenderPrintPage(data, 0, 144)
	if err != nil {
		t.Fatal(err)
	}
	if printed.PageCount() != 1 || !containsRed(preview) {
		t.Fatal("print selection lost unsaved artwork")
	}
	if !doc.Dirty() || !doc.CanUndo() || doc.Revision() != revision || !bytes.Equal(doc.Original(), original) {
		t.Fatal("printing changed source, history or save state")
	}
}

func TestPrintUnsavedImageIncludesCropAndUsesWhitePaper(t *testing.T) {
	doc, err := NewImageFromImage("crop.png", image.NewNRGBA(image.Rect(0, 0, 120, 60)))
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	if err := doc.ApplyCrop(image.Rect(0, 0, 80, 40), image.Rect(0, 0, 120, 60)); err != nil {
		t.Fatal(err)
	}
	if err := doc.TransformImage("right"); err != nil {
		t.Fatal(err)
	}
	data, err := doc.RenderingSnapshot().PrintPDF(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pdfengine.RenderPrintPage(data, 0, 72)
	if err != nil {
		t.Fatal(err)
	}
	if page.Bounds().Size() != image.Pt(40, 80) {
		t.Fatalf("print size %v", page.Bounds())
	}
	r, g, b, a := page.At(0, 0).RGBA()
	if r != 65535 || g != 65535 || b != 65535 || a != 65535 {
		t.Fatal("transparent image did not print on white paper")
	}
	if !doc.Dirty() || doc.Location() != "" {
		t.Fatal("printing saved the generated image")
	}
}

func TestPrintPDFRejectsInvalidSelectionAndExportFailure(t *testing.T) {
	doc := redactionDocument(t)
	for _, pages := range [][]int{{-1}, {1}, {0, 0}} {
		if data, err := doc.RenderingSnapshot().PrintPDF(context.Background(), pages); err == nil || len(data) != 0 {
			t.Fatal("invalid print selection accepted", pages)
		}
	}
	if _, err := doc.AddRedaction(0, selectedWord(t, doc, "SECRET")); err != nil {
		t.Fatal(err)
	}
	doc.source.original = []byte("invalid")
	if data, err := doc.RenderingSnapshot().PrintPDF(context.Background(), nil); err == nil || len(data) != 0 {
		t.Fatal("failed sanitized export fell back to source content")
	}
}
