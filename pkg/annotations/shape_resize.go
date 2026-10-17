package annotations

import (
	"fmt"
	"math"
)

// ResizeShape regenerates control points for a requested artwork size while
// keeping stroke width, padding and arrowhead size in physical units.
func (d Data) ResizeShape(width, height float64) (Data, error) {
	if d.Kind != Shape || len(d.Points) != 2 || !finite(width) || !finite(height) || width <= 0 || height <= 0 {
		return Data{}, fmt.Errorf("Invalid shape size")
	}
	d = d.Clone()
	a, b := d.Points[0], d.Points[1]
	sx, sy := math.Copysign(1, b.X-a.X), math.Copysign(1, b.Y-a.Y)
	_, bounds, err := d.Artwork()
	if err != nil {
		return Data{}, err
	}
	x := math.Abs(b.X-a.X) * width / bounds.W()
	y := math.Abs(b.Y-a.Y) * height / bounds.H()
	measure := func(x, y float64) (float64, float64, error) {
		d.Points[1] = Point{a.X + sx*x, a.Y + sy*y}
		_, bounds, err := d.Artwork()
		return bounds.W(), bounds.H(), err
	}
	// Arrow wings depend on direction, and star extrema do not coincide with
	// their control rectangle. Fit regenerated bounds instead of stretching SVG.
	for range 16 {
		w, h, err := measure(x, y)
		if err != nil {
			return Data{}, err
		}
		dw, dh := width-w, height-h
		if math.Abs(dw) < .001 && math.Abs(dh) < .001 {
			return d, nil
		}
		const step = .01
		wx, hx, err := measure(x+step, y)
		if err != nil {
			return Data{}, err
		}
		wy, hy, err := measure(x, y+step)
		if err != nil {
			return Data{}, err
		}
		j00, j01, j10, j11 := (wx-w)/step, (wy-w)/step, (hx-h)/step, (hy-h)/step
		det := j00*j11 - j01*j10
		if math.Abs(det) < 1e-8 {
			break
		}
		x = math.Max(0, x+(dw*j11-dh*j01)/det)
		y = math.Max(0, y+(dh*j00-dw*j10)/det)
	}
	return Data{}, fmt.Errorf("The shape is too small for this size")
}
