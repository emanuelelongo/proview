package document

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	img "emanuelelongo/proview/pkg/images"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type pdfPoint struct{ x, y float64 }
type pdfGeometry struct {
	x, y, w, h float64
	rotation   int
}

func geometry(a *model.InheritedPageAttrs) pdfGeometry {
	b := a.CropBox
	if b == nil {
		b = a.MediaBox
	}
	return pdfGeometry{b.LL.X, b.LL.Y, b.Width(), b.Height(), (a.Rotate%360 + 360) % 360}
}
func (g pdfGeometry) size() (float64, float64) {
	if g.rotation%180 != 0 {
		return g.h, g.w
	}
	return g.w, g.h
}
func (g pdfGeometry) view(p pdfPoint) pdfPoint {
	x, y := p.x-g.x, p.y-g.y
	switch g.rotation {
	case 90:
		return pdfPoint{y, x}
	case 180:
		return pdfPoint{g.w - x, y}
	case 270:
		return pdfPoint{g.h - y, g.w - x}
	}
	return pdfPoint{x, g.h - y}
}
func (g pdfGeometry) user(p pdfPoint) pdfPoint {
	switch g.rotation {
	case 90:
		return pdfPoint{g.x + p.y, g.y + p.x}
	case 180:
		return pdfPoint{g.x + g.w - p.x, g.y + p.y}
	case 270:
		return pdfPoint{g.x + g.w - p.y, g.y + g.h - p.x}
	}
	return pdfPoint{g.x + p.x, g.y + g.h - p.y}
}
func number(o types.Object) (float64, bool) {
	switch v := o.(type) {
	case types.Float:
		return float64(v), true
	case types.Integer:
		return float64(v), true
	}
	return 0, false
}
func numbers(ctx *model.Context, o types.Object) ([]float64, error) {
	a, e := ctx.DereferenceArray(o)
	if e != nil {
		return nil, e
	}
	out := make([]float64, len(a))
	for i, v := range a {
		n, ok := number(v)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("invalid PDF annotation number")
		}
		out[i] = n
	}
	return out, nil
}
func pdfArray(v ...float64) types.Array {
	a := make(types.Array, len(v))
	for i, n := range v {
		a[i] = types.Float(n)
	}
	return a
}
func writePDFContext(ctx *model.Context) ([]byte, error) {
	if e := api.ValidateContext(context.Background(), ctx); e != nil {
		return nil, e
	}
	var b bytes.Buffer
	e := api.WriteContext(context.Background(), ctx, &b)
	return b.Bytes(), e
}

// Copy an appearance's resource graph, preserving shared/cyclic references. The
// destination owns its objects; no references into another PDF are retained.
type pdfGraft struct {
	from, to *model.Context
	refs     map[int]types.IndirectRef
	count    int
}

func (g *pdfGraft) copy(o types.Object, depth int) (types.Object, error) {
	if depth > 100 || g.count > 100000 {
		return nil, fmt.Errorf("PDF appearance resource graph is too large")
	}
	g.count++
	switch v := o.(type) {
	case types.IndirectRef:
		id := v.ObjectNumber.Value()
		if r, ok := g.refs[id]; ok {
			return r, nil
		}
		r, e := g.to.IndRefForNewObject(types.Dict{})
		if e != nil {
			return nil, e
		}
		g.refs[id] = *r
		value, e := g.from.Dereference(v)
		if e != nil {
			return nil, e
		}
		c, e := g.copy(value, depth+1)
		if e != nil {
			return nil, e
		}
		g.to.Table[r.ObjectNumber.Value()].Object = c
		return *r, nil
	case types.Dict:
		d := types.Dict{}
		for k, value := range v {
			c, e := g.copy(value, depth+1)
			if e != nil {
				return nil, e
			}
			d[k] = c
		}
		return d, nil
	case types.Array:
		a := make(types.Array, len(v))
		for i, value := range v {
			c, e := g.copy(value, depth+1)
			if e != nil {
				return nil, e
			}
			a[i] = c
		}
		return a, nil
	case types.StreamDict:
		c, e := g.copy(v.Dict, depth+1)
		if e != nil {
			return nil, e
		}
		v.Dict = c.(types.Dict)
		v.Raw = bytes.Clone(v.Raw)
		v.Content = bytes.Clone(v.Content)
		return v, nil
	default:
		return o, nil
	}
}
func newGraft(from, to *model.Context) *pdfGraft {
	return &pdfGraft{from: from, to: to, refs: map[int]types.IndirectRef{}}
}
func addPDFStream(ctx *model.Context, data []byte, d types.Dict) (*types.IndirectRef, error) {
	s, e := ctx.NewStreamDictForBuf(data)
	if e != nil {
		return nil, e
	}
	for k, v := range d {
		s.Dict[k] = v
	}
	if e = s.Encode(); e != nil {
		return nil, e
	}
	return ctx.IndRefForNewObject(*s)
}
func transformUnit(p pdfPoint, t img.Transform) pdfPoint {
	if t.FlipX {
		p.x = 1 - p.x
	}
	if t.FlipY {
		p.y = 1 - p.y
	}
	switch t.Turns % 4 {
	case 1:
		p = pdfPoint{1 - p.y, p.x}
	case 2:
		p = pdfPoint{1 - p.x, 1 - p.y}
	case 3:
		p = pdfPoint{p.y, 1 - p.x}
	}
	return p
}
func matrixFor(f func(pdfPoint) pdfPoint) string {
	o, x, y := f(pdfPoint{}), f(pdfPoint{1, 0}), f(pdfPoint{0, 1})
	return fmt.Sprintf("%.10f %.10f %.10f %.10f %.10f %.10f cm\n", x.x-o.x, x.y-o.y, y.x-o.x, y.y-o.y, o.x, o.y)
}

