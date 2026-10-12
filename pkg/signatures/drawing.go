package signatures

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
)

type Point struct{ X, Y float64 }

type Drawing struct {
	Color   color.NRGBA `json:"color,omitempty"`
	Version int         `json:"version"`
	Strokes [][]Point   `json:"strokes"`
	Smooth  float64     `json:"smooth"`
	Width   float64     `json:"width"`
}

func (d Drawing) Clone() Drawing {
	d.Strokes = append([][]Point(nil), d.Strokes...)
	for i := range d.Strokes {
		d.Strokes[i] = append([]Point(nil), d.Strokes[i]...)
	}
	return d
}

func FromDrawing(d Drawing) (Asset, error) {
	data, err := drawingSVG(d, true)
	if err != nil {
		return Asset{}, err
	}
	d = d.Clone()
	return Asset{Kind: Drawn, Data: data, Drawing: &d}, nil
}

func RenderDrawing(d Drawing, pixels int) (image.Image, error) {
	data, err := drawingSVG(d, false)
	if err != nil {
		return nil, err
	}
	return (Asset{Kind: SVG, Data: data}).Render(pixels)
}

func drawingSVG(d Drawing, crop bool) ([]byte, error) {
	if d.Version != 1 || d.Smooth < 0 || d.Smooth > 100 || d.Width < 1 || d.Width > 20 || math.IsNaN(d.Smooth+d.Width) {
		return nil, errors.New("invalid drawing settings")
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	count := 0
	for _, stroke := range d.Strokes {
		for _, p := range stroke {
			if math.IsNaN(p.X+p.Y) || math.IsInf(p.X+p.Y, 0) || p.X < 0 || p.X > 1000 || p.Y < 0 || p.Y > 450 {
				return nil, errors.New("invalid drawing point")
			}
			minX, minY, maxX, maxY = min(minX, p.X), min(minY, p.Y), max(maxX, p.X), max(maxY, p.Y)
			count++
		}
	}
	if count == 0 {
		return nil, errors.New("draw a signature before saving")
	}
	if count > 100_000 {
		return nil, errors.New("signature has too many points")
	}
	x, y, w, h := 0.0, 0.0, 1000.0, 450.0
	if crop {
		x, y, w, h = minX-20, minY-20, maxX-minX+40, maxY-minY+40
	}
	if d.Color.A == 0 {
		d.Color = color.NRGBA{A: 255}
	}
	paint := fmt.Sprintf("#%02x%02x%02x", d.Color.R, d.Color.G, d.Color.B)
	var svg strings.Builder
	fmt.Fprintf(&svg, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="%.5f %.5f %.5f %.5f"><g fill="none" stroke="%s" stroke-width="%.5f" stroke-linecap="round" stroke-linejoin="round">`, x, y, w, h, paint, d.Width)
	for _, stroke := range d.Strokes {
		if len(stroke) == 0 {
			continue
		}
		points := resample(stroke, 1)
		if len(points) == 1 {
			fmt.Fprintf(&svg, `<circle cx="%.5f" cy="%.5f" r="%.5f" fill="%s" stroke="none"/>`, points[0].X, points[0].Y, d.Width/2, paint)
			continue
		}
		fmt.Fprintf(&svg, `<path d="M%.5f %.5f`, points[0].X, points[0].Y)
		if d.Smooth == 0 {
			for _, p := range stroke[1:] {
				fmt.Fprintf(&svg, " L%.5f %.5f", p.X, p.Y)
			}
		} else {
			curves := fitCurves(points, d.Smooth/20)
			for _, c := range curves {
				fmt.Fprintf(&svg, " C%.5f %.5f %.5f %.5f %.5f %.5f", c[1].X, c[1].Y, c[2].X, c[2].Y, c[3].X, c[3].Y)
			}
		}
		svg.WriteString(`"/>`)
	}
	svg.WriteString("</g></svg>")
	return []byte(svg.String()), nil
}

func add(a, b Point) Point         { return Point{a.X + b.X, a.Y + b.Y} }
func sub(a, b Point) Point         { return Point{a.X - b.X, a.Y - b.Y} }
func mul(a Point, s float64) Point { return Point{a.X * s, a.Y * s} }
func dot(a, b Point) float64       { return a.X*b.X + a.Y*b.Y }
func length(a Point) float64       { return math.Hypot(a.X, a.Y) }
func unit(a Point) Point {
	if l := length(a); l > 0 {
		return mul(a, 1/l)
	}
	return Point{1, 0}
}

// Resampling makes fitting independent of mouse event frequency.
func resample(points []Point, spacing float64) []Point {
	total := 0.0
	for i := 1; i < len(points); i++ {
		total += length(sub(points[i], points[i-1]))
	}
	spacing = max(spacing, total/20_000)
	out := []Point{points[0]}
	carry := 0.0
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		distance := length(sub(b, a))
		if distance == 0 {
			continue
		}
		for carry+distance >= spacing {
			t := (spacing - carry) / distance
			a = add(a, mul(sub(b, a), t))
			out = append(out, a)
			distance = length(sub(b, a))
			carry = 0
		}
		carry += distance
	}
	last := points[len(points)-1]
	if length(sub(last, out[len(out)-1])) > 1e-8 {
		out = append(out, last)
	}
	return out
}

