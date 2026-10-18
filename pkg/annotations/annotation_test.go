package annotations

import (
	"image/color"
	"math"
	"strings"
	"testing"
)

func TestTextFamiliesStylesAndAccents(t *testing.T) {
	for _, font := range Fonts {
		for _, style := range []int{0, 1, 2, 3} {
			d := Default(Text)
			d.Text = "Café résumé: ÀÉÏÖÜ €"
			d.Font = font
			d.Bold = style&1 != 0
			d.Italic = style&2 != 0
			d.Underline = true
			d.Strikethrough = true
			asset, bounds, err := d.Artwork()
			if err != nil {
				t.Fatalf("%s style%d: %v", font, style, err)
			}
			if bounds.W() <= 0 || bounds.H() <= 0 || strings.Contains(string(asset.Data), "<text") {
				t.Fatal("text did not become vector outlines")
			}
			if _, err := asset.Prepare(); err != nil {
				t.Fatal(err)
			}
		}
	}
	d := Default(Text)
	d.Text = "a\nb"
	if _, _, err := d.Artwork(); err == nil {
		t.Fatal("multiline was accepted")
	}
	d.Text = "😀"
	if _, _, err := d.Artwork(); err == nil {
		t.Fatal("missing glyph silently substituted")
	}
	if SingleLine("one\r\ntwo\tthree") != "one two three" {
		t.Fatal("pasted newlines were not flattened")
	}
}
func TestHighlightOpacityAtCrossingsAndEdgeClipping(t *testing.T) {
	d := Default(Highlight)
	d.PageWidth = 100
	d.PageHeight = 100
	d.Points = []Point{{10, 10}, {90, 90}, {90, 10}, {10, 90}}
	asset, bounds, err := d.Artwork()
	if err != nil {
		t.Fatal(err)
	}
	picture, err := asset.Render(300)
	if err != nil {
		t.Fatal(err)
	}
	at := func(x, y float64) color.NRGBA {
		return color.NRGBAModel.Convert(picture.At(int((x-bounds.X0)/bounds.W()*float64(picture.Bounds().Dx())), int((y-bounds.Y0)/bounds.H()*float64(picture.Bounds().Dy())))).(color.NRGBA)
	}
	for _, p := range []Point{{50, 50}, {25, 25}, {90, 50}} {
		if c := at(p.X, p.Y); c.A != 255 {
			t.Fatalf("stroke alpha at %v = %d, want uniform 255", p, c.A)
		}
	}
	d.Points = []Point{{0, 0}, {100, 100}}
	_, b, err := d.Artwork()
	if err != nil || b.X0 != 0 || b.Y0 != 0 || b.X1 != 100 || b.Y1 != 100 {
		t.Fatalf("edge stroke was not clipped: %v %v", b, err)
	}
	d.Points = []Point{{50, 50}}
	if _, _, err := d.Artwork(); err != nil {
		t.Fatal("isolated dot lost", err)
	}
}
func TestProportionalResizeKeepsGeometryAndProperties(t *testing.T) {
	for _, kind := range []Kind{Text, Drawing, Highlight} {
		d := Default(kind)
		d.Text = "Proview"
		d.PageWidth = 200
		d.PageHeight = 100
		d.Points = []Point{{0, 20}, {120, 60}}
		_, b, err := d.Artwork()
		if err != nil {
			t.Fatal(err)
		}
		scaled := d.Scaled(2)
		_, s, err := scaled.Artwork()
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(s.W()-b.W()*2) > .1 || math.Abs(s.H()-b.H()*2) > .1 {
			t.Fatalf("%s resized geometry changed: %v => %v", kind, b, s)
		}
		if kind == Text && scaled.Size != d.Size*2 || kind != Text && scaled.Width != d.Width*2 {
			t.Fatal("properties did not scale")
		}
	}
}

func TestDrawingAndShapeStrokeStylesProduceDistinctVectorArtwork(t *testing.T) {
	for _, kind := range []Kind{Drawing, Shape} {
		data := Default(kind)
		data.PageWidth, data.PageHeight = 200, 100
		data.Points = []Point{{10, 20}, {190, 80}}
		var artwork []string
		for _, style := range []StrokeStyle{StrokeSolid, StrokeDashed, StrokeDotted} {
			data.StrokeStyle = style
			asset, _, err := data.Artwork()
			if err != nil {
				t.Fatalf("%s %s: %v", kind, style, err)
			}
			if _, err = asset.Prepare(); err != nil {
				t.Fatalf("%s %s preparation: %v", kind, style, err)
			}
			artwork = append(artwork, string(asset.Data))
		}
		if artwork[0] == artwork[1] || artwork[1] == artwork[2] || artwork[0] == artwork[2] {
			t.Fatalf("%s stroke styles produced identical artwork", kind)
		}
	}
}

func TestClosedDrawingStrokeDoesNotFillItsInterior(t *testing.T) {
	data := Default(Drawing)
	data.PageWidth, data.PageHeight = 100, 100
	data.Width = 4
	data.Points = []Point{{20, 20}, {80, 20}, {80, 80}, {20, 80}, {20, 20}}
	asset, bounds, err := data.Artwork()
	if err != nil {
		t.Fatal(err)
	}
	picture, err := asset.Render(400)
	if err != nil {
		t.Fatal(err)
	}
	x := int((50 - bounds.X0) / bounds.W() * float64(picture.Bounds().Dx()))
	y := int((50 - bounds.Y0) / bounds.H() * float64(picture.Bounds().Dy()))
	if _, _, _, alpha := picture.At(x, y).RGBA(); alpha != 0 {
		t.Fatalf("closed drawing filled its interior: alpha=%d", alpha)
	}
}
