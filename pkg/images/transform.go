package images

import (
	"image"
	"image/color"
)

// Transform applies mirrors in source coordinates followed by clockwise turns.
type Transform struct {
	Turns        int
	FlipX, FlipY bool
}

func (t Transform) Rotate(turns int) Transform { t.Turns = (t.Turns + turns + 4) % 4; return t }
func (t Transform) Mirror(horizontal bool) Transform {
	if t.Turns%2 != 0 {
		horizontal = !horizontal
	}
	if horizontal {
		t.FlipX = !t.FlipX
	} else {
		t.FlipY = !t.FlipY
	}
	return t
}
func (t Transform) Apply(src image.Image) image.Image {
	if t == (Transform{}) {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if t.Turns%2 != 0 {
		ow, oh = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			u, v := x, y
			if t.FlipX {
				u = w - 1 - u
			}
			if t.FlipY {
				v = h - 1 - v
			}
			switch t.Turns {
			case 1:
				u, v = h-1-v, u
			case 2:
				u, v = w-1-u, h-1-v
			case 3:
				u, v = v, w-1-u
			}
			// Typed access avoids boxing a color for every pixel of large rasters.
			sx, sy := b.Min.X+x, b.Min.Y+y
			switch pixels := src.(type) {
			case *image.NRGBA:
				out.SetNRGBA(u, v, pixels.NRGBAAt(sx, sy))
			case image.RGBA64Image:
				c := pixels.RGBA64At(sx, sy)
				if c.A == 0 {
					c = color.RGBA64{}
				}
				out.SetRGBA64(u, v, c)
			default:
				out.Set(u, v, src.At(sx, sy))
			}
		}
	}
	return out
}
