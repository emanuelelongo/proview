// Package annotations renders editable text and strokes as vector artwork.
package annotations

import (
	"fmt"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/unicode/norm"
	"image/color"
	"math"
	"strings"
	"sync"
	"unicode"

	"emanuelelongo/proview/pkg/signatures"
	"github.com/go-fonts/latin-modern/lmroman10bold"
	"github.com/go-fonts/latin-modern/lmroman10bolditalic"
	"github.com/go-fonts/latin-modern/lmroman10italic"
	"github.com/go-fonts/latin-modern/lmroman10regular"
	vector "github.com/tdewolff/canvas"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
)

type Kind string
type StrokeStyle string

const (
	Text         Kind        = "text"
	Drawing      Kind        = "drawing"
	Highlight    Kind        = "highlight"
	Shape        Kind        = "shape"
	StrokeSolid  StrokeStyle = "solid"
	StrokeRound  StrokeStyle = "round"
	StrokeDashed StrokeStyle = "dashed"
	StrokeDotted StrokeStyle = "dotted"
)

var Fonts = []string{"Go Sans", "Go Mono", "Latin Modern Roman"}

type Point struct{ X, Y float64 }
type Quad [4]Point

// Coordinates and sizes use points for PDF pages and original pixels for images.
type Data struct {
	Shape                                  ShapeKind
	StrokeStyle                            StrokeStyle
	Filled                                 bool
	ArrowClosed                            bool
	Magnification                          float64
	Kind                                   Kind
	Text, Font                             string
	Size                                   float64
	Bold, Italic, Underline, Strikethrough bool
	Color                                  color.NRGBA
	Width, Opacity                         float64
	Points                                 []Point
	Quads                                  []Quad
	PageWidth, PageHeight                  float64
	Padding                                float64
}

func (d Data) Clone() Data {
	d.Points = append([]Point(nil), d.Points...)
	d.Quads = append([]Quad(nil), d.Quads...)
	return d
}
func Default(kind Kind) Data {
	d := Data{Kind: kind, Font: Fonts[0], Size: 18, Color: color.NRGBA{A: 255}, Width: 2, Opacity: 1, Padding: 1, StrokeStyle: StrokeSolid}
	if kind == Drawing {
		d.StrokeStyle = StrokeRound
	}
	if kind == Shape {
		d.Shape = Rectangle
		d.Magnification = 2
	}
	if kind == Highlight {
		d.Color = color.NRGBA{R: 255, G: 235, A: 255}
		d.Width = 12
		d.Opacity = 1
	}
	return d
}
func SingleLine(s string) string {
	return norm.NFC.String(strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '\r' || r == '\n' || r == '\t' }), " "))
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func (d Data) Validate() error {
	if !finite(d.Padding) || d.Padding <= 0 {
		return fmt.Errorf("Invalid padding")
	}
	if d.Color.A != 255 {
		return fmt.Errorf("Choose an opaque color; use the opacity control")
	}
	if d.StrokeStyle != "" && d.StrokeStyle != StrokeSolid && d.StrokeStyle != StrokeDashed && d.StrokeStyle != StrokeDotted && (d.StrokeStyle != StrokeRound || d.Kind != Drawing) {
		return fmt.Errorf("Invalid stroke style")
	}
	if d.Kind == Text {
		if strings.TrimSpace(d.Text) == "" || strings.ContainsAny(d.Text, "\r\n\t") || len([]rune(d.Text)) > 2000 {
			return fmt.Errorf("Enter one line of text (maximum 2000 characters)")
		}
		if !finite(d.Size) || d.Size < 6 || d.Size > 144 {
			return fmt.Errorf("Text size must be between 6 and 144")
		}
		return nil
	}
	if d.Kind == Shape {
		if !finite(d.Width) || d.Width < .5 || d.Width > 40 || (d.Shape != Polygon && len(d.Points) != 2) || (d.Shape == Polygon && (len(d.Points) < 3 || len(d.Points) > 100000)) {
			return fmt.Errorf("Invalid shape geometry")
		}
		if !finite(d.Magnification) || d.Magnification < 1 || d.Magnification > 8 {
			return fmt.Errorf("Magnification must be between 1 and 8")
		}
		for _, p := range d.Points {
			if !finite(p.X) || !finite(p.Y) {
				return fmt.Errorf("Invalid shape coordinates")
			}
		}
		return nil
	}
	if d.Kind != Drawing && d.Kind != Highlight {
		return fmt.Errorf("Invalid object type")
	}
	if d.Kind == Highlight && len(d.Quads) > 0 {
		if len(d.Quads) > 100000 || !finite(d.PageWidth) || !finite(d.PageHeight) || d.PageWidth <= 0 || d.PageHeight <= 0 {
			return fmt.Errorf("Invalid text highlight geometry")
		}
		for _, q := range d.Quads {
			area := 0.
			for i, p := range q {
				if !finite(p.X) || !finite(p.Y) || p.X < 0 || p.Y < 0 || p.X > d.PageWidth || p.Y > d.PageHeight {
					return fmt.Errorf("Highlight outside the page")
				}
				next := q[(i+1)%4]
				area += p.X*next.Y - next.X*p.Y
			}
			if math.Abs(area) < .000001 {
				return fmt.Errorf("Empty text highlight")
			}
		}
		return nil
	}
	low, high := .5, 40.
	if d.Kind == Highlight {
		low, high = 2, 80
	}
	if !finite(d.Width) || d.Width < low || d.Width > high {
		return fmt.Errorf("Stroke width must be between %g and %g", low, high)
	}
	if !finite(d.Opacity) || d.Opacity < .05 || d.Opacity > 1 {
		return fmt.Errorf("Invalid opacity")
	}
	if !finite(d.PageWidth) || !finite(d.PageHeight) || d.PageWidth <= 0 || d.PageHeight <= 0 || len(d.Points) == 0 || len(d.Points) > 100000 {
		return fmt.Errorf("Invalid stroke geometry")
	}
	for _, p := range d.Points {
		if !finite(p.X) || !finite(p.Y) || p.X < 0 || p.Y < 0 || p.X > d.PageWidth || p.Y > d.PageHeight {
			return fmt.Errorf("Point outside the page")
		}
	}
	return nil
}

