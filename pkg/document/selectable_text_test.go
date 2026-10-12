package document

import (
	"bytes"
	"codeberg.org/go-pdf/fpdf"
	"context"
	"emanuelelongo/proview/internal/textselect"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"testing"
)

type recordingOCR struct {
	calls  int
	size   image.Point
	center color.RGBA
}

func (e *recordingOCR) Recognize(_ context.Context, im image.Image) (textselect.Layout, error) {
	e.calls++
	e.size = im.Bounds().Size()
	e.center = color.RGBAModel.Convert(im.At(im.Bounds().Min.X+e.size.X/2, im.Bounds().Min.Y+e.size.Y/2)).(color.RGBA)
	return textselect.Layout{Width: float64(e.size.X), Height: float64(e.size.Y), Words: []textselect.Word{{Text: "recognized", Quad: textselect.Rect(.1, .2, .3, .4)}}}, nil
}
func TestSelectableNativeTextAndRedaction(t *testing.T) {
	d := redactionDocument(t)
	e := &recordingOCR{}
	l, err := d.RenderingSnapshot().SelectableText(context.Background(), 0, e)
	if err != nil {
		t.Fatal(err)
	}
	if e.calls != 0 || !strings.Contains(l.Text(0, len(l.Words)-1), "SECRET") {
		t.Fatal("native text not reused")
	}
	key := d.TextSelectionKey(0)
	if _, err = d.AddRedaction(0, selectedWord(t, d, "SECRET")); err != nil {
		t.Fatal(err)
	}
	if key == d.TextSelectionKey(0) {
		t.Fatal("redaction did not invalidate cache")
	}
	l, err = d.RenderingSnapshot().SelectableText(context.Background(), 0, e)
	if err != nil {
		t.Fatal(err)
	}
	text := l.Text(0, len(l.Words)-1)
	if strings.Contains(text, "SECRET") || !strings.Contains(text, "PUBLIC") {
		t.Fatalf("unsafe selectable text: %q", text)
	}
	if e.calls != 0 {
		t.Fatal("native sanitized text should not require OCR")
	}
}
func TestImageOCRCoordinatesAndCrop(t *testing.T) {
	d, err := NewImage("test.png", 1000, 500, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	e := &recordingOCR{}
	key := d.TextSelectionKey(0)
	if err = d.TransformImage("right"); err != nil {
		t.Fatal(err)
	}
	l, err := d.RenderingSnapshot().SelectableText(context.Background(), 0, e)
	if err != nil {
		t.Fatal(err)
	}
	if e.size != image.Pt(1000, 500) || l.Width != 500 || l.Height != 1000 || l.WordAt(textselect.Point{X: .7, Y: .2}, false) != 0 {
		t.Fatalf("rotation: %+v %+v", e, l)
	}
	if key == d.TextSelectionKey(0) {
		t.Fatal("rotation did not invalidate key")
	}
	d.Undo()
	if err = d.ApplyCrop(image.Rect(0, 0, 500, 250), image.Rect(0, 0, 1000, 500)); err != nil {
		t.Fatal(err)
	}
	_, err = d.RenderingSnapshot().SelectableText(context.Background(), 0, e)
	if err != nil {
		t.Fatal(err)
	}
	if e.size != image.Pt(500, 250) {
		t.Fatal(e.size)
	}
}
func TestOCRPixelLimitAndTransparency(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4000, 4000))
	result := ocrRaster(src)
	b := result.Bounds()
	if b.Dx()*b.Dy() > ocrMaxPixels || max(b.Dx(), b.Dy()) > ocrMaxDimension {
		t.Fatal(b)
	}
	r, g, bv, a := result.At(0, 0).RGBA()
	if r != 65535 || g != 65535 || bv != 65535 || a != 65535 {
		t.Fatal("transparent source was not flattened on white")
	}
}

func TestOCRUsesSanitizedSharedImages(t *testing.T) {
	pixels := image.NewRGBA(image.Rect(0, 0, 200, 100))
	draw.Draw(pixels, pixels.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	pdf := fpdf.New("P", "mm", "A4", "")
	options := fpdf.ImageOptions{ImageType: "PNG"}
	pdf.RegisterImageOptionsReader("shared", options, bytes.NewReader(encoded.Bytes()))
	for i := 0; i < 2; i++ {
		pdf.AddPage()
		pdf.ImageOptions("shared", 0, 0, 210, 297, false, options, 0, "")
	}
	var data bytes.Buffer
	if err := pdf.Output(&data); err != nil {
		t.Fatal(err)
	}
	source, err := Load("shared.pdf", bytes.NewReader(data.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	key := d.TextSelectionKey(1)
	e := &recordingOCR{}
	if _, err = d.RenderingSnapshot().SelectableText(context.Background(), 1, e); err != nil {
		t.Fatal(err)
	}
	if e.center.R < 240 || e.center.G > 10 {
		t.Fatal("fixture is not red")
	}
	q := TextQuad{UL: TextPoint{X: .4, Y: .4}, UR: TextPoint{X: .6, Y: .4}, LL: TextPoint{X: .4, Y: .6}, LR: TextPoint{X: .6, Y: .6}}
	if _, err = d.AddRedaction(0, []TextQuad{q}); err != nil {
		t.Fatal(err)
	}
	if d.TextSelectionKey(1) == key {
		t.Fatal("shared page cache not invalidated")
	}
	if _, err = d.RenderingSnapshot().SelectableText(context.Background(), 1, e); err != nil {
		t.Fatal(err)
	}
	if e.center.R > 240 && e.center.G < 10 {
		t.Fatal("OCR received censored pixels through a shared image reuse")
	}
}
func TestSelectableTextFailsClosedOnSanitizationError(t *testing.T) {
	d := redactionDocument(t)
	if _, err := d.AddRedaction(0, selectedWord(t, d, "SECRET")); err != nil {
		t.Fatal(err)
	}
	d.source.original = []byte("%PDF-invalid")
	e := &recordingOCR{}
	layout, err := d.RenderingSnapshot().SelectableText(context.Background(), 0, e)
	if err == nil || len(layout.Words) != 0 || e.calls != 0 {
		t.Fatal("sanitization failure fell back to original content")
	}
}

func TestOCRDownsamplingPreservesAspectRatioAndSelectionCoordinates(t *testing.T) {
	d, err := NewImage("camera.png", 3024, 4032, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	beforeDirty := d.Dirty()
	e := &recordingOCR{}
	l, err := d.RenderingSnapshot().SelectableText(context.Background(), 0, e)
	if err != nil {
		t.Fatal(err)
	}
	if e.size != image.Pt(1800, 2400) {
		t.Fatalf("OCR size %v", e.size)
	}
	if l.WordAt(textselect.Point{X: .2, Y: .3}, false) != 0 {
		t.Fatal("downsampling shifted selectable text")
	}
	size, err := d.PageSize(0)
	if err != nil || size != image.Pt(3024, 4032) || d.Dirty() != beforeDirty {
		t.Fatal("OCR altered source dimensions/history", size, err)
	}
	small := ocrRaster(image.NewNRGBA(image.Rect(0, 0, 320, 200)))
	if small.Bounds().Size() != image.Pt(320, 200) {
		t.Fatal("small source was upscaled")
	}
}