// Appearance pages use normal PDF operators and resources, never application
// payloads. Normalize page boxes/rotation before fitting them to an annotation.
func formFromPDF(to *model.Context, data []byte, t img.Transform) (*types.IndirectRef, float64, float64, error) {
	from, e := readPDFObjects(data)
	if e != nil {
		return nil, 0, 0, e
	}
	p, _, attrs, e := from.PageDict(context.Background(), 1, false)
	if e != nil {
		return nil, 0, 0, e
	}
	content, e := from.PageContent(p, 1)
	if e != nil {
		return nil, 0, 0, e
	}
	geom := geometry(attrs)
	w, h := geom.size()
	ow, oh := w, h
	if t.Turns%2 != 0 {
		ow, oh = h, w
	}
	m := matrixFor(func(p pdfPoint) pdfPoint {
		v := geom.view(p)
		v = transformUnit(pdfPoint{v.x / w, v.y / h}, t)
		return pdfPoint{v.x * ow, (1 - v.y) * oh}
	})

	// Unwrap our isolated appearance page instead of adding another nested Form
	// at every save/reopen cycle. All retained content remains standard PDF.
	tokens := strings.Fields(string(content))
	if len(tokens) == 11 && tokens[0] == "q" && tokens[7] == "cm" && tokens[8] == "/A" && tokens[9] == "Do" && tokens[10] == "Q" {
		matrix := make([]float64, 6)
		valid := true
		for i := range matrix {
			matrix[i], e = strconv.ParseFloat(tokens[i+1], 64)
			if e != nil {
				valid = false
			}
		}
		xobjects, _ := from.DereferenceDict(attrs.Resources["XObject"])
		if valid && xobjects["A"] != nil {
			copied, e := newGraft(from, to).copy(xobjects["A"], 0)
			if e != nil {
				return nil, 0, 0, e
			}
			ref, ok := copied.(types.IndirectRef)
			if ok {
				obj := to.Table[ref.ObjectNumber.Value()].Object
				if form, ok := obj.(types.StreamDict); ok {
					prior, e := pdfMatrix(to, form.Dict["Matrix"])
					if e != nil {
						return nil, 0, 0, e
					}
					outer := make([]float64, 6)
					fields := strings.Fields(m)
					for i := range outer {
						outer[i], _ = strconv.ParseFloat(fields[i], 64)
					}
					form.Dict["Matrix"] = pdfArray(multiplyPDFMatrix(outer, multiplyPDFMatrix(matrix, prior))...)
					to.Table[ref.ObjectNumber.Value()].Object = form
					return &ref, ow, oh, nil
				}
			}
		}
	}
	resources, e := newGraft(from, to).copy(attrs.Resources, 0)
	if e != nil {
		return nil, 0, 0, e
	}
	ref, e := addPDFStream(to, append(append([]byte("q\n"+m), content...), []byte("\nQ")...), types.Dict{"Type": types.Name("XObject"), "Subtype": types.Name("Form"), "FormType": types.Integer(1), "BBox": pdfArray(0, 0, ow, oh), "Resources": resources})
	return ref, ow, oh, e
}
func normalizedAppearance(data []byte, t img.Transform) ([]byte, error) {
	ctx, e := newAppearanceContext(100, 100)
	if e != nil {
		return nil, e
	}
	ref, w, h, e := formFromPDF(ctx, data, t)
	if e != nil {
		return nil, e
	}
	p, _, _, e := ctx.PageDict(context.Background(), 1, false)
	if e != nil {
		return nil, e
	}
	p["MediaBox"] = pdfArray(0, 0, w, h)
	p["Resources"] = types.Dict{"XObject": types.Dict{"A": *ref}}
	stream, e := addPDFStream(ctx, []byte("/A Do"), nil)
	if e != nil {
		return nil, e
	}
	p["Contents"] = *stream
	return writePDFContext(ctx)
}

