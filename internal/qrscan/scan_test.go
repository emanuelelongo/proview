package qrscan

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"emanuelelongo/proview/internal/textselect"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

func qrImage(t *testing.T, text string) image.Image {
	t.Helper()
	matrix, err := qrcode.NewQRCodeWriter().Encode(text, gozxing.BarcodeFormat_QR_CODE, 240, 240, map[gozxing.EncodeHintType]interface{}{gozxing.EncodeHintType_CHARACTER_SET: "UTF-8"})
	if err != nil {
		t.Fatal(err)
	}
	return matrix
}
func TestScanMultiplePayloadsAndGeometry(t *testing.T) {
	photo := image.NewRGBA(image.Rect(0, 0, 800, 400))
	draw.Draw(photo, photo.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	payloads := []string{"https://example.com/path?q=value,", "Caffè pronto\nSecond line"}
	for i, text := range payloads {
		draw.Draw(photo, image.Rect(40+i*400, 80, 280+i*400, 320), qrImage(t, text), image.Point{}, draw.Src)
	}
	layout, err := Scan(context.Background(), photo)
	if err != nil || len(layout.Words) != 2 {
		t.Fatalf("scan: %d results, %v", len(layout.Words), err)
	}
	for i, text := range payloads {
		hit := layout.WordAt(textselect.Point{X: float64(160+i*400) / 800, Y: .5}, false)
		if hit < 0 || layout.Words[hit].Text != text {
			t.Fatalf("payload/geometry mismatch: %+v", layout)
		}
	}
	if layout.WordAt(textselect.Point{X: .5, Y: .5}, false) >= 0 {
		t.Fatal("background became selectable")
	}
}
func TestScanRotatedAndMirrored(t *testing.T) {
	src := qrImage(t, "https://example.com")
	for _, mirror := range []bool{false, true} {
		out := image.NewRGBA(src.Bounds())
		for y := 0; y < 240; y++ {
			for x := 0; x < 240; x++ {
				sx := 239 - y
				if mirror {
					sx = y
				}
				out.Set(x, y, src.At(sx, x))
			}
		}
		layout, err := Scan(context.Background(), out)
		if err != nil || len(layout.Words) != 1 || layout.Words[0].Text != "https://example.com" || layout.WordAt(textselect.Point{X: .5, Y: .5}, false) != 0 {
			t.Fatalf("mirror=%v: %+v, %v", mirror, layout, err)
		}
	}
}
func TestScanEmptyAndCanceled(t *testing.T) {
	blank := image.NewRGBA(image.Rect(0, 0, 200, 200))
	draw.Draw(blank, blank.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	layout, err := Scan(context.Background(), blank)
	if err != nil || len(layout.Words) != 0 {
		t.Fatalf("blank: %+v %v", layout, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Scan(ctx, blank); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
