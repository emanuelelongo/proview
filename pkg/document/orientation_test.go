package document

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	img "emanuelelongo/proview/pkg/images"
)

func TestJPEGOrientationNormalizesViewOCRAndExport(t *testing.T) {
	pixels := image.NewRGBA(image.Rect(0, 0, 80, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			pixels.SetRGBA(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 6), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, pixels, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	// EXIF IFD0: little endian, one SHORT orientation entry with value 6.
	app1 := []byte{0xff, 0xe1, 0, 34, 'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	encoded := append(append(append([]byte{}, raw[:2]...), app1...), raw[2:]...)
	source, err := Load("camera.jpeg", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	want := (img.Transform{Turns: 1}).Apply(decoded)
	preview, err := doc.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Bounds().Size() != image.Pt(40, 80) {
		t.Fatal(preview.Bounds())
	}
	for _, p := range []image.Point{{2, 2}, {35, 72}, {19, 39}} {
		if color.RGBAModel.Convert(preview.At(p.X, p.Y)) != color.RGBAModel.Convert(want.At(p.X, p.Y)) {
			t.Fatal("preview orientation mismatch")
		}
	}
	engine := &recordingOCR{}
	if _, err = doc.RenderingSnapshot().SelectableText(context.Background(), 0, engine); err != nil {
		t.Fatal(err)
	}
	if engine.size != image.Pt(40, 80) {
		t.Fatal("OCR used raw camera orientation")
	}
	if doc.Dirty() || !bytes.Equal(doc.Original(), encoded) {
		t.Fatal("loading changed the source or history")
	}
	output, err := doc.Export(ExportOptions{Format: FormatJPEG})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Load("saved.jpeg", bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	size, err := reopened.PageSize(0)
	if err != nil || size != image.Pt(40, 80) {
		t.Fatal("export/reopen applied orientation twice", size, err)
	}
	if err = doc.ApplyCrop(image.Rect(0, 0, 20, 40), preview.Bounds()); err != nil {
		t.Fatal(err)
	}
	if _, err = doc.RenderingSnapshot().SelectableText(context.Background(), 0, engine); err != nil {
		t.Fatal(err)
	}
	if engine.size != image.Pt(20, 40) {
		t.Fatal("crop ignored normalized source orientation")
	}
}
