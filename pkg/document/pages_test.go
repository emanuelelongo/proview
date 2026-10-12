package document

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestPDFPageEditingPreservesOrderArtworkAndHistory(t *testing.T) {
	load := func(name string) *Source {
		t.Helper()
		s, err := Load(name+".pdf", bytes.NewReader(textPDF(t, name)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		return s
	}
	a, b, c := load("Alpha"), load("Bravo"), load("Charlie")
	doc, _ := New(a)
	overlay, err := doc.AddSignature(0, NormalizedRect{X: .2, Y: .2, Width: .2, Height: .2}, opaqueSignaturePNG(t))
	if err != nil {
		t.Fatal(err)
	}
	doc.MarkSaved()
	if err := doc.AppendPDFs(b, c); err != nil {
		t.Fatal(err)
	}
	if doc.PageCount() != 3 || b.pages[0].preview != nil || c.pages[0].preview != nil {
		t.Fatal("appending eagerly rendered pages")
	}
	ids := doc.PageIDs()
	snapshot := doc.RenderingSnapshot()
	if err := doc.SetPageOrder([]int{2, 0, 1}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(doc.PageIDs(), []uint64{ids[2], ids[0], ids[1]}) || doc.Signatures()[0].Page != 1 || doc.Signatures()[0].ID != overlay.ID {
		t.Fatal("reorder lost page identity or annotation")
	}
	if !slices.Equal(snapshot.PageIDs(), ids) {
		t.Fatal("snapshot changed with live document")
	}
	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	contents := map[int]string{}
	if err := api.ExtractContent(context.Background(), bytes.NewReader(output), nil, func(r io.Reader, page int) error { data, e := io.ReadAll(r); contents[page] += string(data); return e }, nil); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"Charlie", "Alpha", "Bravo"} {
		if !strings.Contains(contents[i+1], name) || !strings.Contains(contents[i+1], "0 0 m 72 72 l S") {
			t.Fatalf("page %d lost text/vector content: %s", i+1, contents[i+1])
		}
	}
	exported, err := Load("combined.pdf", bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	defer exported.Close()
	preview, err := exported.Preview(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := colorBounds(preview, func(r, g, b, a uint32) bool { return a > 0 && r > 0xc000 && g < 0x3000 && b < 0x3000 }); !ok {
		t.Fatal("overlay missing from relocated page")
	}
	if err := doc.SetPageOrder([]int{0, 2}); err != nil {
		t.Fatal(err)
	}
	if len(doc.Signatures()) != 0 {
		t.Fatal("deleting a page retained its overlay")
	}
	if !doc.Undo() || len(doc.Signatures()) != 1 || doc.Signatures()[0].Page != 1 {
		t.Fatal("Undo did not restore deleted page objects")
	}
	if !doc.Undo() || !slices.Equal(doc.PageIDs(), ids) || doc.Signatures()[0].Page != 0 {
		t.Fatal("Undo did not restore original order")
	}
	if !doc.Undo() || doc.PageCount() != 1 || doc.Dirty() {
		t.Fatal("Undo append did not restore saved document")
	}
	if !doc.Redo() || doc.PageCount() != 3 {
		t.Fatal("Redo append failed")
	}
}

func TestBlankPDFPagePreservesVisibleSizeAndUndo(t *testing.T) {
	source, err := Load("rotated.pdf", bytes.NewReader(boxedPDF(t, "[0 0 100 200]", "[10 20 90 180]", 90)))
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := New(source)
	defer doc.Close()
	extra, err := Load("text.pdf", bytes.NewReader(textPDF(t, "Preserved")))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.AppendPDFs(extra); err != nil {
		t.Fatal(err)
	}
	overlay, err := doc.AddSignature(1, NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, opaqueSignaturePNG(t))
	if err != nil {
		t.Fatal(err)
	}
	doc.MarkSaved()
	ids := doc.PageIDs()
	blank, err := doc.RenderingSnapshot().BlankPageAfter(0)
	if err != nil {
		t.Fatal(err)
	}
	wantSize, _ := doc.PageSize(0)
	if gotSize, _ := blank.PageSize(0); gotSize != wantSize {
		t.Fatalf("blank size %v, want visible size %v", gotSize, wantSize)
	}
	if err := doc.InsertBlankPage(0, blank); err != nil {
		blank.Close()
		t.Fatal(err)
	}
	if doc.PageCount() != 3 || !doc.Dirty() || doc.PageIDs()[0] != ids[0] || doc.PageIDs()[2] != ids[1] || doc.Signatures()[0].ID != overlay.ID || doc.Signatures()[0].Page != 2 {
		t.Fatal("insertion lost page identity, overlay position or dirty state")
	}
	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := Load("result.pdf", bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	defer exported.Close()
	if exported.PageCount() != 3 {
		t.Fatal("export lost the blank page")
	}
	preview, err := exported.Preview(1)
	if err != nil {
		t.Fatal(err)
	}
	for y := preview.Bounds().Min.Y; y < preview.Bounds().Max.Y; y++ {
		for x := preview.Bounds().Min.X; x < preview.Bounds().Max.X; x++ {
			r, g, b, _ := preview.At(x, y).RGBA()
			if r != 65535 || g != 65535 || b != 65535 {
				t.Fatal("inserted page is not white")
			}
		}
	}
	var contents strings.Builder
	if err := api.ExtractContent(context.Background(), bytes.NewReader(output), []string{"3"}, func(r io.Reader, _ int) error { _, err := io.Copy(&contents, r); return err }, nil); err != nil || !strings.Contains(contents.String(), "Preserved") {
		t.Fatalf("existing PDF text was not preserved: %v", err)
	}
	if !doc.Undo() || !slices.Equal(doc.PageIDs(), ids) || doc.Dirty() || doc.Signatures()[0].Page != 1 {
		t.Fatal("one Undo did not restore page order, overlays and saved state")
	}
	if !doc.Redo() || doc.PageCount() != 3 || doc.Signatures()[0].Page != 2 {
		t.Fatal("Redo did not restore inserted page")
	}
}

func TestPDFPageOrderValidationAndCropBoxes(t *testing.T) {
	a, err := Load("rotated.pdf", bytes.NewReader(boxedPDF(t, "[0 0 100 200]", "[10 20 90 180]", 90)))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Load("plain.pdf", bytes.NewReader(minimalPDF(t, 2)))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	doc, _ := New(a)
	if err := doc.AppendPDFs(b); err != nil {
		t.Fatal(err)
	}
	ids := doc.PageIDs()
	revision := doc.Revision()
	for _, order := range [][]int{nil, {0, 0}, {-1}, {3}} {
		if doc.SetPageOrder(order) == nil {
			t.Fatalf("invalid order accepted: %v", order)
		}
	}
	if !slices.Equal(ids, doc.PageIDs()) || revision != doc.Revision() {
		t.Fatal("invalid edit mutated document")
	}
	if err := doc.SetPageOrder([]int{2, 0}); err != nil {
		t.Fatal(err)
	}
	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	boxes, err := api.Boxes(context.Background(), bytes.NewReader(output), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes) != 2 || boxes[1].Rot != 90 || boxes[1].CropBox().Width() != 80 || boxes[1].CropBox().Height() != 160 {
		t.Fatal("reordered PDF lost crop box or rotation")
	}
}

func TestSinglePDFReorderingRetainsRequestedSequence(t *testing.T) {
	var merged bytes.Buffer
	readers := []io.ReadSeeker{bytes.NewReader(textPDF(t, "First")), bytes.NewReader(textPDF(t, "Second")), bytes.NewReader(textPDF(t, "Third"))}
	if err := api.MergeRaw(context.Background(), readers, &merged, false, nil); err != nil {
		t.Fatal(err)
	}
	source, err := Load("single.pdf", bytes.NewReader(merged.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	doc, _ := New(source)
	if err := doc.SetPageOrder([]int{2, 0, 1}); err != nil {
		t.Fatal(err)
	}
	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	contents := map[int]string{}
	if err := api.ExtractContent(context.Background(), bytes.NewReader(output), nil, func(r io.Reader, page int) error { data, e := io.ReadAll(r); contents[page] += string(data); return e }, nil); err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{"Third", "First", "Second"} {
		if !strings.Contains(contents[i+1], text) {
			t.Fatalf("collected page %d did not contain %s: %s", i+1, text, contents[i+1])
		}
	}
	if err := doc.SetPageOrder([]int{1}); err != nil {
		t.Fatal(err)
	}
	output, err = doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	count, err := api.PageCount(context.Background(), bytes.NewReader(output), nil)
	if err != nil || count != 1 {
		t.Fatalf("delete export: count=%d, error=%v", count, err)
	}
}

func TestNewImageAndPDFCropCreateIndependentEditableImages(t *testing.T) {
	background := color.NRGBA{R: 10, G: 20, B: 30, A: 120}
	blank, err := NewImage("Untitled.png", 20, 10, background)
	if err != nil {
		t.Fatal(err)
	}
	preview, _ := blank.Preview(0)
	if preview.Bounds().Size() != image.Pt(20, 10) || color.NRGBAModel.Convert(preview.At(0, 0)) != background {
		t.Fatal("new image dimensions/background incorrect")
	}
	for _, size := range []image.Point{{0, 10}, {-1, 10}, {100000, 100000}, {16384, 16384}} {
		if _, err := NewImage("bad.png", size.X, size.Y, nil); err == nil {
			t.Fatal("unsafe image allocation accepted")
		}
	}
	source, err := Load("crop.pdf", bytes.NewReader(minimalPDF(t, 1)))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	doc, _ := New(source)
	if _, err := doc.AddSignature(0, NormalizedRect{X: .25, Y: .25, Width: .5, Height: .5}, opaqueSignaturePNG(t)); err != nil {
		t.Fatal(err)
	}
	revision := doc.Revision()
	crop, err := doc.CropPage(0, image.Rect(36, 36, 108, 108), "crop.png")
	if err != nil {
		t.Fatal(err)
	}
	cropped, _ := crop.Preview(0)
	if cropped.Bounds().Size() != image.Pt(72, 72) || crop.Format() != FormatPNG || crop.Location() != "" {
		t.Fatal("crop is not an independent PNG")
	}
	r, g, b, _ := cropped.At(36, 36).RGBA()
	if r < 0xc000 || g > 0x3000 || b > 0x3000 {
		t.Fatal("crop omitted visible overlay")
	}
	if doc.Revision() != revision || doc.PageCount() != 1 || len(doc.Signatures()) != 1 {
		t.Fatal("cropping mutated PDF")
	}
	if _, err := crop.AddSignature(0, NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, opaqueSignaturePNG(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := crop.Export(ExportOptions{Format: FormatPNG}); err != nil {
		t.Fatal(err)
	}
}
