package signatures

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	vector "github.com/tdewolff/canvas"
	"golang.org/x/image/colornames"
)

// The upstream parser handles geometry and transforms; this adapter applies
// inherited opacity and fill rules explicitly rather than silently ignoring them.
type svgState struct {
	styles    map[string]string
	transform string
}
type styledCanvas struct {
	*vector.Canvas
	fillAlpha, strokeAlpha float64
	evenOdd                bool
}

func (r styledCanvas) RenderPath(path *vector.Path, style vector.Style, m vector.Matrix) {
	style.Fill.Color.R = uint8(float64(style.Fill.Color.R) * r.fillAlpha)
	style.Fill.Color.G = uint8(float64(style.Fill.Color.G) * r.fillAlpha)
	style.Fill.Color.B = uint8(float64(style.Fill.Color.B) * r.fillAlpha)
	style.Fill.Color.A = uint8(float64(style.Fill.Color.A) * r.fillAlpha)
	style.Stroke.Color.R = uint8(float64(style.Stroke.Color.R) * r.strokeAlpha)
	style.Stroke.Color.G = uint8(float64(style.Stroke.Color.G) * r.strokeAlpha)
	style.Stroke.Color.B = uint8(float64(style.Stroke.Color.B) * r.strokeAlpha)
	style.Stroke.Color.A = uint8(float64(style.Stroke.Color.A) * r.strokeAlpha)
	if r.evenOdd && style.HasFill() {
		// Normalize holes to nonzero contours, which the raster preview also
		// supports, while preserving the original stroke geometry.
		fillStyle := style
		fillStyle.Stroke = vector.Paint{}
		fillStyle.FillRule = vector.NonZero
		r.Canvas.RenderPath(path.Settle(vector.EvenOdd), fillStyle, m)
		style.Fill = vector.Paint{}
		if style.HasStroke() {
			r.Canvas.RenderPath(path, style, m)
		}
		return
	}
	r.Canvas.RenderPath(path, style, m)
}

func svgAlpha(value string) (float64, error) {
	if value == "" {
		return 1, nil
	}
	percent := strings.HasSuffix(value, "%")
	alpha, err := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
	if percent {
		alpha /= 100
	}
	if err != nil || math.IsNaN(alpha) || math.IsInf(alpha, 0) || alpha < 0 || alpha > 1 {
		return 0, fmt.Errorf("invalid SVG opacity %q", value)
	}
	return alpha, nil
}

