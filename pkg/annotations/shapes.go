package annotations

import (
	"emanuelelongo/proview/pkg/signatures"
	"fmt"
	vector "github.com/tdewolff/canvas"
	"math"
)

type ShapeKind string

const (
	Polygon   ShapeKind = "Polygon"
	Rectangle ShapeKind = "Rectangle"
	Oval      ShapeKind = "Oval"
	Callout   ShapeKind = "Callout"
	Arrow     ShapeKind = "Arrow"
	Line      ShapeKind = "Line"
	Star      ShapeKind = "Star"
	Magnifier ShapeKind = "Magnifier"
)

var Shapes = []string{string(Rectangle), string(Oval), string(Callout), string(Line), string(Arrow), string(Star), string(Magnifier)}

// ArrowHeadLength is a fixed physical size, independent of shaft length.
const ArrowHeadLength = 12.

func (d Data) shapeArtwork() (signatures.Asset, vector.Rect, error) {
	if d.Shape == Polygon {
		return d.polygonArtwork()
	}
	if len(d.Points) != 2 {
		return signatures.Asset{}, vector.Rect{}, fmt.Errorf("Drag to create a shape")
	}
	a, b := d.Points[0], d.Points[1]
	x, y := math.Min(a.X, b.X), math.Min(a.Y, b.Y)
	w, h := math.Abs(b.X-a.X), math.Abs(b.Y-a.Y)
	if d.Shape == Arrow || d.Shape == Line {
		if math.Hypot(b.X-a.X, b.Y-a.Y) < .1 {
			return signatures.Asset{}, vector.Rect{}, fmt.Errorf("The shape is too small")
		}
		w = math.Max(w, .1)
		h = math.Max(h, .1)
	}
	if w < .1 || h < .1 {
		return signatures.Asset{}, vector.Rect{}, fmt.Errorf("The shape is too small")
	}
	pad := d.Width/2 + d.Padding
	bounds := vector.Rect{X0: x - pad, Y0: y - pad, X1: x + w + pad, Y1: y + h + pad}
	var path *vector.Path
	switch d.Shape {
	case Rectangle:
		path = vector.Rectangle(w, h)
	case Oval, Magnifier:
		path = vector.Ellipse(w/2, h/2).Translate(w/2, h/2)
	case Callout:
		path = &vector.Path{}
		path.MoveTo(0, 0)
		path.LineTo(w, 0)
		path.LineTo(w, h*.72)
		path.LineTo(w*.42, h*.72)
		path.LineTo(w*.2, h)
		path.LineTo(w*.2, h*.72)
		path.LineTo(0, h*.72)
		path.Close()
	case Star:
		path = &vector.Path{}
		for i := 0; i < 10; i++ {
			angle := -math.Pi/2 + float64(i)*math.Pi/5
			r := .5
			if i%2 != 0 {
				r = .22
			}
			px, py := w/2+math.Cos(angle)*w*r, h/2+math.Sin(angle)*h*r
			if i == 0 {
				path.MoveTo(px, py)
			} else {
				path.LineTo(px, py)
			}
		}
		path.Close()
	case Arrow:
		dx, dy := b.X-a.X, b.Y-a.Y
		l := math.Hypot(dx, dy)
		ux, uy := dx/l, dy/l
		nx, ny := -uy, ux
		head := ArrowHeadLength
		path = &vector.Path{}
		path.MoveTo(a.X-x, a.Y-y)
		path.LineTo(b.X-x, b.Y-y)
		path.MoveTo(b.X-x-ux*head+nx*head/2, b.Y-y-uy*head+ny*head/2)
		path.LineTo(b.X-x, b.Y-y)
		path.LineTo(b.X-x-ux*head-nx*head/2, b.Y-y-uy*head-ny*head/2)
		if d.ArrowClosed {
			// A closed triangular head is a separate filled path; the shaft
			// keeps its normal stroke width and never becomes an outline.
			shaft := (&vector.Path{})
			shaft.MoveTo(a.X-x, a.Y-y)
			shaft.LineTo(b.X-x, b.Y-y)
			headPath := &vector.Path{}
			headPath.MoveTo(b.X-x, b.Y-y)
			headPath.LineTo(b.X-x-ux*head+nx*head/2, b.Y-y-uy*head+ny*head/2)
			headPath.LineTo(b.X-x-ux*head-nx*head/2, b.Y-y-uy*head-ny*head/2)
			headPath.Close()
			shaft = d.dashPath(shaft).Stroke(d.Width, vector.RoundCap, vector.RoundJoin, .01)
			combined := shaft.Append(headPath).Translate(x, y)
			bounds = combined.Bounds().Expand(d.Padding)
			svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="%.8f %.8f %.8f %.8f"><path fill="#%02x%02x%02x" d="%s"/></svg>`, bounds.X0, bounds.Y0, bounds.W(), bounds.H(), d.Color.R, d.Color.G, d.Color.B, combined.ToSVG())
			return signatures.Asset{Kind: signatures.SVG, Data: []byte(svg)}, bounds, nil
		}
	case Line:
		path = &vector.Path{}
		path.MoveTo(a.X-x, a.Y-y)
		path.LineTo(b.X-x, b.Y-y)
	default:
		return signatures.Asset{}, vector.Rect{}, fmt.Errorf("Unknown shape")
	}
	path = path.Translate(x, y)
	// Pattern gaps must not move the object's frame or destabilize resize fits.
	frame := path
	if !d.Filled || d.Shape == Magnifier || d.Shape == Arrow || d.Shape == Line {
		frame = path.Stroke(d.Width, vector.RoundCap, vector.RoundJoin, .01)
		if d.Shape != Magnifier {
			path = d.dashPath(path)
		}
		path = path.Stroke(d.Width, vector.RoundCap, vector.RoundJoin, .01)
	}
	// Bounds include arrow wings and keep the complete outline inside its object.
	bounds = frame.Bounds().Expand(d.Padding)
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="%.8f %.8f %.8f %.8f"><path fill="#%02x%02x%02x" d="%s"/></svg>`, bounds.X0, bounds.Y0, bounds.W(), bounds.H(), d.Color.R, d.Color.G, d.Color.B, path.ToSVG())
	return signatures.Asset{Kind: signatures.SVG, Data: []byte(svg)}, bounds, nil
}

// PolygonVertices uses standard PDF geometry for shapes without a dedicated type.
func (d Data) PolygonVertices() []Point {
	if d.Shape == Polygon {
		return append([]Point(nil), d.Points...)
	}
	a, b := d.Points[0], d.Points[1]
	x, y := math.Min(a.X, b.X), math.Min(a.Y, b.Y)
	w, h := math.Abs(b.X-a.X), math.Abs(b.Y-a.Y)
	var points []Point
	if d.Shape == Star {
		for i := 0; i < 10; i++ {
			angle := -math.Pi/2 + float64(i)*math.Pi/5
			r := .5
			if i%2 != 0 {
				r = .22
			}
			points = append(points, Point{x + w/2 + math.Cos(angle)*w*r, y + h/2 + math.Sin(angle)*h*r})
		}
		return points
	}
	for _, p := range []Point{{0, 0}, {1, 0}, {1, .72}, {.42, .72}, {.2, 1}, {.2, .72}, {0, .72}} {
		points = append(points, Point{x + p.X*w, y + p.Y*h})
	}
	return points
}
func (d Data) polygonArtwork() (signatures.Asset, vector.Rect, error) {
	if len(d.Points) < 3 {
		return signatures.Asset{}, vector.Rect{}, fmt.Errorf("Invalid polygon")
	}
	path := &vector.Path{}
	for i, p := range d.Points {
		if i == 0 {
			path.MoveTo(p.X, p.Y)
		} else {
			path.LineTo(p.X, p.Y)
		}
	}
	path.Close()
	frame := path
	if !d.Filled {
		frame = path.Stroke(d.Width, vector.RoundCap, vector.RoundJoin, .01)
		path = d.dashPath(path).Stroke(d.Width, vector.RoundCap, vector.RoundJoin, .01)
	}
	b := frame.Bounds().Expand(d.Padding)
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="%.8f %.8f %.8f %.8f"><path fill="#%02x%02x%02x" d="%s"/></svg>`, b.X0, b.Y0, b.W(), b.H(), d.Color.R, d.Color.G, d.Color.B, path.ToSVG())
	return signatures.Asset{Kind: signatures.SVG, Data: []byte(svg)}, b, nil
}
