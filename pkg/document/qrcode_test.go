package document

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	"codeberg.org/go-pdf/fpdf"
	"emanuelelongo/proview/internal/textselect"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

func qrPNG(t *testing.T) []byte {
	t.Helper()
	matrix, err := qrcode.NewQRCodeWriter().Encode("https://example.com/qr", gozxing.BarcodeFormat_QR_CODE, 240, 240, nil)
	if err != nil {
		t.Fatal(err)
	}
	photo := image.NewRGBA(image.Rect(0, 0, 800, 400))
	draw.Draw(photo, photo.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(photo, image.Rect(40, 80, 280, 320), matrix, image.Point{}, draw.Src)
	var data bytes.Buffer
	if err = png.Encode(&data, photo); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
func TestImageQRCodesFollowCropAndRotation(t *testing.T) {
	source, err := Load("qr.png", bytes.NewReader(qrPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, step := range []struct {
		action string
		point  textselect.Point
	}{
		{"", textselect.Point{X: .2, Y: .5}}, {"right", textselect.Point{X: .5, Y: .2}}, {"horizontal", textselect.Point{X: .5, Y: .2}},
	} {
		if step.action != "" {
			if err = d.TransformImage(step.action); err != nil {
				t.Fatal(err)
			}
		}
		layout, err := d.RenderingSnapshot().SelectableQRCodes(context.Background(), 0)
		if err != nil || len(layout.Words) != 1 || layout.WordAt(step.point, false) != 0 {
			t.Fatalf("%s: %+v %v", step.action, layout, err)
		}
	}
	d.Undo()
	d.Undo()
	if err = d.ApplyCrop(image.Rect(0, 0, 400, 400), image.Rect(0, 0, 800, 400)); err != nil {
		t.Fatal(err)
	}
	layout, err := d.RenderingSnapshot().SelectableQRCodes(context.Background(), 0)
	if err != nil || layout.WordAt(textselect.Point{X: .4, Y: .5}, false) != 0 {
		t.Fatalf("crop: %+v %v", layout, err)
	}
}
func TestPDFQRCodesWithNativeTextAndSharedRedaction(t *testing.T) {
	pdf := fpdf.New("L", "mm", "A4", "")
	opts := fpdf.ImageOptions{ImageType: "PNG"}
	pdf.RegisterImageOptionsReader("qr", opts, bytes.NewReader(qrPNG(t)))
	for i := 0; i < 2; i++ {
		pdf.AddPage()
		pdf.ImageOptions("qr", 0, 0, 240, 120, false, opts, 0, "")
		pdf.SetFont("Helvetica", "", 14)
		pdf.Text(20, 150, "Native searchable text")
	}
	var data bytes.Buffer
	if err := pdf.Output(&data); err != nil {
		t.Fatal(err)
	}
	source, err := Load("qr.pdf", bytes.NewReader(data.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for page := 0; page < 2; page++ {
		layout, err := d.RenderingSnapshot().SelectableQRCodes(context.Background(), page)
		if err != nil || len(layout.Words) != 1 {
			t.Fatalf("page %d: %+v %v", page, layout, err)
		}
	}
	if err = d.TransformPDFPage(0, "right"); err != nil {
		t.Fatal(err)
	}
	rotated, err := d.RenderingSnapshot().SelectableQRCodes(context.Background(), 0)
	if err != nil || rotated.WordAt(textselect.Point{X: .71, Y: .16}, false) < 0 {
		t.Fatalf("rotated PDF QR: %+v %v", rotated, err)
	}
	if !d.Undo() {
		t.Fatal("could not restore page rotation")
	}
	// Cover the entire QR in the first reuse; actual sanitation must protect both.
	q := TextQuad{UL: TextPoint{X: 0, Y: 0}, UR: TextPoint{X: .4, Y: 0}, LR: TextPoint{X: .4, Y: .6}, LL: TextPoint{X: 0, Y: .6}}
	if _, err = d.AddRedaction(0, []TextQuad{q}); err != nil {
		t.Fatal(err)
	}
	for page := 0; page < 2; page++ {
		layout, err := d.RenderingSnapshot().SelectableQRCodes(context.Background(), page)
		if err != nil || len(layout.Words) != 0 {
			t.Fatalf("redacted page %d: %+v %v", page, layout, err)
		}
	}
	d.source.original = []byte("%PDF-invalid")
	if layout, err := d.RenderingSnapshot().SelectableQRCodes(context.Background(), 0); err == nil || len(layout.Words) != 0 {
		t.Fatal("sanitization error exposed original QR")
	}
}
