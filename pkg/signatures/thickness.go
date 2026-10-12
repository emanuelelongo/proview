package signatures

import (
	vector "github.com/tdewolff/canvas"
)

// Thickness expands imported outlines relative to a 1024-pixel preview. Original
// SVG bytes remain unchanged, so returning to zero restores the exact artwork.
func (a Asset) WithThickness(width float64) (Asset, error) {
	a = a.Clone()
	a.Thickness = width
	return a, a.Validate()
}

func (a Asset) styledCanvas() (*vector.Canvas, error) {
	c, err := parseSVG(a.Data)
	if err != nil || a.Thickness == 0 {
		return c, err
	}
	w, h := c.Size()
	out := vector.New(w, h)
	c.RenderTo(expandedCanvas{Canvas: out, amount: a.Thickness * max(w, h) / 1024})
	return out, nil
}

type expandedCanvas struct {
	*vector.Canvas
	amount float64
}

func (r expandedCanvas) RenderPath(path *vector.Path, style vector.Style, m vector.Matrix) {
	if style.HasFill() {
		fill := style
		fill.Stroke = vector.Paint{}
		fill.FillRule = vector.NonZero
		outline := path.Settle(style.FillRule).Offset(r.amount, max(r.amount/10, 0.001))
		r.Canvas.RenderPath(outline, fill, m)
	}
	if style.HasStroke() {
		style.Fill = vector.Paint{}
		style.StrokeWidth += 2 * r.amount
		r.Canvas.RenderPath(path, style, m)
	}
}
