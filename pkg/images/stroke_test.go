package images

import (
	"image"
	"image/color"
	"reflect"
	"testing"
)

func TestThickenAlpha(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 9, 9))
	source.SetNRGBA(4, 4, color.NRGBA{R: 25, G: 50, B: 75, A: 255})

	unchanged := ThickenAlpha(source, 0)
	if got := alphaPixels(unchanged); got != 1 {
		t.Fatalf("strength 0 visible pixels = %d, want 1", got)
	}

	thickened := ThickenAlpha(source, 1)
	if got := alphaPixels(thickened); got <= 1 {
		t.Fatalf("thickened visible pixels = %d, want more than 1", got)
	}
	if thickened.Bounds() != source.Bounds() {
		t.Fatalf("bounds = %v, want %v", thickened.Bounds(), source.Bounds())
	}
	if pixel := color.NRGBAModel.Convert(thickened.At(4, 3)).(color.NRGBA); pixel.R != 25 || pixel.A != 255 {
		t.Fatalf("expanded pixel = %#v", pixel)
	}
}

func TestThickenAlphaTypedPixelPathsMatchGeneric(t *testing.T) {
	bounds := image.Rect(4, 7, 11, 13)
	nrgba := image.NewNRGBA(bounds)
	rgba := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBA{R: uint8(x * 13), G: uint8(y * 17), B: uint8((x + y) * 11), A: uint8((x*y + 40) % 256)}
			nrgba.SetNRGBA(x, y, pixel)
			rgba.Set(x, y, pixel)
		}
	}
	for _, src := range []image.Image{nrgba, rgba} {
		got := ThickenAlpha(src, 4)
		want := ThickenAlpha(genericStrokeImage{src}, 4)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("typed path differs for %T", src)
		}
	}
}

func BenchmarkThickenAlphaLarge(b *testing.B) {
	src := image.NewNRGBA(image.Rect(0, 0, 2048, 1536))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = ThickenAlpha(src, 4)
	}
}

type genericStrokeImage struct{ image.Image }

func alphaPixels(input image.Image) int {
	count := 0
	for y := input.Bounds().Min.Y; y < input.Bounds().Max.Y; y++ {
		for x := input.Bounds().Min.X; x < input.Bounds().Max.X; x++ {
			_, _, _, alpha := input.At(x, y).RGBA()
			if alpha != 0 {
				count++
			}
		}
	}
	return count
}
