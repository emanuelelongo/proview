package ocr

import (
	"context"
	"image"
	"image/color"
)

// Tesseract's adaptive thresholding expects dark lettering on a light page.
// Normalize predominantly dark inputs before deskewing and recognition, keeping
// the original pixels and normalized selection coordinates unchanged.
func normalizePolarity(ctx context.Context, src image.Image) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b := src.Bounds()
	step := max(1, max(b.Dx(), b.Dy())/256)
	dark, samples := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y += step {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := b.Min.X; x < b.Max.X; x += step {
			if opaqueGray(src.At(x, y)) < 128 {
				dark++
			}
			samples++
		}
	}
	if samples == 0 || dark*4 < samples*3 {
		return src, nil
	}
	out := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < b.Dx(); x++ {
			out.SetGray(x, y, color.Gray{Y: 255 - opaqueGray(src.At(b.Min.X+x, b.Min.Y+y))})
		}
	}
	return out, nil
}

func opaqueGray(c color.Color) uint8 {
	r, g, b, a := c.RGBA()
	// RGBA components are premultiplied; flatten transparency onto white.
	return uint8(((299*r+587*g+114*b)/1000 + 65535 - a) >> 8)
}
