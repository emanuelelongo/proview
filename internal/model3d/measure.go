package model3d

import (
	"image"
	"math"
)

// Unit describes how one coordinate in a unitless STL should be interpreted.
type Unit string

const (
	UnitModel      Unit = "model"
	UnitMillimeter Unit = "mm"
	UnitCentimeter Unit = "cm"
	UnitMeter      Unit = "m"
	UnitInch       Unit = "in"
)

func (u Unit) Label() string {
	if u == UnitModel || u == "" {
		return "units"
	}
	return string(u)
}

func (u Unit) MillimetersPerUnit() float64 {
	switch u {
	case UnitCentimeter:
		return 10
	case UnitMeter:
		return 1000
	case UnitInch:
		return 25.4
	default:
		return 1
	}
}

func Distance(a, b Vector) float64 {
	d := a.Sub(b)
	return math.Hypot(math.Hypot(d.X, d.Y), d.Z)
}

func modelSpan(m *Mesh) float64 {
	s := m.Size()
	return math.Max(s.X, math.Max(s.Y, s.Z))
}

// Project maps an original model coordinate onto the same orthographic frame
// used by the CPU and GPU renderers. The returned Z grows toward the camera.
func Project(m *Mesh, c Camera, point Vector, w, h int) Vector {
	scale := float64(min(w, h)) * .29 * math.Max(.05, math.Min(c.Zoom, 30))
	v := c.rotate(m.normalized(point))
	return Vector{float64(w)/2 + v.X*scale + c.PanX*float64(w),
		float64(h)/2 - v.Y*scale + c.PanY*float64(h), v.Z}
}

func (c Camera) unrotate(v Vector) Vector {
	sy, cy := math.Sincos(c.Yaw)
	sp, cp := math.Sincos(c.Pitch)
	y := -sp*v.Y + cp*v.Z
	return Vector{cy*v.X + sy*y, -sy*v.X + cy*y, cp*v.Y + sp*v.Z}
}

// Pick returns the nearest visible point on the mesh at a frame pixel. It does
// not depend on which renderer produced the displayed frame. Call off the UI
// thread for large meshes.
func Pick(m *Mesh, c Camera, w, h int, x, y float64) (Vector, bool) {
	if m == nil || w < 1 || h < 1 || x < 0 || y < 0 || x >= float64(w) || y >= float64(h) {
		return Vector{}, false
	}
	span := modelSpan(m)
	if span <= 0 || !finite(span) {
		return Vector{}, false
	}
	scale := float64(min(w, h)) * .29 * math.Max(.05, math.Min(c.Zoom, 30))
	viewX := (x - float64(w)/2 - c.PanX*float64(w)) / scale
	viewY := (float64(h)/2 - y + c.PanY*float64(h)) / scale
	center := Vector{(m.Min.X + m.Max.X) / 2, (m.Min.Y + m.Max.Y) / 2, (m.Min.Z + m.Max.Z) / 2}
	fromView := c.unrotate(Vector{viewX, viewY, 4})
	origin := Vector{center.X + fromView.X*span/2, center.Y + fromView.Y*span/2, center.Z + fromView.Z*span/2}
	dir := c.unrotate(Vector{0, 0, -1}).Unit()
	nearest := math.Inf(1)
	var point Vector
	for _, triangle := range m.Triangles {
		a, b, d := triangle.Vertices[0], triangle.Vertices[1], triangle.Vertices[2]
		e1, e2 := b.Sub(a), d.Sub(a)
		p := dir.Cross(e2)
		det := e1.Dot(p)
		// Compare with the triangle's own scale: a small feature can be
		// pickable even when another part of the model makes span large.
		if math.Abs(det) < 1e-12*math.Sqrt(e1.Dot(e1)*e2.Dot(e2)) {
			continue
		}
		inv := 1 / det
		t := origin.Sub(a)
		u := t.Dot(p) * inv
		if u < -1e-9 || u > 1+1e-9 {
			continue
		}
		q := t.Cross(e1)
		v := dir.Dot(q) * inv
		if v < -1e-9 || u+v > 1+1e-9 {
			continue
		}
		hit := e2.Dot(q) * inv
		if hit >= 0 && hit < nearest {
			nearest = hit
			point = Vector{origin.X + dir.X*hit, origin.Y + dir.Y*hit, origin.Z + dir.Z*hit}
		}
	}
	return point, !math.IsInf(nearest, 1)
}

