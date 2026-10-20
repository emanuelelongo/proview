package ocr

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func TestNormalizePolarity(t *testing.T) {
	for _, background := range []color.Color{color.White, color.Black, color.Transparent} {
		src := image.NewRGBA(image.Rect(7, 11, 107, 61))
		draw.Draw(src, src.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
		ink := color.Black
		if background == color.Black {
			ink = color.White
		}
		src.Set(20, 30, ink)
		got, err := normalizePolarity(context.Background(), src)
		if err != nil {
			t.Fatal(err)
		}
		if background != color.Black {
			if got != src {
				t.Fatal("light or transparent input was changed")
			}
			continue
		}
		if got.Bounds().Size() != src.Bounds().Size() || opaqueGray(got.At(0, 0)) != 255 || opaqueGray(got.At(13, 19)) != 0 {
			t.Fatal("inversion changed text geometry or polarity")
		}
		if opaqueGray(src.At(7, 11)) != 0 || opaqueGray(src.At(20, 30)) != 255 {
			t.Fatal("source pixels changed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := normalizePolarity(ctx, image.NewGray(image.Rect(0, 0, 10, 10))); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
