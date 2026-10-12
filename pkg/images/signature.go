package images

import (
	"image"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
)

func AddSignatureAt(base, signature image.Image, target image.Rectangle) image.Image {
	if base == nil || signature == nil {
		return base
	}
	baseBounds := base.Bounds()
	target = target.Canon().Intersect(baseBounds)
	if baseBounds.Empty() || target.Empty() || signature.Bounds().Empty() {
		return base
	}

	destination := image.NewNRGBA(baseBounds)
	draw.Draw(destination, baseBounds, base, baseBounds.Min, draw.Src)

	signatureBounds := signature.Bounds()
	scale := math.Min(
		float64(target.Dx())/float64(signatureBounds.Dx()),
		float64(target.Dy())/float64(signatureBounds.Dy()),
	)
	width := max(1, int(math.Round(float64(signatureBounds.Dx())*scale)))
	height := max(1, int(math.Round(float64(signatureBounds.Dy())*scale)))
	scaled := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), signature, signatureBounds, draw.Src, nil)

	x := target.Min.X + (target.Dx()-width)/2
	y := target.Min.Y + (target.Dy()-height)/2
	draw.Draw(destination, image.Rect(x, y, x+width, y+height), scaled, scaled.Bounds().Min, draw.Over)
	return destination
}