var fontMu sync.Mutex
var families = map[string][]*sfnt.Font{}

func family(name string, style int) (*sfnt.Font, error) {
	if f := families[name]; f != nil {
		return f[style], nil
	}
	data, err := fontSources(name)
	if err != nil {
		return nil, err
	}
	faces := make([]*sfnt.Font, len(data))
	for i, b := range data {
		f, err := sfnt.Parse(b)
		if err != nil {
			return nil, err
		}
		faces[i] = f
	}
	families[name] = faces
	return faces[style], nil
}

// FontData returns an independent copy of the bundled face used for text artwork.
func FontData(name string, bold, italic bool) ([]byte, error) {
	faces, err := fontSources(name)
	if err != nil {
		return nil, err
	}
	style := 0
	if bold {
		style |= 1
	}
	if italic {
		style |= 2
	}
	return append([]byte(nil), faces[style]...), nil
}

func fontSources(name string) ([][]byte, error) {
	switch name {
	case "Go Sans":
		return [][]byte{goregular.TTF, gobold.TTF, goitalic.TTF, gobolditalic.TTF}, nil
	case "Go Mono":
		return [][]byte{gomono.TTF, gomonobold.TTF, gomonoitalic.TTF, gomonobolditalic.TTF}, nil
	case "Latin Modern Roman":
		return [][]byte{lmroman10regular.TTF, lmroman10bold.TTF, lmroman10italic.TTF, lmroman10bolditalic.TTF}, nil
	default:
		return nil, fmt.Errorf("Font unavailable: %s", name)
	}
}

func textPath(d Data) (*vector.Path, float64, error) {
	style := 0
	if d.Bold {
		style |= 1
	}
	if d.Italic {
		style |= 2
	}
	f, err := family(d.Font, style)
	if err != nil {
		return nil, 0, err
	}
	var buffer sfnt.Buffer
	size := fixed.Int26_6(math.Round(d.Size * 64))
	path := &vector.Path{}
	advance := 0.
	var previous sfnt.GlyphIndex
	for _, r := range d.Text {
		if unicode.IsControl(r) {
			return nil, 0, fmt.Errorf("Unsupported control character")
		}
		glyph, err := f.GlyphIndex(&buffer, r)
		if err != nil {
			return nil, 0, err
		}
		if glyph == 0 {
			return nil, 0, fmt.Errorf("Font %s does not contain character %q", d.Font, r)
		}
		if previous != 0 {
			kern, err := f.Kern(&buffer, previous, glyph, size, font.HintingNone)
			if err != nil && err != sfnt.ErrNotFound {
				return nil, 0, err
			}
			advance += float64(kern) / 64
		}
		segments, err := f.LoadGlyph(&buffer, glyph, size, nil)
		if err != nil {
			return nil, 0, err
		}
		open := false
		for _, segment := range segments {
			point := func(i int) (float64, float64) {
				return advance + float64(segment.Args[i].X)/64, float64(segment.Args[i].Y) / 64
			}
			switch segment.Op {
			case sfnt.SegmentOpMoveTo:
				if open {
					path.Close()
				}
				x, y := point(0)
				path.MoveTo(x, y)
				open = true
			case sfnt.SegmentOpLineTo:
				x, y := point(0)
				path.LineTo(x, y)
			case sfnt.SegmentOpQuadTo:
				x, y := point(0)
				u, v := point(1)
				path.QuadTo(x, y, u, v)
			case sfnt.SegmentOpCubeTo:
				x, y := point(0)
				u, v := point(1)
				a, b := point(2)
				path.CubeTo(x, y, u, v, a, b)
			}
		}
		if open {
			path.Close()
		}
		width, err := f.GlyphAdvance(&buffer, glyph, size, font.HintingNone)
		if err != nil {
			return nil, 0, err
		}
		advance += float64(width) / 64
		previous = glyph
	}
	if d.Underline {
		path = path.Append(vector.Rectangle(advance, d.Size*.05).Translate(0, d.Size*.1))
	}
	if d.Strikethrough {
		path = path.Append(vector.Rectangle(advance, d.Size*.05).Translate(0, -d.Size*.3))
	}
	return path, advance, nil
}

