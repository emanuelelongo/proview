package signatures

import (
	"bytes"
	img "emanuelelongo/proview/pkg/images"
	"encoding/xml"
	"fmt"
	vector "github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/svg"
	"image/color"
	"image/png"
	"io"
	"math"
	"strconv"
	"strings"
)

// Transformed retains vector paths and leaves original drawing data untouched.
func (a Asset) Transformed(t img.Transform) (Asset, error) {
	if t == (img.Transform{}) {
		return a.Clone(), nil
	}
	if a.Kind == PNG {
		p, e := a.Render(1024)
		if e != nil {
			return Asset{}, e
		}
		var b bytes.Buffer
		e = png.Encode(&b, t.Apply(p))
		return Asset{Kind: PNG, Data: b.Bytes()}, e
	}
	c, e := parseSVG(a.Data)
	if e != nil {
		return Asset{}, e
	}
	w, h := c.Size()
	ow, oh := w, h
	m := vector.Identity
	if t.FlipX {
		m = m.Translate(w, 0).Scale(-1, 1)
	}
	if t.FlipY {
		m = m.Translate(0, h).Scale(1, -1)
	}
	// Canvas coordinates run upwards; a clockwise turn is negative.
	switch t.Turns {
	case 1:
		m = vector.Identity.Translate(0, w).Rotate(-90).Mul(m)
		ow, oh = h, w
	case 2:
		m = vector.Identity.Translate(w, h).Rotate(180).Mul(m)
	case 3:
		m = vector.Identity.Translate(h, 0).Rotate(90).Mul(m)
		ow, oh = h, w
	}
	var b bytes.Buffer
	r := svg.New(&b, ow, oh, nil)
	c.RenderViewTo(r, m)
	if e = r.Close(); e != nil {
		return Asset{}, e
	}
	return Asset{Kind: SVG, Data: normalizeSVGAlpha(b.Bytes()), Thickness: a.Thickness}, nil
}

// WithColor changes visible paints while keeping geometry and alpha intact.
func (a Asset) WithColor(c color.NRGBA) (Asset, error) {
	if a.Kind == PNG {
		return Asset{}, fmt.Errorf("PNG signatures cannot be recolored")
	}
	if a.Drawing != nil {
		d := a.Drawing.Clone()
		d.Color = c
		return FromDrawing(d)
	}
	dec := xml.NewDecoder(bytes.NewReader(normalizeSVGAlpha(a.Data)))
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	paint := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	for {
		tok, e := dec.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return Asset{}, e
		}
		if start, ok := tok.(xml.StartElement); ok {
			hasFill := false
			for i := range start.Attr {
				v := &start.Attr[i]
				if v.Name.Local == "fill" {
					hasFill = true
				}
				if v.Name.Local == "fill" || v.Name.Local == "stroke" || v.Name.Local == "color" {
					if v.Value != "none" && v.Value != "transparent" {
						v.Value = paint
					}
				}
				if v.Name.Local == "style" {
					decl := strings.Split(v.Value, ";")
					for j, s := range decl {
						key, value, ok := strings.Cut(s, ":")
						key = strings.TrimSpace(key)
						if ok && (key == "fill" || key == "stroke" || key == "color") && strings.TrimSpace(value) != "none" && strings.TrimSpace(value) != "transparent" {
							decl[j] = key + ":" + paint
						}
					}
					v.Value = strings.Join(decl, ";")
				}
			}
			if start.Name.Local == "svg" && !hasFill {
				start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "fill"}, Value: paint})
			}
			tok = start
		}
		if e = enc.EncodeToken(tok); e != nil {
			return Asset{}, e
		}
	}
	if e := enc.Flush(); e != nil {
		return Asset{}, e
	}
	result := Asset{Kind: SVG, Data: out.Bytes(), Thickness: a.Thickness}
	return result, result.Validate()
}

