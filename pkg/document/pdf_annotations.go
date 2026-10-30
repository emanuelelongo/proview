package document

import (
	"bytes"
	"context"
	"emanuelelongo/proview/internal/pdfengine"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log"
	"math"
	"strconv"
	"strings"

	"emanuelelongo/proview/pkg/annotations"
	img "emanuelelongo/proview/pkg/images"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func (d *Document) exportPDF() ([]byte, error) {
	// Applied redactions must bake every visible appearance before sanitizing.
	editable := false
	for _, s := range d.signatures {
		if s.annotation != nil || len(s.pdfAppearance) > 0 {
			editable = true
			break
		}
	}
	if !editable || d.HasRedactions() {
		return d.exportPDFFlattened()
	}
	base, e := d.basePDF()
	if e != nil {
		return nil, e
	}
	ctx, e := readPDFObjects(base)
	if e != nil {
		return nil, e
	}
	fonts := map[string]*types.IndirectRef{}
	for _, s := range d.signatures {
		p, pageRef, attrs, e := ctx.PageDict(context.Background(), s.Page+1, false)
		if e != nil {
			return nil, e
		}
		geom := geometry(attrs)
		a, e := d.pdfAnnotation(s, geom)
		if e != nil {
			return nil, fmt.Errorf("document: write PDF annotation: %w", e)
		}
		raw, e := d.overlayPDF(s)
		if e != nil {
			return nil, e
		}
		ref, w, h, e := formFromPDF(ctx, raw, s.Transform)
		if e != nil {
			return nil, e
		}
		if s.IsHighlight() {
			obj := ctx.Table[ref.ObjectNumber.Value()].Object.(types.StreamDict)
			if e = applyMultiply(ctx, &obj); e != nil {
				return nil, e
			}
			ctx.Table[ref.ObjectNumber.Value()].Object = obj
		}

		if text, ok := s.Annotation(); ok && text.Kind == annotations.Text {
			form := ctx.Table[ref.ObjectNumber.Value()].Object.(types.StreamDict)
			resources, e := ctx.DereferenceDict(form.Dict["Resources"])
			if e != nil {
				return nil, e
			}
			if resources == nil {
				resources = types.Dict{}
			}
			fontResources, e := ctx.DereferenceDict(resources["Font"])
			if e != nil {
				return nil, e
			}
			if fontResources == nil {
				fontResources = types.Dict{}
			}
			key := fontKey(text)
			fontRef := fonts[key]
			name, existing := existingAnnotationFont(ctx, fontResources, text)
			if fontRef == nil {
				fontRef = existing
			}
			if fontRef == nil {
				fontRef, e = annotationFont(ctx, text)
				if e != nil {
					return nil, e
				}
			}
			fonts[key] = fontRef
			if name == "" {
				for i := 0; ; i++ {
					name = fmt.Sprintf("FEdit%d", i)
					if fontResources[name] == nil {
						break
					}
				}
			}
			fontResources[name] = *fontRef
			resources["Font"] = fontResources
			form.Dict["Resources"] = resources
			ctx.Table[ref.ObjectNumber.Value()].Object = form
			a["DA"] = pdfText(strings.Replace(annotationString(ctx, a, "DA"), "/F0 ", "/"+name+" ", 1))
		}
		// Appearance coordinates must counter the page's display rotation.
		if geom.rotation != 0 {
			obj := ctx.Table[ref.ObjectNumber.Value()].Object.(types.StreamDict)

			prior, e := pdfMatrix(ctx, obj.Dict["Matrix"])
			if e != nil {
				return nil, e
			}
			rotation := []float64{1, 0, 0, 1, 0, 0}
			switch geom.rotation {
			case 90:
				rotation = []float64{0, 1, -1, 0, h, 0}
			case 180:
				rotation = []float64{-1, 0, 0, -1, w, h}
			case 270:
				rotation = []float64{0, -1, 1, 0, 0, w}
			}
			obj.Dict["Matrix"] = pdfArray(multiplyPDFMatrix(rotation, prior)...)

			ctx.Table[ref.ObjectNumber.Value()].Object = obj
		}
		a["AP"] = types.Dict{"N": *ref}
		a["P"] = *pageRef
		annots, e := ctx.DereferenceArray(p["Annots"])
		if e != nil {
			return nil, e
		}
		ar, e := ctx.IndRefForNewObject(a)
		if e != nil {
			return nil, e
		}
		p["Annots"] = append(annots, *ar)
	}
	out, e := writePDFContext(ctx)
	if e != nil {
		return nil, e
	}
	if e = api.Validate(context.Background(), bytes.NewReader(out), nil, nil); e != nil {
		return nil, e
	}
	return out, nil
}

func (d *Document) overlayPDF(s Overlay) ([]byte, error) {
	if len(s.pdfAppearance) > 0 {
		return s.pdfAppearance, nil
	}
	if s.IsLens() {
		picture, e := d.RenderLens(s.ID, 300)
		if e != nil {
			return nil, e
		}
		return imagePDF(picture)
	}
	if s.IsVector() {
		return s.Asset().PDF()
	}
	return imagePDF(s.preview)
}
func imagePDF(p image.Image) ([]byte, error) {
	var encoded, out bytes.Buffer
	if e := png.Encode(&encoded, p); e != nil {
		return nil, e
	}
	e := api.ImportImages(context.Background(), nil, &out, []io.Reader{bytes.NewReader(encoded.Bytes())}, nil, nil)
	return out.Bytes(), e
}

func (d *Document) pdfAnnotation(s Overlay, g pdfGeometry) (types.Dict, error) {
	w, h := g.size()
	corners := []pdfPoint{g.user(pdfPoint{s.Rect.X * w, s.Rect.Y * h}), g.user(pdfPoint{(s.Rect.X + s.Rect.Width) * w, (s.Rect.Y + s.Rect.Height) * h})}
	loX, hiX := math.Min(corners[0].x, corners[1].x), math.Max(corners[0].x, corners[1].x)
	loY, hiY := math.Min(corners[0].y, corners[1].y), math.Max(corners[0].y, corners[1].y)
	a := types.Dict{"Type": types.Name("Annot"), "Subtype": types.Name("Stamp"), "Rect": pdfArray(loX, loY, hiX, hiY), "F": types.Integer(4 | 64 | 128)}
	for k, v := range s.pdfMarkup {
		a[k] = pdfText(v)
	}
	data, ok := s.Annotation()
	if !ok || s.IsLens() {
		// The standard markup subject distinguishes signature artwork from a
		// generic stroked stamp without any private keys or editor payload.
		if s.ObjectKind == "signature" {
			a["Subj"] = pdfText("Signature")
		}
		return a, nil
	}
	_, b, e := data.Artwork()
	if e != nil {
		return nil, e
	}
	mapPoint := func(p annotations.Point) pdfPoint {
		q := transformUnit(pdfPoint{(p.X - b.X0) / b.W(), (p.Y - b.Y0) / b.H()}, s.Transform)
		return g.user(pdfPoint{(s.Rect.X + q.x*s.Rect.Width) * w, (s.Rect.Y + q.y*s.Rect.Height) * h})
	}
	pointArray := func(points []annotations.Point) types.Array {
		var v []float64
		for _, p := range points {
			q := mapPoint(p)
			v = append(v, q.x, q.y)
		}
		return pdfArray(v...)
	}
	a["F"] = types.Integer(4)
	delete(a, "Name")
	a["C"] = pdfArray(float64(data.Color.R)/255, float64(data.Color.G)/255, float64(data.Color.B)/255)
	a["CA"] = types.Float(data.Opacity)
	bs := types.Dict{"W": types.Float(data.Width), "S": types.Name("S")}
	switch data.StrokeStyle {
	case annotations.StrokeDashed:
		bs["S"] = types.Name("D")
		bs["D"] = pdfArray(data.Width*4, data.Width*3)
	case annotations.StrokeDotted:
		bs["S"] = types.Name("D")
		bs["D"] = pdfArray(data.Width, data.Width*2)
	}
	a["BS"] = bs
	switch data.Kind {
	case annotations.Text:
		a["Subtype"] = types.Name("FreeText")
		a["Contents"] = pdfText(data.Text)
		style := fmt.Sprintf("font-family:%s;font-size:%.8fpt;color:#%02x%02x%02x", data.Font, data.Size, data.Color.R, data.Color.G, data.Color.B)
		if data.Bold {
			style += ";font-weight:bold"
		}
		if data.Italic {
			style += ";font-style:italic"
		}
		if data.Underline || data.Strikethrough {
			style += ";text-decoration:"
			if data.Underline {
				style += "underline "
			}
			if data.Strikethrough {
				style += "line-through"
			}
		}
		a["DS"] = pdfText(style)
		a["RC"] = pdfText(`<body xmlns="http://www.w3.org/1999/xhtml"><p><span style="` + html.EscapeString(style) + `">` + html.EscapeString(data.Text) + `</span></p></body>`)
		a["DA"] = pdfText(fmt.Sprintf("/F0 %.8f Tf %.8f %.8f %.8f rg", data.Size, float64(data.Color.R)/255, float64(data.Color.G)/255, float64(data.Color.B)/255))
		a["BS"] = types.Dict{"W": types.Integer(0)}
		a["Q"] = types.Integer(0)
		a["Rotate"] = types.Integer((g.rotation - s.Transform.Turns*90 + 360) % 360)
		// FreeText's C is the background, not its text color. Text color is in DA/DS.
		delete(a, "C")
	case annotations.Drawing:
		a["Subtype"] = types.Name("Ink")
		a["InkList"] = types.Array{pointArray(data.Points)}
	case annotations.Highlight:
		if len(data.Quads) == 0 {
			a["Subtype"] = types.Name("Ink")
			a["InkList"] = types.Array{pointArray(data.Points)}
			break
		}
		a["Subtype"] = types.Name("Highlight")
		var q types.Array
		for _, quad := range data.Quads {
			q = append(q, pointArray([]annotations.Point{quad[0], quad[1], quad[3], quad[2]})...)
		}
		a["QuadPoints"] = q
	case annotations.Shape:
		if data.Filled {
			a["IC"] = a["C"]
		}
		switch data.Shape {
		case annotations.Rectangle, annotations.Oval:
			if data.Shape == annotations.Rectangle {
				a["Subtype"] = types.Name("Square")
			} else {
				a["Subtype"] = types.Name("Circle")
			}
			// RD is the standard difference between the appearance and shape bounds.
			p, q := mapPoint(data.Points[0]), mapPoint(data.Points[1])
			a["RD"] = pdfArray(math.Min(p.x, q.x)-loX, hiY-math.Max(p.y, q.y), hiX-math.Max(p.x, q.x), math.Min(p.y, q.y)-loY)
		case annotations.Line, annotations.Arrow:
			a["Subtype"] = types.Name("Line")
			a["L"] = pointArray(data.Points)
			a["LE"] = types.Array{types.Name("None"), types.Name("None")}
			if data.Shape == annotations.Arrow {
				ending := types.Name("OpenArrow")
				if data.ArrowClosed {
					ending = types.Name("ClosedArrow")
				}
				a["LE"] = types.Array{types.Name("None"), ending}
			}
		default:
			a["Subtype"] = types.Name("Polygon")
			a["Vertices"] = pointArray(data.PolygonVertices())
		}
	}
	return a, nil
}

func pdfText(s string) types.StringLiteral {
	value, e := types.EscapedUTF16String(s)
	if e != nil {
		return types.StringLiteral("")
	}
	return types.StringLiteral(*value)
}
func annotationString(ctx *model.Context, d types.Dict, k string) string {
	o, e := ctx.Dereference(d[k])
	if e != nil {
		return ""
	}
	s, e := types.StringOrHexLiteral(o)
	if e != nil {
		return ""
	}
	return *s
}
func annotationName(d types.Dict, k string) string {
	n := d.NameEntry(k)
	if n == nil {
		return ""
	}
	return *n
}

// Unsupported or malformed objects stay in the immutable source, with their
// original appearance/actions/resources. Only successfully prepared objects are
// detached, so opening never destroys content that the editor cannot represent.
func readPDFAnnotations(data []byte) ([]byte, []Overlay, bool, error) {
	ctx, e := readPDFObjects(data)
	if e != nil {
		log.Printf("event=pdf_annotations_unavailable level=WARN error=%q", e)
		return nil, nil, true, nil
	}
	var previewPixels int64
	var overlays []Overlay
	hasAnnots := false
	for page := 1; page <= ctx.PageCount; page++ {
		p, _, attrs, e := ctx.PageDict(context.Background(), page, false)
		if e != nil {
			log.Printf("event=pdf_annotations_unavailable level=WARN error=%q", e)
			return nil, nil, true, nil
		}
		list, e := ctx.DereferenceArray(p["Annots"])
		if e != nil {
			log.Printf("event=pdf_annotations_unavailable level=WARN error=%q", e)
			return nil, nil, true, nil
		}
		if len(list) > 0 {
			hasAnnots = true
		}
		retained := types.Array{}
		for _, ref := range list {
			a, e := ctx.DereferenceDict(ref)
			if e != nil {
				log.Printf("event=pdf_annotations_unavailable level=WARN error=%q", e)
				return nil, nil, true, nil
			}
			if len(overlays) >= 1024 || previewPixels >= 64*1024*1024 {
				retained = append(retained, ref)
				continue
			}
			s, ok := importPDFAnnotation(ctx, a, geometry(attrs))
			if !ok {
				retained = append(retained, ref)
				continue
			}
			s.Page = page - 1
			previewPixels += int64(s.preview.Bounds().Dx()) * int64(s.preview.Bounds().Dy())
			overlays = append(overlays, s)
		}
		if len(retained) > 0 {
			p["Annots"] = retained
		} else {
			delete(p, "Annots")
		}
	}
	if len(overlays) == 0 {
		return nil, nil, hasAnnots, nil
	}
	clean, e := writePDFContext(ctx)
	if e != nil {
		log.Printf("event=pdf_annotations_unavailable level=WARN error=%q", e)
		return nil, nil, true, nil
	}
	log.Printf("event=pdf_annotations_loaded objects=%d", len(overlays))
	return clean, overlays, hasAnnots, nil
}
func importPDFAnnotation(ctx *model.Context, a types.Dict, g pdfGeometry) (Overlay, bool) {
	s := Overlay{}
	if a == nil {
		return s, false
	}
	flag, _ := number(a["F"])
	if int(flag)&(1|2|32) != 0 {
		return s, false
	}
	subtype := annotationName(a, "Subtype")
	if int(flag)&(64|128) != 0 && subtype != "Stamp" {
		return s, false
	}
	if subtype != "FreeText" && subtype != "Ink" && subtype != "Highlight" && subtype != "Square" && subtype != "Circle" && subtype != "Line" && subtype != "Polygon" && subtype != "Stamp" {
		return s, false
	}
	rect, e := numbers(ctx, a["Rect"])
	if e != nil || len(rect) != 4 {
		return s, false
	}
	w, h := g.size()
	p, q := g.view(pdfPoint{rect[0], rect[1]}), g.view(pdfPoint{rect[2], rect[3]})
	s.Rect = NormalizedRect{math.Min(p.x, q.x) / w, math.Min(p.y, q.y) / h, math.Abs(q.x-p.x) / w, math.Abs(q.y-p.y) / h}
	if !s.Rect.Valid() {
		return s, false
	}
	raw, e := annotationAppearance(ctx, a, rect, g)
	if e != nil && subtype != "Line" {
		return s, false
	}
	// Render the existing appearance once, keeping all vector bytes for export.
	var preview image.Image
	if len(raw) > 0 {
		preview, e = pdfengine.RenderFormPage(raw, 0, math.Min(144, 2048*72/math.Max(s.Rect.Width*w, s.Rect.Height*h)))
		if e != nil {
			return s, false
		}
	}
	if preview == nil {
		preview = image.NewNRGBA(image.Rect(0, 0, 1, 1))
	}
	var encoded bytes.Buffer
	if png.Encode(&encoded, preview) != nil {
		return s, false
	}
	s.preview = preview
	s.png = encoded.Bytes()
	s.pdfAppearance = raw
	s.ObjectKind = "pdf-stamp"
	s.pdfMarkup = map[string]string{}
	for _, key := range []string{"T", "Subj", "NM", "M", "CreationDate", "Contents"} {
		if a[key] != nil {
			s.pdfMarkup[key] = annotationString(ctx, a, key)
		}
	}
	if subtype == "Stamp" || int(flag)&(64|128) != 0 {
		if subtype == "Stamp" && !strings.EqualFold(annotationString(ctx, a, "Subj"), "Signature") {
			if data, ok := simplePDFPath(raw, g, s.Rect, true); ok {
				s.annotation = &data
				s.ObjectKind = string(data.Kind)
			}
		}
		return s, true
	}
	d := annotations.Default(annotations.Drawing)
	if subtype != "Ink" {
		d.StrokeStyle = annotations.StrokeSolid
	}
	d.PageWidth = w
	d.PageHeight = h
	d.Color = pdfColor(ctx, a["C"], color.NRGBA{A: 255})
	d.Opacity = 1
	if n, ok := number(a["CA"]); ok {
		d.Opacity = n
	}
	if bs, _ := ctx.DereferenceDict(a["BS"]); bs != nil {
		if n, ok := number(bs["W"]); ok {
			d.Width = n
		}
		if annotationName(bs, "S") == "D" {
			d.StrokeStyle = annotations.StrokeDashed
			if dash, _ := numbers(ctx, bs["D"]); len(dash) > 0 && dash[0] <= d.Width*1.5 {
				d.StrokeStyle = annotations.StrokeDotted
			}
		}
	}
	points := func(o types.Object) []annotations.Point {
		v, e := numbers(ctx, o)
		if e != nil || len(v)%2 != 0 || len(v) > 200000 {
			return nil
		}
		var result []annotations.Point
		for i := 0; i < len(v); i += 2 {
			p := g.view(pdfPoint{v[i], v[i+1]})
			result = append(result, annotations.Point{X: p.x, Y: p.y})
		}
		return result
	}
	switch subtype {
	case "FreeText":
		// Preserve foreign text boxes whose layout the single-line editor cannot
		// reproduce. Their original appearance remains part of the source page.
		if a["CL"] != nil || a["C"] != nil || (a["Q"] != nil && a["Q"] != types.Integer(0)) {
			return Overlay{}, false
		}
		d = annotations.Default(annotations.Text)
		d.Text = annotationString(ctx, a, "Contents")
		style := annotationString(ctx, a, "DS")
		for _, part := range strings.Split(style, ";") {
			key, value, ok := strings.Cut(part, ":")
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			switch strings.TrimSpace(key) {
			case "font-family":
				for _, font := range annotations.Fonts {
					if strings.Contains(value, font) {
						d.Font = font
					}
				}
			case "font-size":
				if size, e := strconv.ParseFloat(strings.TrimSuffix(value, "pt"), 64); e == nil {
					d.Size = size
				}
			case "font-weight":
				d.Bold = value == "bold"
			case "font-style":
				d.Italic = value == "italic"
			case "text-decoration":
				d.Underline = strings.Contains(value, "underline")
				d.Strikethrough = strings.Contains(value, "line-through")
			case "color":
				if len(value) == 7 && value[0] == '#' {
					if rgb, e := strconv.ParseUint(value[1:], 16, 32); e == nil {
						d.Color = color.NRGBA{R: uint8(rgb >> 16), G: uint8(rgb >> 8), B: uint8(rgb), A: 255}
					}
				}
			}
		}
		// DA is required by PDF and is the fallback when rich styling is absent.
		fields := strings.Fields(annotationString(ctx, a, "DA"))
		{
			for i, token := range fields {
				if token == "Tf" && i >= 2 {
					if n, e := strconv.ParseFloat(fields[i-1], 64); e == nil {
						d.Size = n
					}

					name := defaultAppearanceFontName(ctx, a, strings.TrimPrefix(fields[i-2], "/"))
					lower := strings.ToLower(name)
					if strings.Contains(lower, "courier") || strings.Contains(lower, "gomono") {
						d.Font = "Go Mono"
					} else if strings.Contains(lower, "times") || strings.Contains(lower, "lmroman") {
						d.Font = "Latin Modern Roman"
					} else if strings.Contains(lower, "helv") || strings.Contains(lower, "goregular") || strings.Contains(lower, "gobold") || strings.Contains(lower, "goitalic") {
						d.Font = "Go Sans"
					}
					if name != "" {
						d.Bold = strings.Contains(lower, "bold")
						d.Italic = strings.Contains(lower, "italic") || strings.Contains(lower, "oblique")
					}
					name = strings.ToLower(fields[i-2])
					if style == "" {
						d.Bold = strings.Contains(name, "bold")
						d.Italic = strings.Contains(name, "italic") || strings.Contains(name, "oblique")
					}
				}
				if token == "rg" && i >= 3 {
					var rgb [3]uint8
					for j := 0; j < 3; j++ {
						n, _ := strconv.ParseFloat(fields[i-3+j], 64)
						rgb[j] = uint8(math.Max(0, math.Min(255, math.Round(n*255))))
					}
					d.Color = color.NRGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 255}
				}
			}
		}

		rotation, _ := number(a["Rotate"])
		s.Transform = textAppearanceTransform(ctx, a, g, img.Transform{Turns: ((g.rotation - int(rotation) + 360) % 360) / 90})
		if s.Transform != (img.Transform{}) {
			raw, e = normalizedAppearance(raw, inversePDFTransform(s.Transform))
			if e != nil {
				return Overlay{}, false
			}
			preview, e := pdfengine.RenderFormPage(raw, 0, math.Min(144, 2048*72/math.Max(s.Rect.Width*w, s.Rect.Height*h)))
			if e != nil {
				return Overlay{}, false
			}
			var encoded bytes.Buffer
			if png.Encode(&encoded, preview) != nil {
				return Overlay{}, false
			}
			s.preview = preview
			s.png = encoded.Bytes()
			s.pdfAppearance = raw
		}

	case "Ink":
		paths, _ := ctx.DereferenceArray(a["InkList"])
		if len(paths) != 1 {
			return Overlay{}, false
		}
		d.Points = points(paths[0])
	case "Highlight":
		d.Kind = annotations.Highlight
		d.Width = 12
		p := points(a["QuadPoints"])
		if len(p) == 0 || len(p)%4 != 0 {
			return Overlay{}, false
		}
		for i := 0; i < len(p); i += 4 {
			d.Quads = append(d.Quads, annotations.Quad{p[i], p[i+1], p[i+3], p[i+2]})
		}
	case "Square", "Circle":
		d.Kind = annotations.Shape
		d.Shape = annotations.Rectangle
		d.Magnification = 2
		if subtype == "Circle" {
			d.Shape = annotations.Oval
		}
		inner := append([]float64(nil), rect...)
		if rd, _ := numbers(ctx, a["RD"]); len(rd) == 4 {
			inner[0] += rd[0]
			inner[1] += rd[3]
			inner[2] -= rd[2]
			inner[3] -= rd[1]
		}
		d.Points = points(pdfArray(inner...))
		d.Filled = a["IC"] != nil
		if d.Filled {
			d.Color = pdfColor(ctx, a["IC"], d.Color)
		}
	case "Line":
		d.Kind = annotations.Shape
		d.Shape = annotations.Line
		d.Magnification = 2
		d.Points = points(a["L"])
		ends, _ := ctx.DereferenceArray(a["LE"])
		if len(ends) == 2 && ends[0] == types.Name("None") && (ends[1] == types.Name("OpenArrow") || ends[1] == types.Name("ClosedArrow")) {
			d.Shape = annotations.Arrow
			d.ArrowClosed = ends[1] == types.Name("ClosedArrow")
		} else if len(ends) != 0 && (len(ends) != 2 || ends[0] != types.Name("None") || ends[1] != types.Name("None")) {
			return Overlay{}, false
		}
	case "Polygon":
		d.Kind = annotations.Shape
		d.Shape = annotations.Polygon
		d.Magnification = 2
		d.Points = points(a["Vertices"])
		d.Filled = a["IC"] != nil
		if d.Filled {
			d.Color = pdfColor(ctx, a["IC"], d.Color)
		}
	}
	if d.Validate() != nil {
		return Overlay{}, false
	}
	art, _, err := d.Artwork()
	if err != nil {
		return Overlay{}, false
	}
	if len(s.pdfAppearance) == 0 {
		prepared, err := art.Prepare()
		if err != nil {
			return Overlay{}, false
		}
		s.asset = &prepared.Asset
		s.png = prepared.PNG
		s.preview = prepared.Preview
	}
	// Keep physical geometry relative to the original appearance frame. Replacing
	// properties regenerates artwork; the normal placement/resize path refits it.
	copy := d.Clone()
	s.annotation = &copy
	s.ObjectKind = string(d.Kind)
	return s, true
}
func pdfColor(ctx *model.Context, o types.Object, fallback color.NRGBA) color.NRGBA {
	v, e := numbers(ctx, o)
	if e != nil {
		return fallback
	}
	if len(v) == 1 {
		v = []float64{v[0], v[0], v[0]}
	}
	if len(v) != 3 {
		return fallback
	}
	return color.NRGBA{R: uint8(math.Max(0, math.Min(255, math.Round(v[0]*255)))), G: uint8(math.Max(0, math.Min(255, math.Round(v[1]*255)))), B: uint8(math.Max(0, math.Min(255, math.Round(v[2]*255)))), A: 255}
}

// Page previews use paper white, matching go-fitz. Isolated appearances keep
// alpha so their previews can be composed without opaque rectangular patches.
func renderPDFPage(data []byte, page int, dpi float64) (image.Image, error) {
	p, e := pdfengine.RenderFormPage(data, page, dpi)
	if e != nil {
		return nil, e
	}
	rgba, ok := p.(*image.RGBA)
	if !ok {
		out := image.NewRGBA(p.Bounds())
		draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
		draw.Draw(out, out.Bounds(), p, p.Bounds().Min, draw.Over)
		return out, nil
	}
	flattenOnWhite(rgba)
	return rgba, nil
}

// flattenOnWhite composites premultiplied pixels over white in place, avoiding
// a second full-page raster for every form render.
func flattenOnWhite(p *image.RGBA) {
	for i := 0; i+3 < len(p.Pix); i += 4 {
		inverse := 255 - p.Pix[i+3]
		p.Pix[i] += inverse
		p.Pix[i+1] += inverse
		p.Pix[i+2] += inverse
		p.Pix[i+3] = 255
	}
}
