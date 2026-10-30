package document

import (
	"image"
	"image/color"
	"image/draw"

	xdraw "golang.org/x/image/draw"
)

// MultiplyHighlight returns an ordinary alpha-composited fragment that gives the
// Multiply blend result over base. Black text remains black; white becomes ink.
// Pixels outside the artwork remain transparent, including inter-line gaps.
func MultiplyHighlight(base, artwork image.Image, target image.Rectangle) image.Image {
	mask := image.NewNRGBA(image.Rect(0, 0, target.Dx(), target.Dy()))
	if artwork.Bounds().Size() == mask.Bounds().Size() {
		draw.Draw(mask, mask.Bounds(), artwork, artwork.Bounds().Min, draw.Src)
	} else {
		xdraw.BiLinear.Scale(mask, mask.Bounds(), artwork, artwork.Bounds(), draw.Src, nil)
	}
	// Composed pages and live draft backdrops are NRGBA. Reading their pixels
	// directly avoids allocating interface colors for every highlighted pixel.
	pixels, _ := base.(*image.NRGBA)
	rgba, _ := base.(*image.RGBA)
	for y := 0; y < target.Dy(); y++ {
		for x := 0; x < target.Dx(); x++ {
			ink := mask.NRGBAAt(x, y)
			if ink.A == 0 {
				continue
			}
			var under color.NRGBA
			if pixels != nil {
				under = pixels.NRGBAAt(target.Min.X+x, target.Min.Y+y)
			} else if rgba != nil {
				// Premultiplied pixels are already straight when opaque.
				if p := rgba.RGBAAt(target.Min.X+x, target.Min.Y+y); p.A == 255 {
					under = color.NRGBA{p.R, p.G, p.B, 255}
				} else {
					under = color.NRGBAModel.Convert(p).(color.NRGBA)
				}
			} else {
				under = color.NRGBAModel.Convert(base.At(target.Min.X+x, target.Min.Y+y)).(color.NRGBA)
			}
			blend := func(c, b uint8) uint8 {
				return uint8((uint32(c)*(255*255-uint32(under.A)*(255-uint32(b))) + 32512) / 65025)
			}
			ink.R, ink.G, ink.B = blend(ink.R, under.R), blend(ink.G, under.G), blend(ink.B, under.B)
			mask.SetNRGBA(x, y, ink)
		}
	}
	return mask
}