// WithAspect preserves paths while matching independent object axis resizing.
func (a Asset) WithAspect(aspect float64) (Asset, error) {
	if a.Kind == PNG {
		return a.Clone(), nil
	}
	c, e := parseSVG(a.Data)
	if e != nil {
		return Asset{}, e
	}
	w, h := c.Size()
	height := w / aspect
	if math.Abs(height-h) < 1e-8 {
		return a.Clone(), nil
	}
	var b bytes.Buffer
	r := svg.New(&b, w, height, nil)
	c.RenderViewTo(r, vector.Identity.Scale(1, height/h))
	if e = r.Close(); e != nil {
		return Asset{}, e
	}
	return Asset{Kind: SVG, Data: normalizeSVGAlpha(b.Bytes()), Thickness: a.Thickness}, nil
}

// Normalize CSS rgba into explicit paint opacity to avoid upstream parser
// differences while retaining the original alpha through vector transformations.
func normalizeSVGAlpha(data []byte) []byte {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var out bytes.Buffer
	encoder := xml.NewEncoder(&out)
	convert := func(value string) (string, string) {
		if !strings.HasPrefix(value, "rgba(") {
			return value, ""
		}
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(value, "rgba("), ")"), ",")
		if len(parts) != 4 {
			return value, ""
		}
		r, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		g, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		b, e3 := strconv.Atoi(strings.TrimSpace(parts[2]))
		alpha, e4 := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			return value, ""
		}
		return fmt.Sprintf("#%02x%02x%02x", r, g, b), strconv.FormatFloat(alpha, 'g', -1, 64)
	}
	for {
		token, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return data
		}
		if start, ok := token.(xml.StartElement); ok {
			var extra []xml.Attr
			for i := range start.Attr {
				attr := &start.Attr[i]
				if attr.Name.Local == "fill" || attr.Name.Local == "stroke" {
					paint, opacity := convert(attr.Value)
					attr.Value = paint
					if opacity != "" {
						extra = append(extra, xml.Attr{Name: xml.Name{Local: attr.Name.Local + "-opacity"}, Value: opacity})
					}
				}
				if attr.Name.Local == "style" {
					var declarations []string
					for _, d := range strings.Split(attr.Value, ";") {
						key, value, ok := strings.Cut(d, ":")
						key = strings.TrimSpace(key)
						if ok && (key == "fill" || key == "stroke") {
							paint, opacity := convert(strings.TrimSpace(value))
							d = key + ":" + paint
							if opacity != "" {
								declarations = append(declarations, key+"-opacity:"+opacity)
							}
						}
						declarations = append(declarations, d)
					}
					attr.Value = strings.Join(declarations, ";")
				}
			}
			start.Attr = append(start.Attr, extra...)
			token = start
		}
		if e = encoder.EncodeToken(token); e != nil {
			return data
		}
	}
	if encoder.Flush() != nil {
		return data
	}
	return out.Bytes()
}

// InkColor supplies a property swatch; multicolor SVGs use their first paint.
func (a Asset) InkColor() color.NRGBA {
	if a.Drawing != nil && a.Drawing.Color.A != 0 {
		return a.Drawing.Color
	}
	dec := xml.NewDecoder(bytes.NewReader(a.Data))
	parse := func(value string) (color.NRGBA, bool) {
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "#") && len(value) == 7 {
			v, e := strconv.ParseUint(value[1:], 16, 32)
			if e == nil {
				return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}, true
			}
		}
		return color.NRGBA{}, false
	}
	for {
		token, e := dec.Token()
		if e != nil {
			break
		}
		if element, ok := token.(xml.StartElement); ok {
			for _, attr := range element.Attr {
				if attr.Name.Local == "fill" || attr.Name.Local == "stroke" {
					if c, ok := parse(attr.Value); ok {
						return c
					}
				}
				if attr.Name.Local == "style" {
					for _, d := range strings.Split(attr.Value, ";") {
						key, value, _ := strings.Cut(d, ":")
						if key == "fill" || key == "stroke" {
							if c, ok := parse(value); ok {
								return c
							}
						}
					}
				}
			}
		}
	}
	return color.NRGBA{A: 255}
}
