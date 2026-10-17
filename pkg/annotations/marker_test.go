package annotations

import (
	"bytes"
	"image/color"
	"math"
	"testing"
)

func TestFlatNibIsDirectionalAndKeepsStrokeOpacity(t *testing.T) {
	var areas []float64
	for _, end := range []Point{{140, 40}, {140, 140}} {
		data := Default(Drawing)
		data.StrokeStyle = StrokeSolid
		data.Width = 20
		data.Opacity = .3
		data.PageWidth = 200
		data.PageHeight = 200
		data.Points = []Point{{40, 140}, end, {40, 140}, end}
		asset, _, err := data.Artwork()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(asset.Data, []byte(`fill-opacity="0.30000000"`)) {
			t.Fatal("Draw opacity missing from vector output")
		}
		picture, err := asset.Render(400)
		if err != nil {
			t.Fatal(err)
		}
		maxAlpha := uint8(0)
		area := 0.
		for y := 0; y < picture.Bounds().Dy(); y++ {
			for x := 0; x < picture.Bounds().Dx(); x++ {
				c := color.NRGBAModel.Convert(picture.At(x, y)).(color.NRGBA)
				area += float64(c.A)
				if c.A > maxAlpha {
					maxAlpha = c.A
				}
			}
		}
		if math.Abs(float64(maxAlpha)-77) > 2 {
			t.Fatalf("flat nib crossing alpha=%d", maxAlpha)
		}
		polygon := MarkerHull(data.Points[0], end, 20)
		logicalArea := 0.
		for i, p := range polygon {
			q := polygon[(i+1)%len(polygon)]
			logicalArea += p.X*q.Y - q.X*p.Y
		}
		areas = append(areas, math.Abs(logicalArea)/2)
		if area == 0 {
			t.Fatal("empty marker")
		}
	}
	if areas[1] <= areas[0]*2 {
		t.Fatal("flat nib did not vary width with movement direction")
	}
}

func TestTextHighlightGeometryCloneScaleAndUniformAlpha(t *testing.T) {
	data := Default(Highlight)
	data.PageWidth = 200
	data.PageHeight = 100
	data.Quads = []Quad{{{10, 10}, {90, 10}, {90, 30}, {10, 30}}, {{50, 10}, {120, 10}, {120, 30}, {50, 30}}}
	clone := data.Clone()
	clone.Quads[0][0].X = 0
	if data.Quads[0][0].X != 10 {
		t.Fatal("highlight clone aliases geometry")
	}
	scaled := data.Scaled(2)
	if scaled.Quads[0][0].X != 20 || scaled.PageWidth != 400 {
		t.Fatal("highlight geometry did not scale")
	}
	asset, bounds, err := data.Artwork()
	if err != nil {
		t.Fatal(err)
	}
	picture, err := asset.Render(600)
	if err != nil {
		t.Fatal(err)
	}
	at := func(x, y float64) uint8 {
		return color.NRGBAModel.Convert(picture.At(int((x-bounds.X0)/bounds.W()*float64(picture.Bounds().Dx())), int((y-bounds.Y0)/bounds.H()*float64(picture.Bounds().Dy())))).(color.NRGBA).A
	}
	if math.Abs(float64(at(30, 20))-float64(at(70, 20))) > 1 || math.Abs(float64(at(70, 20))-77) > 2 {
		t.Fatal("overlapping highlight regions accumulated opacity")
	}
}
