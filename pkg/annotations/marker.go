package annotations

import (
	vector "github.com/tdewolff/canvas"
	"math"
	"sort"
)

// MarkerHull sweeps a fixed 45-degree flat nib between two points. The narrow
// nib dimension is one fifth of its width, so direction naturally changes the
// apparent stroke width while the gesture remains freehand.
func MarkerHull(a, b Point, width float64) []Point {
	v := width / (2 * math.Sqrt2)
	thin := v * .2
	points := make([]Point, 0, 8)
	for _, p := range []Point{a, b} {
		for _, u := range []float64{-1, 1} {
			for _, w := range []float64{-1, 1} {
				points = append(points, Point{p.X + u*v + w*thin, p.Y - u*v + w*thin})
			}
		}
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].X == points[j].X {
			return points[i].Y < points[j].Y
		}
		return points[i].X < points[j].X
	})
	cross := func(o, a, b Point) float64 { return (a.X-o.X)*(b.Y-o.Y) - (a.Y-o.Y)*(b.X-o.X) }
	hull := make([]Point, 0, 8)
	for _, p := range points {
		for len(hull) >= 2 && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	lower := len(hull) + 1
	for i := len(points) - 2; i >= 0; i-- {
		p := points[i]
		for len(hull) >= lower && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	return hull[:len(hull)-1]
}
func flatMarkerPath(points []Point, width float64) *vector.Path {
	path := &vector.Path{}
	for i, b := range points {
		a := b
		if i > 0 {
			a = points[i-1]
		}
		polygon := MarkerHull(a, b, width)
		path.MoveTo(polygon[0].X, polygon[0].Y)
		for _, p := range polygon[1:] {
			path.LineTo(p.X, p.Y)
		}
		path.Close()
	}
	return path
}
