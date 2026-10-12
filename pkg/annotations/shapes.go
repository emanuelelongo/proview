package annotations

import (
	"emanuelelongo/proview/pkg/signatures"
	"fmt"
	vector "github.com/tdewolff/canvas"
	"math"
)

type ShapeKind string

const (
	Rectangle ShapeKind = "Rectangle"
	Oval      ShapeKind = "Oval"
	Callout   ShapeKind = "Callout"
	Arrow     ShapeKind = "Arrow"
	Star      ShapeKind = "Star"
	Magnifier ShapeKind = "Magnifier"
)

var Shapes = []string{string(Rectangle), string(Oval), string(Callout), string(Arrow), string(Star), string(Magnifier)}

func (d Data) shapeArtwork() (signatures.Asset, vector.Rect, error) {
	if len(d.Points) != 2 {
		return signatures.Asset{}, vector.Rect{}, fmt.Errorf("Drag to create a shape")
	}
	a, b := d.Points[0], d.Points[1]
	x, y := math.Min(a.X, b.X), math.Min(a.Y, b.Y)
	w, h := math.Abs(b.X-a.X), math.Abs(b.Y-a.Y)
	if d.Shape == Arrow {
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
		shaft := math.Min(d.Width*1.5, l*.1)
		head := math.Min(math.Max(d.Width*4, l*.22), l*.45)
		path = &vector.Path{}
		pts := [][2]float64{{a.X - x + nx*shaft/2, a.Y - y + ny*shaft/2}, {b.X - x - ux*head + nx*shaft/2, b.Y - y - uy*head + ny*shaft/2}, {b.X - x - ux*head + nx*head/2, b.Y - y - uy*head + ny*head/2}, {b.X - x, b.Y - y}, {b.X - x - ux*head - nx*head/2, b.Y - y - uy*head - ny*head/2}, {b.X - x - ux*head - nx*shaft/2, b.Y - y - uy*head - ny*shaft/2}, {a.X - x - nx*shaft/2, a.Y - y - ny*shaft/2}}
		for i, p := range pts {
			if i == 0 {
				path.MoveTo(p[0], p[1])
			} else {
				path.LineTo(p[0], p[1])
			}
		}
		path.Close()
	default:
		return signatures.Asset{}, vector.Rect{}, fmt.Errorf("Unknown shape")
	}
	path = path.Translate(x, y)
	if !d.Filled || d.Shape == Magnifier {
		if d.Shape != Magnifier {
			path = d.dashPath(path)
		}
		path = path.Stroke(d.Width, vector.RoundCap, vector.RoundJoin, .01)
	}
	// Bounds include arrow wings and keep the complete outline inside its object.
	bounds = path.Bounds().Expand(d.Padding)
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="%.8f %.8f %.8f %.8f"><path fill="#%02x%02x%02x" d="%s"/></svg>`, bounds.X0, bounds.Y0, bounds.W(), bounds.H(), d.Color.R, d.Color.G, d.Color.B, path.ToSVG())
	return signatures.Asset{Kind: signatures.SVG, Data: []byte(svg)}, bounds, nil
}
