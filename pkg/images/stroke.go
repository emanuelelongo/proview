package images

import (
	"image"
	"image/color"
)

// ThickenAlpha expands the visible pixels without changing the image bounds.
// Strength is a UI-oriented value between 0 and 10.
func ThickenAlpha(src image.Image, strength int) image.Image {
	if src == nil {
		return nil
	}
	bounds := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	if bounds.Empty() {
		return dst
	}
	pixelAt := func(x, y int) color.NRGBA {
		return color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
	}
	switch pixels := src.(type) {
	case *image.NRGBA:
		pixelAt = func(x, y int) color.NRGBA {
			i := pixels.PixOffset(x, y)
			return color.NRGBA{R: pixels.Pix[i], G: pixels.Pix[i+1], B: pixels.Pix[i+2], A: pixels.Pix[i+3]}
		}
	case *image.RGBA:
		pixelAt = func(x, y int) color.NRGBA {
			i := pixels.PixOffset(x, y)
			return color.NRGBAModel.Convert(color.RGBA{
				R: pixels.Pix[i], G: pixels.Pix[i+1], B: pixels.Pix[i+2], A: pixels.Pix[i+3],
			}).(color.NRGBA)
		}
	}
	radius := 0
	if strength > 0 {
		radius = max(1, strength*max(bounds.Dx(), bounds.Dy())/500)
		radius = min(radius, 32)
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := pixelAt(x, y)
			if pixel.A == 0 {
				continue
			}
			dx, dy := x-bounds.Min.X, y-bounds.Min.Y
			for offsetY := -radius; offsetY <= radius; offsetY++ {
				for offsetX := -radius; offsetX <= radius; offsetX++ {
					if offsetX*offsetX+offsetY*offsetY > radius*radius {
						continue
					}
					targetX, targetY := dx+offsetX, dy+offsetY
					if targetX < 0 || targetY < 0 || targetX >= dst.Bounds().Dx() || targetY >= dst.Bounds().Dy() {
						continue
					}
					if dst.NRGBAAt(targetX, targetY).A < pixel.A {
						dst.SetNRGBA(targetX, targetY, pixel)
					}
				}
			}
		}
	}
	return dst
}