type cubic [4]Point

// Schneider's recursive least-squares Bézier fitting, with anchored endpoints.
func fitCurves(points []Point, tolerance float64) []cubic {
	var out []cubic
	fit(points, unit(sub(points[1], points[0])), unit(sub(points[len(points)-2], points[len(points)-1])), tolerance*tolerance, 0, &out)
	return out
}

func evaluate(c cubic, t float64) Point {
	u := 1 - t
	return add(add(mul(c[0], u*u*u), mul(c[1], 3*u*u*t)), add(mul(c[2], 3*u*t*t), mul(c[3], t*t*t)))
}

func fit(p []Point, left, right Point, tolerance float64, depth int, out *[]cubic) {
	if len(p) == 2 {
		d := length(sub(p[1], p[0])) / 3
		*out = append(*out, cubic{p[0], add(p[0], mul(left, d)), add(p[1], mul(right, d)), p[1]})
		return
	}
	u := make([]float64, len(p))
	for i := 1; i < len(p); i++ {
		u[i] = u[i-1] + length(sub(p[i], p[i-1]))
	}
	for i := 1; i < len(p); i++ {
		u[i] /= u[len(u)-1]
	}
	curve := generate(p, u, left, right)
	maxError, split := curveError(p, u, curve)
	if maxError <= tolerance {
		*out = append(*out, curve)
		return
	}
	if maxError < tolerance*4 {
		for attempt := 0; attempt < 4; attempt++ {
			for i := 1; i < len(u)-1; i++ {
				t := u[i]
				v := 1 - t
				q := evaluate(curve, t)
				first := add(add(mul(sub(curve[1], curve[0]), 3*v*v), mul(sub(curve[2], curve[1]), 6*v*t)), mul(sub(curve[3], curve[2]), 3*t*t))
				second := add(mul(add(sub(curve[2], mul(curve[1], 2)), curve[0]), 6*v), mul(add(sub(curve[3], mul(curve[2], 2)), curve[1]), 6*t))
				delta := sub(q, p[i])
				denominator := dot(first, first) + dot(delta, second)
				if math.Abs(denominator) > 1e-12 {
					u[i] = max(u[i-1], min(u[i+1], t-dot(delta, first)/denominator))
				}
			}
			curve = generate(p, u, left, right)
			maxError, split = curveError(p, u, curve)
			if maxError <= tolerance {
				*out = append(*out, curve)
				return
			}
		}
	}
	if depth >= 32 {
		split = len(p) / 2
	}
	center := unit(sub(p[split-1], p[split+1]))
	fit(p[:split+1], left, center, tolerance, depth+1, out)
	fit(p[split:], mul(center, -1), right, tolerance, depth+1, out)
}

func generate(p []Point, u []float64, left, right Point) cubic {
	c := cubic{p[0], {}, {}, p[len(p)-1]}
	c00, c01, c11, x0, x1 := 0.0, 0.0, 0.0, 0.0, 0.0
	for i, t := range u {
		v := 1 - t
		b0, b1, b2, b3 := v*v*v, 3*t*v*v, 3*t*t*v, t*t*t
		a, b := mul(left, b1), mul(right, b2)
		delta := sub(p[i], add(mul(c[0], b0+b1), mul(c[3], b2+b3)))
		c00 += dot(a, a)
		c01 += dot(a, b)
		c11 += dot(b, b)
		x0 += dot(a, delta)
		x1 += dot(b, delta)
	}
	det := c00*c11 - c01*c01
	l, r := 0.0, 0.0
	if math.Abs(det) > 1e-12 {
		l = (x0*c11 - x1*c01) / det
		r = (c00*x1 - c01*x0) / det
	}
	distance := length(sub(c[3], c[0]))
	if l < distance*1e-6 || r < distance*1e-6 || l > distance*3 || r > distance*3 {
		l, r = distance/3, distance/3
	}
	c[1], c[2] = add(c[0], mul(left, l)), add(c[3], mul(right, r))
	return c
}

func curveError(p []Point, u []float64, c cubic) (float64, int) {
	maximum, split := 0.0, len(p)/2
	for i := 1; i < len(p)-1; i++ {
		d := sub(evaluate(c, u[i]), p[i])
		if e := dot(d, d); e > maximum {
			maximum, split = e, i
		}
	}
	return maximum, split
}