func parseSupportedSVG(data []byte) (*vector.Canvas, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var root xml.StartElement
	var out *vector.Canvas
	stack := []svgState{{styles: map[string]string{}, transform: ""}}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local == "metadata" {
				if err := decoder.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			state := svgState{styles: map[string]string{}, transform: stack[len(stack)-1].transform}
			for k, v := range stack[len(stack)-1].styles {
				state.styles[k] = v
			}
			attributes := map[string]string{}
			for _, attr := range element.Attr {
				if attr.Name.Space == "" {
					attributes[attr.Name.Local] = attr.Value
				}
			}
			for _, declaration := range strings.Split(attributes["style"], ";") {
				key, value, ok := strings.Cut(declaration, ":")
				if ok {
					attributes[strings.TrimSpace(key)] = strings.TrimSpace(value)
				}
			}
			for key, value := range attributes {
				if value == "inherit" {
					continue
				}
				switch key {
				case "fill", "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin", "stroke-miterlimit", "fill-rule", "fill-opacity", "stroke-opacity", "stroke-dasharray", "stroke-dashoffset", "color":
					if key == "fill" || key == "stroke" || key == "color" {
						if err := validatePaint(value); err != nil {
							return nil, err
						}
					}
					state.styles[key] = value
				}
			}
			if transform := attributes["transform"]; transform != "" {
				state.transform += " " + transform
			}
			stack = append(stack, state)
			if element.Name.Local == "svg" {
				if out != nil {
					return nil, fmt.Errorf("nested SVG viewports are not supported")
				}
				root = xml.StartElement{Name: xml.Name{Local: "svg"}}
				var standardBox [4]float64
				haveBox := false
				// This pinned parser expects viewBox endpoints. Convert the standard
				// SVG width/height representation only at the renderer boundary.
				if box := attributes["viewBox"]; box != "" {
					values := strings.Fields(strings.ReplaceAll(box, ",", " "))
					if len(values) != 4 {
						return nil, fmt.Errorf("invalid SVG viewBox")
					}
					var v [4]float64
					for i, value := range values {
						number, err := strconv.ParseFloat(value, 64)
						if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
							return nil, fmt.Errorf("invalid SVG viewBox")
						}
						v[i] = number
					}
					if v[2] <= 0 || v[3] <= 0 {
						return nil, fmt.Errorf("invalid SVG viewBox dimensions")
					}
					standardBox = v
					haveBox = true
					attributes["viewBox"] = fmt.Sprintf("%.9f %.9f %.9f %.9f", v[0], v[1], v[0]+v[2], v[1]+v[3])
				}
				for _, key := range []string{"viewBox", "width", "height", "version"} {
					if value := attributes[key]; value != "" {
						root.Attr = append(root.Attr, xml.Attr{Name: xml.Name{Local: key}, Value: value})
					}
				}
				var b bytes.Buffer
				enc := xml.NewEncoder(&b)
				_ = enc.EncodeToken(root)
				_ = enc.EncodeToken(root.End())
				_ = enc.Flush()
				base, err := vector.ParseSVG(&b)
				if err != nil {
					return nil, err
				}
				w, h := base.Size()
				if w <= 0 || h <= 0 || math.IsNaN(w+h) || math.IsInf(w+h, 0) {
					return nil, fmt.Errorf("invalid SVG dimensions")
				}
				out = vector.New(w, h)
				mode := strings.TrimSpace(attributes["preserveAspectRatio"])
				if mode != "" && mode != "xMidYMid" && mode != "xMidYMid meet" && mode != "none" {
					return nil, fmt.Errorf("unsupported SVG preserveAspectRatio %q", mode)
				}
				if haveBox && mode != "none" {
					scale := math.Min(w/standardBox[2], h/standardBox[3])
					boxW, boxH := w/scale, h/scale
					x, y := standardBox[0]-(boxW-standardBox[2])/2, standardBox[1]-(boxH-standardBox[3])/2
					for i := range root.Attr {
						if root.Attr[i].Name.Local == "viewBox" {
							root.Attr[i].Value = fmt.Sprintf("%.9f %.9f %.9f %.9f", x, y, x+boxW, y+boxH)
						}
					}
					// Fix the canvas dimensions while expanding its logical viewport.
					filtered := root.Attr[:0]
					for _, attr := range root.Attr {
						if attr.Name.Local != "width" && attr.Name.Local != "height" {
							filtered = append(filtered, attr)
						}
					}
					root.Attr = filtered
					root.Attr = append(root.Attr, xml.Attr{Name: xml.Name{Local: "width"}, Value: fmt.Sprintf("%.9f", w)}, xml.Attr{Name: xml.Name{Local: "height"}, Value: fmt.Sprintf("%.9f", h)})
				}
			}
			alpha, err := svgAlpha(attributes["opacity"])
			if err != nil {
				return nil, err
			}
			if element.Name.Local == "g" || element.Name.Local == "svg" {
				if alpha != 1 {
					return nil, fmt.Errorf("group opacity is not supported; use fill-opacity and stroke-opacity on the paths")
				}
				continue
			}
			if element.Name.Local == "title" || element.Name.Local == "desc" {
				continue
			}
			for key, value := range attributes {
				switch key {
				case "x", "y", "x1", "x2", "y1", "y2", "cx", "cy", "r", "rx", "ry", "width", "height", "stroke-width":
					if strings.Contains(value, "%") {
						return nil, fmt.Errorf("percentage SVG geometry is not supported: %s", key)
					}
				}
			}
			if strings.Contains(state.styles["stroke-width"], "%") {
				return nil, fmt.Errorf("percentage SVG stroke width is not supported")
			}
			fillAlpha, err := svgAlpha(state.styles["fill-opacity"])
			if err != nil {
				return nil, err
			}
			strokeAlpha, err := svgAlpha(state.styles["stroke-opacity"])
			if err != nil {
				return nil, err
			}
			rule := state.styles["fill-rule"]
			if rule != "" && rule != "nonzero" && rule != "evenodd" {
				return nil, fmt.Errorf("unsupported SVG fill rule %q", rule)
			}
			shape := xml.StartElement{Name: xml.Name{Local: element.Name.Local}}
			for key, value := range state.styles {
				if key == "fill-opacity" || key == "stroke-opacity" || key == "fill-rule" {
					continue
				}
				if strings.EqualFold(value, "currentColor") {
					value = state.styles["color"]
					if value == "" {
						value = "#000000"
					}
				}
				if key == "fill" || key == "stroke" || key == "color" {
					if strings.EqualFold(value, "transparent") {
						value = "rgba(0,0,0,0)"
					} else if strings.EqualFold(value, "rebeccapurple") {
						value = "#663399"
					} else if c, ok := colornames.Map[strings.ToLower(value)]; ok {
						value = fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
					}
				}
				shape.Attr = append(shape.Attr, xml.Attr{Name: xml.Name{Local: key}, Value: value})
			}
			for key, value := range attributes {
				switch key {
				case "d", "points", "x", "y", "x1", "x2", "y1", "y2", "cx", "cy", "r", "rx", "ry", "width", "height":
					shape.Attr = append(shape.Attr, xml.Attr{Name: xml.Name{Local: key}, Value: value})
				}
			}
			if state.transform != "" {
				shape.Attr = append(shape.Attr, xml.Attr{Name: xml.Name{Local: "transform"}, Value: state.transform})
			}
			var b bytes.Buffer
			enc := xml.NewEncoder(&b)
			_ = enc.EncodeToken(root)
			_ = enc.EncodeToken(shape)
			_ = enc.EncodeToken(shape.End())
			_ = enc.EncodeToken(root.End())
			_ = enc.Flush()
			parsed, err := vector.ParseSVG(&b)
			if err != nil {
				return nil, err
			}
			parsed.RenderTo(styledCanvas{Canvas: out, fillAlpha: alpha * fillAlpha, strokeAlpha: alpha * strokeAlpha, evenOdd: rule == "evenodd"})
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	return out, nil
}
