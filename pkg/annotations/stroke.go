package annotations

import (
	"math"

	vector "github.com/tdewolff/canvas"
)

// Each segment is a simple, closed capsule with consistent winding. Painting
// them as one nonzero-filled path applies opacity once across overlaps without
// running the intersection solver on an arbitrary freehand outline.
func roundStrokePath(centerline *vector.Path, width float64) *vector.Path {
	out := &vector.Path{}
	r := width / 2
	for _, part := range centerline.Split() {
		points := part.Coords()
		if len(points) == 1 {
			out = out.Append(vector.Circle(width/2).Translate(points[0].X, points[0].Y))
		}
		for i := 1; i < len(points); i++ {
			a, b := points[i-1], points[i]
			dx, dy := b.X-a.X, b.Y-a.Y
			length := math.Hypot(dx, dy)
			if length < 1e-9 {
				out = out.Append(vector.Circle(r).Translate(a.X, a.Y))
				continue
			}
			nx, ny := -dy/length*r, dx/length*r
			out.MoveTo(a.X-nx, a.Y-ny)
			out.LineTo(b.X-nx, b.Y-ny)
			out.ArcTo(r, r, 0, false, true, b.X+nx, b.Y+ny)
			out.LineTo(a.X+nx, a.Y+ny)
			out.ArcTo(r, r, 0, false, true, a.X-nx, a.Y-ny)
			out.Close()
		}
	}
	return out
}
