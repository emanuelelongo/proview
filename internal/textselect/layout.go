// Package textselect describes transient selectable text independently of its source.
package textselect

import (
	"math"
	"strings"
)

type Point struct{ X, Y float64 }

// Quad is ordered around the perimeter, rather than by screen position.
type Quad [4]Point
type Word struct {
	Text        string
	Quad        Quad
	Block, Line int
	Confidence  float64
}
type Layout struct {
	Width, Height float64
	Words         []Word
}

func Rect(x0, y0, x1, y1 float64) Quad { return Quad{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}} }
func (q Quad) Contains(p Point) bool {
	sign := 0.0
	for i, a := range q {
		b := q[(i+1)%4]
		cross := (b.X-a.X)*(p.Y-a.Y) - (b.Y-a.Y)*(p.X-a.X)
		if math.Abs(cross) < 1e-12 {
			continue
		}
		if sign*cross < 0 {
			return false
		}
		sign = cross
	}
	return sign != 0
}

// WordAt requires a genuine hit at gesture start; dragging may snap to the nearest word.
func (l Layout) WordAt(p Point, near bool) int {
	best, distance := -1, math.Inf(1)
	for i, w := range l.Words {
		if w.Quad.Contains(p) {
			return i
		}
		if !near {
			continue
		}
		for j, a := range w.Quad {
			b := w.Quad[(j+1)%4]
			ax, ay := (a.X-p.X)*l.Width, (a.Y-p.Y)*l.Height
			dx, dy := (b.X-a.X)*l.Width, (b.Y-a.Y)*l.Height
			t := 0.0
			if d := dx*dx + dy*dy; d > 0 {
				t = math.Max(0, math.Min(1, -(ax*dx+ay*dy)/d))
			}
			d := (ax+t*dx)*(ax+t*dx) + (ay+t*dy)*(ay+t*dy)
			if d < distance {
				best, distance = i, d
			}
		}
	}
	return best
}
func (l Layout) Range(a, b int) []Word {
	if a > b {
		a, b = b, a
	}
	if a < 0 || b >= len(l.Words) {
		return nil
	}
	return l.Words[a : b+1]
}
func (l Layout) Text(a, b int) string {
	words := l.Range(a, b)
	var out strings.Builder
	for i, w := range words {
		if i > 0 {
			if words[i-1].Block != w.Block {
				out.WriteString("\n\n")
			} else if words[i-1].Line != w.Line {
				out.WriteByte('\n')
			} else {
				out.WriteByte(' ')
			}
		}
		out.WriteString(w.Text)
	}
	return out.String()
}
func (l *Layout) Transform(turns int, flipX, flipY bool) {
	for i := range l.Words {
		for j, p := range l.Words[i].Quad {
			if flipX {
				p.X = 1 - p.X
			}
			if flipY {
				p.Y = 1 - p.Y
			}
			for n := 0; n < turns; n++ {
				p = Point{1 - p.Y, p.X}
			}
			l.Words[i].Quad[j] = p
		}
	}
	if turns%2 != 0 {
		l.Width, l.Height = l.Height, l.Width
	}
}
