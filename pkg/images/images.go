package images

import (
	"image"
	"image/draw"
)

func Crop(src image.Image, rect image.Rectangle) image.Image {
	normalized := rect.Canon()
	dst := image.NewRGBA(image.Rect(0, 0, normalized.Dx(), normalized.Dy()))
	draw.Draw(dst, dst.Bounds(), src, normalized.Min, draw.Src)
	return dst
}
