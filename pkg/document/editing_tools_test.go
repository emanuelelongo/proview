package document

import (
	"bytes"
	"context"
	"emanuelelongo/proview/pkg/annotations"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestDuplicateCopiesOverlayRedactionAndIndependentPageIdentity(t *testing.T) {
	source, e := Load("duplicate.pdf", bytes.NewReader(textPDF(t, "Sensitive text")))
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	d, _ := New(source)
	_, e = d.AddSignature(0, NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, opaqueSignaturePNG(t))
	if e != nil {
		t.Fatal(e)
	}
	q := TextQuad{UL: TextPoint{X: .1, Y: .1}, UR: TextPoint{X: .3, Y: .1}, LL: TextPoint{X: .1, Y: .3}, LR: TextPoint{X: .3, Y: .3}}
	if _, e = d.AddRedaction(0, []TextQuad{q}); e != nil {
		t.Fatal(e)
	}
	d.MarkSaved()
	if e = d.DuplicatePage(0); e != nil {
		t.Fatal(e)
	}
	if d.PageCount() != 2 || d.PageIDs()[0] == d.PageIDs()[1] {
		t.Fatal("page identity reused")
	}
	overlays := d.Overlays()
	if len(overlays) != 2 || overlays[0].ID == overlays[1].ID || overlays[1].Page != 1 {
		t.Fatal("overlay clone missing")
	}
	redactions := d.Redactions()
	if len(redactions) != 2 || redactions[0].ID == redactions[1].ID || redactions[0].PageID == redactions[1].PageID {
		t.Fatal("redaction clone missing")
	}
	if !d.Undo() || d.PageCount() != 1 || len(d.Overlays()) != 1 || d.Dirty() {
		t.Fatal("duplicate Undo did not restore saved state")
	}
	if !d.Redo() {
		t.Fatal("duplicate Redo failed")
	}
	if e = d.RemoveOverlay(overlays[1].ID); e != nil {
		t.Fatal(e)
	}
	if len(d.Overlays()) != 1 || d.Overlays()[0].ID != overlays[0].ID {
		t.Fatal("copy deletion affected original")
	}
}

func TestRotatePDFPagePreservesNativePageAndHistory(t *testing.T) {
	source, err := Load("rotate.pdf", bytes.NewReader(boxedPDF(t, "[0 0 300 400]", "[0 0 300 400]", 0)))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := doc.PageSize(0)
	if err = doc.TransformPDFPage(0, "right"); err != nil {
		t.Fatal(err)
	}
	after, _ := doc.PageSize(0)
	if after.X != before.Y || after.Y != before.X {
		t.Fatalf("page size did not rotate: %v => %v", before, after)
	}
	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	boxes, err := api.Boxes(context.Background(), bytes.NewReader(output), nil, nil)
	if err != nil || len(boxes) != 1 || boxes[0].Rot%360 != 90 {
		t.Fatalf("export did not retain a native PDF rotation: boxes=%v err=%v", boxes, err)
	}
	if !doc.Undo() {
		t.Fatal("page rotation was not undoable")
	}
	restored, _ := doc.PageSize(0)
	if restored != before {
		t.Fatalf("undo did not restore page size: %v", restored)
	}
	if err = doc.TransformPDFPage(0, "horizontal"); err == nil {
		t.Fatal("unsupported whole-page PDF flip was accepted")
	}
}
func TestTransformedVectorPDFAndImageHistory(t *testing.T) {
	source, e := Load("rotated.pdf", bytes.NewReader(boxedPDF(t, "[0 0 100 200]", "[10 20 90 180]", 90)))
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	d, _ := New(source)
	data := annotations.Default(annotations.Text)
	data.Text = "Test"
	o, e := d.AddAnnotation(0, NormalizedRect{X: .2, Y: .2, Width: .3, Height: .2}, data, prepareAnnotation(t, data))
	if e != nil {
		t.Fatal(e)
	}
	d.MarkSaved()
	if e = d.TransformOverlay(o.ID, "right"); e != nil {
		t.Fatal(e)
	}
	if e = d.TransformOverlay(o.ID, "horizontal"); e != nil {
		t.Fatal(e)
	}
	out, e := d.Export(ExportOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if e = api.Validate(context.Background(), bytes.NewReader(out), nil, nil); e != nil {
		t.Fatal(e)
	}
	if d.Overlays()[0].Transform.Turns != 1 {
		t.Fatal("transform lost")
	}
	d.Undo()
	d.Undo()
	if d.Dirty() {
		t.Fatal("transforms Undo failed")
	}
	base := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	base.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var pngBytes bytes.Buffer
	png.Encode(&pngBytes, base)
	imageSource, e := Load("image.png", bytes.NewReader(pngBytes.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	im, _ := New(imageSource)
	if e = im.TransformImage("right"); e != nil {
		t.Fatal(e)
	}
	p, e := im.RenderPage(0)
	if e != nil {
		t.Fatal(e)
	}
	if p.Bounds().Size() != image.Pt(2, 4) || color.NRGBAModel.Convert(p.At(1, 0)).(color.NRGBA).R != 255 {
		t.Fatal("image rotation wrong")
	}
	im.Undo()
	size, _ := im.PageSize(0)
	if size != image.Pt(4, 2) {
		t.Fatal("image rotation Undo failed")
	}
}
func TestLensUsesRedactedComposedContentAndLeavesSourcePDFSearchable(t *testing.T) {
	source, e := Load("lens.pdf", bytes.NewReader(textPDF(t, "Secret")))
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	d, _ := New(source)
	data := annotations.Default(annotations.Shape)
	data.Shape = annotations.Magnifier
	data.Points = []annotations.Point{{X: 20, Y: 20}, {X: 80, Y: 80}}
	o, e := d.AddAnnotation(0, NormalizedRect{X: .25, Y: .25, Width: .5, Height: .5}, data, prepareAnnotation(t, data))
	if e != nil {
		t.Fatal(e)
	}
	plain, e := d.Export(ExportOptions{})
	if e != nil {
		t.Fatal(e)
	}
	ps, e := Load("plain.pdf", bytes.NewReader(plain))
	if e != nil {
		t.Fatal(e)
	}
	ps.mu.Lock()
	if e = ps.openPDF(); e != nil {
		t.Fatal(e)
	}
	text, e := ps.pdf.Text(0)
	ps.mu.Unlock()
	ps.Close()
	if e != nil || !strings.Contains(text, "Secret") {
		t.Fatal("lens removed original searchable text")
	}
	q := TextQuad{UL: TextPoint{}, UR: TextPoint{X: 1}, LL: TextPoint{Y: 1}, LR: TextPoint{X: 1, Y: 1}}
	if _, e = d.AddRedaction(0, []TextQuad{q}); e != nil {
		t.Fatal(e)
	}
	lens, e := d.RenderLens(o.ID, 144)
	if e != nil {
		t.Fatal(e)
	}
	b := lens.Bounds()
	center := color.NRGBAModel.Convert(lens.At(b.Dx()/2, b.Dy()/2)).(color.NRGBA)
	if center != (color.NRGBA{A: 255}) {
		t.Fatalf("redaction revealed by lens: %v", center)
	}
	corner := color.NRGBAModel.Convert(lens.At(0, 0)).(color.NRGBA)
	if corner.A != 0 {
		t.Fatal("lens contains hidden corner pixels")
	}
	safe, e := d.SafeRenderPage(0, 144)
	if e != nil {
		t.Fatal(e)
	}
	c := color.NRGBAModel.Convert(safe.At(safe.Bounds().Dx()/2, safe.Bounds().Dy()/2)).(color.NRGBA)
	if c.R != 0 || c.G != 0 || c.B != 0 {
		t.Fatal("safe copy reveals redaction")
	}
}

func TestCropUsesTransformedImageCoordinates(t *testing.T) {
	base := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			c := color.NRGBA{R: 255, A: 255}
			if x >= 4 {
				c = color.NRGBA{B: 255, A: 255}
			}
			base.SetNRGBA(x, y, c)
		}
	}
	var data bytes.Buffer
	png.Encode(&data, base)
	source, e := Load("crop.png", bytes.NewReader(data.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	d, _ := New(source)
	d.TransformImage("right")
	p, e := d.RenderBasePage(0)
	if e != nil {
		t.Fatal(e)
	}
	if e = d.ApplyCrop(image.Rect(0, 0, 4, 4), p.Bounds()); e != nil {
		t.Fatal(e)
	}
	cropped, e := d.RenderPage(0)
	if e != nil {
		t.Fatal(e)
	}
	if cropped.Bounds().Size() != image.Pt(4, 4) {
		t.Fatalf("crop dimensions %v", cropped.Bounds())
	}
	c := color.NRGBAModel.Convert(cropped.At(2, 2)).(color.NRGBA)
	if c.R != 255 || c.B != 0 {
		t.Fatal("crop used untransformed coordinates")
	}
	d.Undo()
	size, _ := d.PageSize(0)
	if size != image.Pt(4, 8) {
		t.Fatal("crop Undo lost rotation")
	}
}