// GridStep chooses 1/2/5 spacing at a readable screen pitch. For physical
// units it uses millimeters; unitless models remain in source coordinates.
func GridStep(m *Mesh, c Camera, w, h int, unit Unit) (float64, string) {
	span := modelSpan(m)
	if span <= 0 || w < 1 || h < 1 {
		return 0, ""
	}
	pixelsPerUnit := 2 * float64(min(w, h)) * .29 * math.Max(.05, math.Min(c.Zoom, 30)) / span
	physical := 45 / pixelsPerUnit * unit.MillimetersPerUnit()
	base := math.Pow(10, math.Floor(math.Log10(physical)))
	step := base
	for _, factor := range []float64{1, 2, 5, 10} {
		step = factor * base
		if step >= physical {
			break
		}
	}
	if unit == UnitModel || unit == "" {
		return step, "units"
	}
	return step / unit.MillimetersPerUnit(), "mm"
}

// DrawGrid paints a reference plane behind visible mesh pixels. It works on
// either CPU or GPU frames without changing the renderer's depth behavior.
func DrawGrid(frame *image.RGBA, m *Mesh, c Camera, unit Unit, view string) {
	if frame == nil || m == nil {
		return
	}
	w, h := frame.Bounds().Dx(), frame.Bounds().Dy()
	step, _ := GridStep(m, c, w, h, unit)
	if step <= 0 || !finite(step) {
		return
	}
	span := modelSpan(m)
	margin := math.Max(step, span*.1)
	type axis struct{ lo, hi float64 }
	var a, b axis
	point := func(x, y float64) Vector { return Vector{x, y, m.Min.Z} }
	switch view {
	case "Front", "Rear":
		a, b = axis{m.Min.X - margin, m.Max.X + margin}, axis{m.Min.Z - margin, m.Max.Z + margin}
		fixed := m.Min.Y
		if view == "Rear" {
			fixed = m.Max.Y
		}
		point = func(x, y float64) Vector { return Vector{x, fixed, y} }
	case "Left", "Right":
		a, b = axis{m.Min.Y - margin, m.Max.Y + margin}, axis{m.Min.Z - margin, m.Max.Z + margin}
		fixed := m.Min.X
		if view == "Left" {
			fixed = m.Max.X
		}
		point = func(x, y float64) Vector { return Vector{fixed, x, y} }
	default:
		a, b = axis{m.Min.X - margin, m.Max.X + margin}, axis{m.Min.Y - margin, m.Max.Y + margin}
		fixed := m.Min.Z
		if view == "Bottom" {
			fixed = m.Max.Z
		}
		point = func(x, y float64) Vector { return Vector{x, y, fixed} }
	}
	draw := func(p, q Vector, major bool) {
		color := [3]byte{59, 69, 78}
		if major {
			color = [3]byte{78, 91, 100}
		}
		gridLine(frame, Project(m, c, p, w, h), Project(m, c, q, w, h), color)
	}
	for i := math.Ceil(a.lo / step); i <= math.Floor(a.hi/step) && i < math.Ceil(a.lo/step)+200; i++ {
		draw(point(i*step, b.lo), point(i*step, b.hi), math.Mod(math.Abs(i), 5) == 0)
	}
	for i := math.Ceil(b.lo / step); i <= math.Floor(b.hi/step) && i < math.Ceil(b.lo/step)+200; i++ {
		draw(point(a.lo, i*step), point(a.hi, i*step), math.Mod(math.Abs(i), 5) == 0)
	}
}

func gridLine(frame *image.RGBA, a, b Vector, color [3]byte) {
	w, h := frame.Bounds().Dx(), frame.Bounds().Dy()
	dx, dy := b.X-a.X, b.Y-a.Y
	lo, hi := 0., 1.
	for _, bound := range [][2]float64{{-dx, a.X}, {dx, float64(w-1) - a.X}, {-dy, a.Y}, {dy, float64(h-1) - a.Y}} {
		p, q := bound[0], bound[1]
		if p == 0 {
			if q < 0 {
				return
			}
			continue
		}
		r := q / p
		if p < 0 {
			lo = math.Max(lo, r)
		} else {
			hi = math.Min(hi, r)
		}
		if lo > hi {
			return
		}
	}
	steps := max(1, int(math.Ceil(math.Max(math.Abs(dx*(hi-lo)), math.Abs(dy*(hi-lo))))))
	for i := 0; i <= steps; i++ {
		t := lo + (hi-lo)*float64(i)/float64(steps)
		x, y := int(math.Round(a.X+t*dx)), int(math.Round(a.Y+t*dy))
		if x < 0 || y < 0 || x >= w || y >= h {
			continue
		}
		j := y*frame.Stride + x*4
		if frame.Pix[j] == BackgroundColor.R && frame.Pix[j+1] == BackgroundColor.G && frame.Pix[j+2] == BackgroundColor.B {
			frame.Pix[j], frame.Pix[j+1], frame.Pix[j+2] = color[0], color[1], color[2]
		}
	}
}
