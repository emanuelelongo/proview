package ocr

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"

	"emanuelelongo/proview/internal/textselect"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

type rotation struct {
	width, height        int
	cosine, sine, tx, ty float64
}

// Estimate text direction from the concentration of contrast edges in parallel
// rows. Analysis uses a small image; recognition keeps the input resolution.
func skewAngle(ctx context.Context, src image.Image) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	b := src.Bounds()
	scale := math.Min(1, 800/float64(max(b.Dx(), b.Dy())))
	w, h := max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale))
	gray := image.NewGray(image.Rect(0, 0, w, h))
	xdraw.BiLinear.Scale(gray, gray.Bounds(), src, b, draw.Src, nil)
	points := make([]textselect.Point, 0)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			v := int(gray.GrayAt(x, y).Y)
			dx := abs(int(gray.GrayAt(x+1, y).Y) - int(gray.GrayAt(x-1, y).Y))
			dy := abs(int(gray.GrayAt(x, y+1).Y) - int(gray.GrayAt(x, y-1).Y))
			if v < 190 && max(dx, dy) >= 40 {
				points = append(points, textselect.Point{X: float64(x) - float64(w)/2, Y: float64(y) - float64(h)/2})
			}
		}
	}
	if len(points) < 100 {
		return 0, ctx.Err()
	}
	radius := int(math.Ceil(math.Hypot(float64(w), float64(h))/2)) + 2
	bins := make([]float64, radius*2+2)
	score := func(degrees float64) (float64, error) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		clear(bins)
		s, c := math.Sincos(degrees * math.Pi / 180)
		for _, p := range points {
			y := s*p.X + c*p.Y + float64(radius)
			i := int(y)
			f := y - float64(i)
			bins[i] += 1 - f
			bins[i+1] += f
		}
		sum := 0.0
		for _, v := range bins {
			sum += v * v
		}
		return sum, nil
	}
	baseline, err := score(0)
	if err != nil {
		return 0, err
	}
	best, angle, total := baseline, 0.0, 0.0
	for a := -40.0; a <= 40; a++ {
		v, e := score(a)
		if e != nil {
			return 0, e
		}
		total += v
		if v > best {
			best, angle = v, a
		}
	}
	// Avoid rotating ordinary pictures or already horizontal text without a clear
	// dominant direction. Perspective correction is deliberately excluded.
	if best < baseline*1.08 || best < total/81*1.25 {
		return 0, nil
	}
	coarse := angle
	for a := coarse - .9; a <= coarse+.9; a += .1 {
		v, e := score(a)
		if e != nil {
			return 0, e
		}
		if v > best {
			best, angle = v, a
		}
	}
	if math.Abs(angle) < .75 {
		return 0, nil
	}
	return angle * math.Pi / 180, nil
}
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// The output encloses all four source corners: rotation never crops the input.
func rotateForOCR(src image.Image, angle float64) (image.Image, rotation) {
	b := src.Bounds()
	s, c := math.Sincos(angle)
	r := rotation{width: b.Dx(), height: b.Dy(), cosine: c, sine: s}
	minX, minY, maxX, maxY := 0.0, 0.0, 0.0, 0.0
	for _, p := range []textselect.Point{{X: 0, Y: 0}, {X: float64(r.width), Y: 0}, {X: 0, Y: float64(r.height)}, {X: float64(r.width), Y: float64(r.height)}} {
		x, y := c*p.X-s*p.Y, s*p.X+c*p.Y
		minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
	}
	r.tx, r.ty = -minX+2, -minY+2
	out := image.NewRGBA(image.Rect(0, 0, int(math.Ceil(maxX-minX))+4, int(math.Ceil(maxY-minY))+4))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.BiLinear.Transform(out, f64.Aff3{c, -s, r.tx - c*float64(b.Min.X) + s*float64(b.Min.Y), s, c, r.ty - s*float64(b.Min.X) - c*float64(b.Min.Y)}, src, b, draw.Over, nil)
	return out, r
}
func (r rotation) restore(layout textselect.Layout) textselect.Layout {
	words := make([]textselect.Word, 0, len(layout.Words))
	for _, word := range layout.Words {
		word.Characters = append([]textselect.Character(nil), word.Characters...)
		for i := range word.Characters {
			for j, p := range word.Characters[i].Quad {
				x, y := p.X*layout.Width-r.tx, p.Y*layout.Height-r.ty
				word.Characters[i].Quad[j] = textselect.Point{X: (r.cosine*x + r.sine*y) / float64(r.width), Y: (-r.sine*x + r.cosine*y) / float64(r.height)}
			}
		}
		center := textselect.Point{}
		for j, p := range word.Quad {
			x, y := p.X*layout.Width-r.tx, p.Y*layout.Height-r.ty
			q := textselect.Point{X: (r.cosine*x + r.sine*y) / float64(r.width), Y: (-r.sine*x + r.cosine*y) / float64(r.height)}
			word.Quad[j] = q
			center.X += q.X / 4
			center.Y += q.Y / 4
		}
		if center.X >= 0 && center.X <= 1 && center.Y >= 0 && center.Y <= 1 {
			words = append(words, word)
		}
	}
	layout.Width, layout.Height, layout.Words = float64(r.width), float64(r.height), words
	return layout
}
