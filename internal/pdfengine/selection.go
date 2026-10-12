package pdfengine

import (
	"math"
	"unicode"
)

// GlyphAt uses physical page distances, independent of aspect ratio and zoom.
// A negative result means the pointer did not start on selectable text.
func (l Layout) GlyphAt(point Point, near bool) int {
	best, distance := -1, math.Inf(1)
	for i, glyph := range l.Glyphs {
		if unicode.IsSpace(glyph.Rune) {
			continue
		}
		x0, y0, x1, y1 := glyph.Quad.Bounds()
		dx := math.Max(math.Max(x0-point.X, point.X-x1), 0) * l.Width
		dy := math.Max(math.Max(y0-point.Y, point.Y-y1), 0) * l.Height
		d := dx*dx + dy*dy
		if d < distance {
			best, distance = i, d
		}
	}
	if !near && distance > 4 {
		return -1
	}
	return best
}

func (q Quad) Bounds() (float64, float64, float64, float64) {
	x0, y0, x1, y1 := q.UL.X, q.UL.Y, q.UL.X, q.UL.Y
	for _, p := range []Point{q.UR, q.LL, q.LR} {
		x0 = math.Min(x0, p.X)
		y0 = math.Min(y0, p.Y)
		x1 = math.Max(x1, p.X)
		y1 = math.Max(y1, p.Y)
	}
	return x0, y0, x1, y1
}

func (q Quad) Valid() bool {
	points := []Point{q.UL, q.UR, q.LR, q.LL}
	area := 0.
	sign := 0.
	for i, p := range points {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) || p.X < 0 || p.X > 1 || p.Y < 0 || p.Y > 1 {
			return false
		}
		next := points[(i+1)%len(points)]
		area += p.X*next.Y - next.X*p.Y
		after := points[(i+2)%len(points)]
		cross := (next.X-p.X)*(after.Y-next.Y) - (next.Y-p.Y)*(after.X-next.X)
		if math.Abs(cross) < 1e-12 {
			return false
		}
		if sign == 0 {
			sign = cross
		} else if sign*cross < 0 {
			return false
		}
	}
	return math.Abs(area) > 1e-12
}

// Selection snaps both endpoints to words and keeps each line's orientation.
// Adjacent ligature glyphs can share geometry and are included together.
func (l Layout) Selection(start, end int) []Quad {
	if start < 0 || end < 0 || start >= len(l.Glyphs) || end >= len(l.Glyphs) {
		return nil
	}
	if end < start {
		start, end = end, start
	}
	for start > 0 && l.Glyphs[start-1].Line == l.Glyphs[start].Line && !unicode.IsSpace(l.Glyphs[start-1].Rune) {
		start--
	}
	for end+1 < len(l.Glyphs) && l.Glyphs[end+1].Line == l.Glyphs[end].Line && !unicode.IsSpace(l.Glyphs[end+1].Rune) {
		end++
	}
	var quads []Quad
	for i := start; i <= end; {
		j := i
		for j+1 <= end && l.Glyphs[j+1].Line == l.Glyphs[i].Line {
			j++
		}
		nextLine := j + 1
		for i <= j && unicode.IsSpace(l.Glyphs[i].Rune) {
			i++
		}
		for j >= i && unicode.IsSpace(l.Glyphs[j].Rune) {
			j--
		}
		if i > j {
			i = nextLine
			continue
		}
		quad := lineQuad(l.Glyphs[i : j+1])
		// Source glyphs can straddle the CropBox. Clip the visible selection.
		for _, p := range []*Point{&quad.UL, &quad.UR, &quad.LL, &quad.LR} {
			p.X = math.Max(0, math.Min(1, p.X))
			p.Y = math.Max(0, math.Min(1, p.Y))
		}
		if !quad.Valid() {
			// Clipping a rotated band at the CropBox may collapse two corners.
			// A conservative visible bounding box still covers the selection.
			x0, y0, x1, y1 := quad.Bounds()
			quad = Quad{UL: Point{x0, y0}, UR: Point{x1, y0}, LL: Point{x0, y1}, LR: Point{x1, y1}}
		}
		if quad.Valid() {
			quads = append(quads, quad)
		}
		i = nextLine
	}
	return quads
}

// Project every glyph onto the first glyph's axes. This covers mixed font sizes,
// reversed writing order and ligatures without losing the line orientation.
func lineQuad(glyphs []Glyph) Quad {
	for _, glyph := range glyphs {
		q := glyph.Quad
		u, v := Point{q.UR.X - q.UL.X, q.UR.Y - q.UL.Y}, Point{q.LL.X - q.UL.X, q.LL.Y - q.UL.Y}
		det := u.X*v.Y - u.Y*v.X
		if math.Abs(det) < 1e-15 {
			continue
		}
		minU, minV, maxU, maxV := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, g := range glyphs {
			for _, p := range []Point{g.Quad.UL, g.Quad.UR, g.Quad.LL, g.Quad.LR} {
				x, y := p.X-q.UL.X, p.Y-q.UL.Y
				a, b := (x*v.Y-y*v.X)/det, (u.X*y-u.Y*x)/det
				minU, maxU = math.Min(minU, a), math.Max(maxU, a)
				minV, maxV = math.Min(minV, b), math.Max(maxV, b)
			}
		}
		point := func(a, b float64) Point { return Point{q.UL.X + a*u.X + b*v.X, q.UL.Y + a*u.Y + b*v.Y} }
		return Quad{UL: point(minU, minV), UR: point(maxU, minV), LL: point(minU, maxV), LR: point(maxU, maxV)}
	}
	return Quad{}
}