// Artwork builds one filled path per stroke so opacity is applied once even at
// joins and self intersections. Text is exported as glyph outlines.
func (d Data) Artwork() (signatures.Asset, vector.Rect, error) {
	return d.artwork(nil)
}

// TextMetrics locates the glyph origin and baseline within padded artwork.
type TextMetrics struct{ Left, Baseline float64 }

// TextArtwork shares the exact outlines and bounds used for insertion/export.
func (d Data) TextArtwork() (signatures.Asset, vector.Rect, TextMetrics, error) {
	var metrics TextMetrics
	asset, bounds, err := d.artwork(&metrics)
	return asset, bounds, metrics, err
}

func (d Data) artwork(metrics *TextMetrics) (asset signatures.Asset, bounds vector.Rect, err error) {
	if err = d.Validate(); err != nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("Cannot prepare object: %v", r)
		}
	}()
	if d.Kind == Shape {
		return d.shapeArtwork()
	}
	var path *vector.Path
	alpha := 1.
	if d.Kind == Text {
		d.Text = norm.NFC.String(d.Text)
		fontMu.Lock()
		defer fontMu.Unlock()
		var advance float64
		path, advance, err = textPath(d)
		if err != nil {
			return
		}
		b := path.Bounds()
		left := math.Min(0, b.X0)
		bounds = vector.Rect{X0: 0, Y0: 0, X1: math.Max(advance, b.X1) - left + 2*d.Padding, Y1: b.H() + 2*d.Padding}
		if metrics != nil {
			*metrics = TextMetrics{Left: d.Padding - left, Baseline: d.Padding - b.Y0}
		}
		path = path.Translate(d.Padding-left, d.Padding-b.Y0)
	} else if d.Kind == Highlight && len(d.Quads) > 0 {
		path = &vector.Path{}
		for _, q := range d.Quads {
			path.MoveTo(q[0].X, q[0].Y)
			for _, p := range q[1:] {
				path.LineTo(p.X, p.Y)
			}
			path.Close()
		}
		path = path.Settle(vector.NonZero)
		bounds = path.Bounds().Expand(d.Padding)
		bounds.X0 = math.Max(0, bounds.X0)
		bounds.Y0 = math.Max(0, bounds.Y0)
		bounds.X1 = math.Min(d.PageWidth, bounds.X1)
		bounds.Y1 = math.Min(d.PageHeight, bounds.Y1)
		alpha = d.Opacity
	} else {
		path = &vector.Path{}
		path.MoveTo(d.Points[0].X, d.Points[0].Y)
		for _, p := range d.Points[1:] {
			path.LineTo(p.X, p.Y)
		}
		if len(d.Points) == 1 {
			path = vector.Circle(d.Width/2).Translate(d.Points[0].X, d.Points[0].Y)
		} else {
			path = d.dashPath(path)
			if d.Kind == Drawing {
				path = roundStrokePath(path, d.Width)
			} else {
				path = path.Stroke(d.Width, vector.RoundCap, vector.RoundJoin, .05*d.Padding)
			}
		}
		bounds = path.Bounds().Expand(d.Padding)
		bounds.X0 = math.Max(0, bounds.X0)
		bounds.Y0 = math.Max(0, bounds.Y0)
		bounds.X1 = math.Min(d.PageWidth, bounds.X1)
		bounds.Y1 = math.Min(d.PageHeight, bounds.Y1)
		alpha = d.Opacity
		if d.Kind == Highlight {
			alpha = d.Opacity
		}
	}
	if bounds.W() <= 0 || bounds.H() <= 0 {
		err = fmt.Errorf("Empty object")
		return
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="%.8f %.8f %.8f %.8f"><path fill="#%02x%02x%02x" fill-opacity="%.8f" d="%s"/></svg>`, bounds.X0, bounds.Y0, bounds.W(), bounds.H(), d.Color.R, d.Color.G, d.Color.B, alpha, path.ToSVG())
	asset = signatures.Asset{Kind: signatures.SVG, Data: []byte(svg)}
	return
}

func (d Data) dashPath(path *vector.Path) *vector.Path {
	switch d.StrokeStyle {
	case StrokeDashed:
		return path.Dash(0, 4*d.Width, 2*d.Width)
	case StrokeDotted:
		return path.Dash(0, .01, 2*d.Width)
	default:
		return path
	}
}

// Scaled bakes a proportional resize into editable properties and geometry.
func (d Data) Scaled(factor float64) Data {
	d = d.Clone()
	d.Padding *= factor
	if d.Kind == Text {
		d.Size *= factor
	} else {
		d.Width *= factor
		d.PageWidth *= factor
		d.PageHeight *= factor
		for i := range d.Quads {
			for j := range d.Quads[i] {
				d.Quads[i][j].X *= factor
				d.Quads[i][j].Y *= factor
			}
		}
		for i := range d.Points {
			d.Points[i].X *= factor
			d.Points[i].Y *= factor
		}
	}
	return d
}