// Preserve the standard appearance exactly on import. It is replaced only when
// an editable property changes; moving an object merely changes its rectangle.
func annotationAppearance(from *model.Context, annot types.Dict, rect []float64, geom pdfGeometry) ([]byte, error) {
	ap, e := from.DereferenceDict(annot["AP"])
	if e != nil || ap == nil {
		return nil, fmt.Errorf("missing annotation appearance")
	}
	obj, e := from.Dereference(ap["N"])
	if e != nil {
		return nil, e
	}
	stream, ok := obj.(types.StreamDict)
	if !ok {
		return nil, fmt.Errorf("unsupported annotation appearance state")
	}
	box, e := numbers(from, stream.Dict["BBox"])
	if e != nil || len(box) != 4 {
		return nil, fmt.Errorf("invalid appearance box")
	}
	m := []float64{1, 0, 0, 1, 0, 0}
	if stream.Dict["Matrix"] != nil {
		m, e = numbers(from, stream.Dict["Matrix"])
		if e != nil || len(m) != 6 {
			return nil, fmt.Errorf("invalid appearance matrix")
		}
	}
	apply := func(p pdfPoint) pdfPoint { return pdfPoint{m[0]*p.x + m[2]*p.y + m[4], m[1]*p.x + m[3]*p.y + m[5]} }
	lo, hi := pdfPoint{math.Inf(1), math.Inf(1)}, pdfPoint{math.Inf(-1), math.Inf(-1)}
	for _, p := range []pdfPoint{{box[0], box[1]}, {box[0], box[3]}, {box[2], box[1]}, {box[2], box[3]}} {
		q := apply(p)
		lo.x = math.Min(lo.x, q.x)
		lo.y = math.Min(lo.y, q.y)
		hi.x = math.Max(hi.x, q.x)
		hi.y = math.Max(hi.y, q.y)
	}
	if hi.x <= lo.x || hi.y <= lo.y {
		return nil, fmt.Errorf("empty appearance box")
	}
	a, b := geom.view(pdfPoint{rect[0], rect[1]}), geom.view(pdfPoint{rect[2], rect[3]})
	x, y := math.Min(a.x, b.x), math.Min(a.y, b.y)
	w, h := math.Abs(a.x-b.x), math.Abs(a.y-b.y)
	ctx, e := newAppearanceContext(w, h)
	if e != nil {
		return nil, e
	}
	// The form's own Matrix is applied by Do; fit its transformed bounds into Rect.
	wrap := matrixFor(func(p pdfPoint) pdfPoint {
		u := pdfPoint{rect[0] + (p.x-lo.x)*(rect[2]-rect[0])/(hi.x-lo.x), rect[1] + (p.y-lo.y)*(rect[3]-rect[1])/(hi.y-lo.y)}
		v := geom.view(u)
		return pdfPoint{v.x - x, h - (v.y - y)}
	})
	copied, e := newGraft(from, ctx).copy(ap["N"], 0)
	if e != nil {
		return nil, e
	}
	p, _, _, e := ctx.PageDict(context.Background(), 1, false)
	if e != nil {
		return nil, e
	}
	p["Resources"] = types.Dict{"XObject": types.Dict{"A": copied}}
	c, e := addPDFStream(ctx, []byte("q\n"+wrap+"/A Do\nQ"), nil)
	if e != nil {
		return nil, e
	}
	p["Contents"] = *c
	return writePDFContext(ctx)
}

func newAppearanceContext(w, h float64) (*model.Context, error) {
	ctx, e := pdfcpu.CreateContextWithXRefTable(nil, &types.Dim{Width: w, Height: h})
	if e != nil {
		return nil, e
	}
	root, e := ctx.Catalog()
	if e != nil {
		return nil, e
	}
	parent := root["Pages"]
	pages, e := ctx.DereferenceDict(parent)
	if e != nil {
		return nil, e
	}
	ref, e := ctx.IndRefForNewObject(types.Dict{"Type": types.Name("Page"), "Parent": parent, "MediaBox": pdfArray(0, 0, w, h), "Resources": types.Dict{}})
	if e != nil {
		return nil, e
	}
	pages["Kids"] = types.Array{*ref}
	pages["Count"] = types.Integer(1)
	ctx.PageCount = 1
	return ctx, nil
}

func readPDFObjects(data []byte) (*model.Context, error) {
	ctx, e := api.ReadContext(context.Background(), bytes.NewReader(data), nil)
	if e == nil {
		e = ctx.EnsurePageCount()
	}
	return ctx, e
}

