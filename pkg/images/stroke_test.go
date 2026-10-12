package images

import (
	"image"
	"image/color"
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
