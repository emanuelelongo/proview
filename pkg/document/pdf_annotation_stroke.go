package document

import (
	"context"
	"image/color"
	"math"
	"regexp"
	"strconv"
	"strings"

	"emanuelelongo/proview/pkg/annotations"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

var strokeTokens = regexp.MustCompile(`/[^\s\[\]<>/()]+|[-+]?(?:\d*\.?\d+)(?:[eE][-+]?\d+)?|[a-zA-Z][a-zA-Z0-9*]*|\S`)

type pdfStrokeState struct {
	matrix []float64
	width  float64
	ink    color.NRGBA
}
type pdfStrokeReader struct {
	points  []pdfPoint
	strokes int
	state   pdfStrokeState
	final   pdfStrokeState
	steps   int
}

// Recognize only a single stroked vector path. Text, filled artwork (including
// our signature outlines), images and complex stamps stay locked. This consumes
// standard appearance operators, without depending on Apple's private metadata.
func simplePDFStroke(raw []byte, g pdfGeometry, rect NormalizedRect) (annotations.Data, bool) {
	d := annotations.Default(annotations.Drawing)
	ctx, err := readPDFObjects(raw)
	if err != nil {
		return d, false
	}
	p, _, attrs, err := ctx.PageDict(context.Background(), 1, false)
	if err != nil {
		return d, false
	}
	content, err := ctx.PageContent(p, 1)
	if err != nil {
		return d, false
	}
	reader := pdfStrokeReader{state: pdfStrokeState{matrix: []float64{1, 0, 0, 1, 0, 0}, width: 1, ink: color.NRGBA{A: 255}}}
	var read func([]byte, types.Dict, int) bool
	read = func(content []byte, resources types.Dict, depth int) bool {
		if depth > 12 || len(content) > 2<<20 {
			return false
		}
		var args []string
		var stack []pdfStrokeState
		values := func(n int) ([]float64, bool) {
			if len(args) != n {
				return nil, false
			}
			out := make([]float64, n)
			for i, s := range args {
				v, e := strconv.ParseFloat(s, 64)
				if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
					return nil, false
				}
				out[i] = v
			}
			return out, true
		}
		point := func(x, y float64) pdfPoint {
			m := reader.state.matrix
			return pdfPoint{m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]}
		}
		clipRect := false
		for _, token := range strokeTokens.FindAllString(string(content), -1) {
			reader.steps++
			if reader.steps > 200000 {
				return false
			}
			if strings.HasPrefix(token, "/") {
				args = append(args, token)
				continue
			}
			if _, e := strconv.ParseFloat(token, 64); e == nil {
				args = append(args, token)
				continue
			}
			switch token {
			case "q":
				if len(args) != 0 {
					return false
				}
				stack = append(stack, reader.state)
			case "Q":
				if len(args) != 0 || len(stack) == 0 {
					return false
				}
				reader.state = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			case "cm":
				v, ok := values(6)
				if !ok {
					return false
				}
				reader.state.matrix = multiplyPDFMatrix(reader.state.matrix, v)
			case "w":
				v, ok := values(1)
				if !ok || v[0] <= 0 {
					return false
				}
				reader.state.width = v[0]
			case "J", "j", "M":
				if _, ok := values(1); !ok {
					return false
				}
			case "CS", "cs":
				if len(args) != 1 {
					return false
				}
				name := strings.TrimPrefix(args[0], "/")
				if name != "DeviceRGB" && name != "DeviceGray" {
					spaces, _ := ctx.DereferenceDict(resources["ColorSpace"])
					space, _ := ctx.Dereference(spaces[name])
					supported := space == types.Name("DeviceRGB") || space == types.Name("DeviceGray")
					if a, ok := space.(types.Array); ok && len(a) == 2 && a[0] == types.Name("ICCBased") {
						profile, _ := ctx.Dereference(a[1])
						if stream, ok := profile.(types.StreamDict); ok {
							n, _ := number(stream.Dict["N"])
							supported = n == 3
						}
					}
					if !supported {
						return false
					}
				}
			case "RG", "SC", "SCN":
				v, ok := values(3)
				if !ok {
					return false
				}
				reader.state.ink = color.NRGBA{R: uint8(math.Round(max(0, min(1, v[0])) * 255)), G: uint8(math.Round(max(0, min(1, v[1])) * 255)), B: uint8(math.Round(max(0, min(1, v[2])) * 255)), A: 255}
			case "G":
				v, ok := values(1)
				if !ok {
					return false
				}
				c := uint8(math.Round(max(0, min(1, v[0])) * 255))
				reader.state.ink = color.NRGBA{R: c, G: c, B: c, A: 255}
			case "re":
				v, ok := values(4)
				if !ok || len(reader.points) > 0 {
					return false
				}
				// Only the full appearance-frame clip is reproducible by the
				// drawing editor; cropped strokes must stay untouched.
				geom := geometry(attrs)
				fw, fh := geom.size()
				loX, loY, hiX, hiY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
				for _, p := range []pdfPoint{point(v[0], v[1]), point(v[0]+v[2], v[1]), point(v[0], v[1]+v[3]), point(v[0]+v[2], v[1]+v[3])} {
					p = geom.view(p)
					loX = min(loX, p.x)
					loY = min(loY, p.y)
					hiX = max(hiX, p.x)
					hiY = max(hiY, p.y)
				}
				if loX > .01 || loY > .01 || hiX < fw-.01 || hiY < fh-.01 {
					return false
				}
				clipRect = true
			case "W":
				if !clipRect || len(args) != 0 {
					return false
				}
			case "n":
				if !clipRect || len(args) != 0 {
					return false
				}
				clipRect = false
			case "m":
				v, ok := values(2)
				if !ok || len(reader.points) > 0 || reader.strokes > 0 {
					return false
				}
				reader.points = append(reader.points, point(v[0], v[1]))
			case "l":
				v, ok := values(2)
				if !ok || len(reader.points) == 0 {
					return false
				}
				reader.points = append(reader.points, point(v[0], v[1]))
			case "c":
				v, ok := values(6)
				if !ok || len(reader.points) == 0 {
					return false
				}
				a := reader.points[len(reader.points)-1]
				b, c, end := point(v[0], v[1]), point(v[2], v[3]), point(v[4], v[5])
				length := math.Hypot(b.x-a.x, b.y-a.y) + math.Hypot(c.x-b.x, c.y-b.y) + math.Hypot(end.x-c.x, end.y-c.y)
				steps := max(2, int(math.Ceil(length/.5)))
				if steps > 10000 || len(reader.points)+steps > 100000 {
					return false
				}
				for i := 1; i <= steps; i++ {
					t := float64(i) / float64(steps)
					u := 1 - t
					reader.points = append(reader.points, pdfPoint{u*u*u*a.x + 3*u*u*t*b.x + 3*u*t*t*c.x + t*t*t*end.x, u*u*u*a.y + 3*u*u*t*b.y + 3*u*t*t*c.y + t*t*t*end.y})
				}
			case "S":
				if len(args) != 0 || len(reader.points) < 2 || reader.strokes > 0 {
					return false
				}
				reader.strokes++
				reader.final = reader.state
			case "Do":
				if len(args) != 1 {
					return false
				}
				objects, _ := ctx.DereferenceDict(resources["XObject"])
				obj, err := ctx.Dereference(objects[strings.TrimPrefix(args[0], "/")])
				if err != nil {
					return false
				}
				form, ok := obj.(types.StreamDict)
				if !ok || annotationName(form.Dict, "Subtype") != "Form" || form.Decode() != nil {
					return false
				}
				saved := reader.state
				matrix, err := pdfMatrix(ctx, form.Dict["Matrix"])
				if err != nil {
					return false
				}
				reader.state.matrix = multiplyPDFMatrix(reader.state.matrix, matrix)
				child, _ := ctx.DereferenceDict(form.Dict["Resources"])
				if child == nil {
					child = resources
				}
				if !read(form.Content, child, depth+1) {
					return false
				}
				reader.state = saved
			default:
				return false
			}
			args = nil
		}
		return len(args) == 0 && len(stack) == 0 && !clipRect
	}
	if !read(content, attrs.Resources, 0) || reader.strokes != 1 {
		return d, false
	}
	m := reader.final.matrix
	sx, sy := math.Hypot(m[0], m[1]), math.Hypot(m[2], m[3])
	if math.Abs(sx-sy) > 1e-4*max(sx, sy) || math.Abs(m[0]*m[2]+m[1]*m[3]) > 1e-4*sx*sy {
		return d, false
	}
	local := geometry(attrs)
	w, h := g.size()
	d.PageWidth, d.PageHeight = w, h
	lw, lh := local.size()
	d.Width = reader.final.width * sx * (rect.Width * w / lw)
	d.Color = reader.final.ink
	for _, p := range reader.points {
		p = local.view(p)
		d.Points = append(d.Points, annotations.Point{X: rect.X*w + p.x*rect.Width*w/lw, Y: rect.Y*h + p.y*rect.Height*h/lh})
	}
	return d, d.Validate() == nil
}
