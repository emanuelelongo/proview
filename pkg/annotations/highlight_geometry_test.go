package annotations

import (
	"image/color"
	"math"
	"testing"
)

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
	if math.Abs(float64(at(30, 20))-float64(at(70, 20))) > 1 || math.Abs(float64(at(70, 20))-255) > 2 {
		t.Fatal("overlapping highlight regions accumulated opacity")
	}
}