func pdfMatrix(ctx *model.Context, o types.Object) ([]float64, error) {
	if o == nil {
		return []float64{1, 0, 0, 1, 0, 0}, nil
	}
	v, e := numbers(ctx, o)
	if e != nil || len(v) != 6 {
		return nil, fmt.Errorf("invalid PDF matrix")
	}
	return v, nil
}
func multiplyPDFMatrix(a, b []float64) []float64 {
	return []float64{a[0]*b[0] + a[2]*b[1], a[1]*b[0] + a[3]*b[1], a[0]*b[2] + a[2]*b[3], a[1]*b[2] + a[3]*b[3], a[0]*b[4] + a[2]*b[5] + a[4], a[1]*b[4] + a[3]*b[5] + a[5]}
}

func inversePDFTransform(t img.Transform) img.Transform {
	for turns := 0; turns < 4; turns++ {
		for _, flip := range []bool{false, true} {
			candidate := img.Transform{Turns: turns, FlipX: flip}
			ok := true
			for _, p := range []pdfPoint{{0, 0}, {1, 0}, {0, 1}} {
				q := transformUnit(transformUnit(p, t), candidate)
				if math.Hypot(q.x-p.x, q.y-p.y) > .0001 {
					ok = false
				}
			}
			if ok {
				return candidate
			}
		}
	}
	return img.Transform{}
}

// A whole-appearance affine wrapper carries standard geometric transformations
// even though FreeText has no separate mirror property. Recognize its orientation
// without interpreting glyph outlines or storing application-specific metadata.
func textAppearanceTransform(ctx *model.Context, a types.Dict, g pdfGeometry, fallback img.Transform) img.Transform {
	ap, _ := ctx.DereferenceDict(a["AP"])
	if ap == nil {
		return fallback
	}
	o, e := ctx.Dereference(ap["N"])
	if e != nil {
		return fallback
	}
	form, ok := o.(types.StreamDict)
	if !ok || form.Decode() != nil {
		return fallback
	}
	fields := strings.Fields(string(form.Content))
	if len(fields) < 9 || fields[0] != "q" || fields[7] != "cm" || fields[len(fields)-1] != "Q" {
		return fallback
	}
	prefix := make([]float64, 6)
	for i := range prefix {
		prefix[i], e = strconv.ParseFloat(fields[i+1], 64)
		if e != nil {
			return fallback
		}
	}
	matrix, e := pdfMatrix(ctx, form.Dict["Matrix"])
	if e != nil {
		return fallback
	}
	m := multiplyPDFMatrix(matrix, prefix)
	view := func(p pdfPoint) pdfPoint {
		return g.view(pdfPoint{m[0]*p.x + m[2]*p.y + m[4], m[1]*p.x + m[3]*p.y + m[5]})
	}
	o0, x, y := view(pdfPoint{}), view(pdfPoint{1, 0}), view(pdfPoint{0, -1})
	vx, vy := pdfPoint{x.x - o0.x, x.y - o0.y}, pdfPoint{y.x - o0.x, y.y - o0.y}
	sx, sy := math.Hypot(vx.x, vx.y), math.Hypot(vy.x, vy.y)
	if sx < 1e-10 || sy < 1e-10 {
		return fallback
	}
	vx.x /= sx
	vx.y /= sx
	vy.x /= sy
	vy.y /= sy
	for turns := 0; turns < 4; turns++ {
		for _, flip := range []bool{false, true} {
			t := img.Transform{Turns: turns, FlipX: flip}
			o := transformUnit(pdfPoint{}, t)
			x, y := transformUnit(pdfPoint{1, 0}, t), transformUnit(pdfPoint{0, 1}, t)
			if math.Hypot(x.x-o.x-vx.x, x.y-o.y-vx.y) < .0001 && math.Hypot(y.x-o.x-vy.x, y.y-o.y-vy.y) < .0001 {
				return t
			}
		}
	}
	return fallback
}
func defaultAppearanceFontName(ctx *model.Context, a types.Dict, name string) string {
	ap, _ := ctx.DereferenceDict(a["AP"])
	if ap != nil {
		object, _ := ctx.Dereference(ap["N"])
		if form, ok := object.(types.StreamDict); ok {
			resources, _ := ctx.DereferenceDict(form.Dict["Resources"])
			fonts, _ := ctx.DereferenceDict(resources["Font"])
			font, _ := ctx.DereferenceDict(fonts[name])
			if font != nil {
				return annotationName(font, "BaseFont")
			}
		}
	}
	lower := strings.ToLower(name)
	if strings.Contains(lower, "helv") || strings.Contains(lower, "times") || strings.Contains(lower, "courier") {
		return name
	}
	return ""
}
