package annotations

import (
	"image/color"
	"math"
	"testing"
)

func TestArrowHasSolidShaftAndFixedOpenHead(t *testing.T) {
	for _, length := range []float64{40, 400} {
		d := Default(Shape)
		d.Shape, d.Filled = Arrow, true // Fill never turns a simple arrow into a polygon.
		d.Points = []Point{{X: 20, Y: 30}, {X: 20 + length, Y: 30}}
		asset, bounds, err := d.Artwork()
		if err != nil {
			t.Fatal(err)
		}
		picture, err := asset.Render(int(bounds.W() * 4))
		if err != nil {
			t.Fatal(err)
		}
		alpha := func(x, y float64) uint8 {
			px := int((x - bounds.X0) / bounds.W() * float64(picture.Bounds().Dx()))
			py := int((y - bounds.Y0) / bounds.H() * float64(picture.Bounds().Dy()))
			return color.NRGBAModel.Convert(picture.At(px, py)).(color.NRGBA).A
		}
		if alpha(20+length/2, 30) < 240 {
			t.Fatal("arrow shaft has a hollow center")
		}
		if alpha(20+length-8, 34) < 240 || alpha(20+length-8, 32) > 10 {
			t.Fatal("arrow head is not a fixed open chevron")
		}
		if length > 100 && alpha(20+length-30, 38) > 10 {
			t.Fatal("arrow head grew with shaft length")
		}
	}
}

func TestLineSupportsHorizontalVerticalAndReverseGestures(t *testing.T) {
	for _, end := range []Point{{X: 150, Y: 20}, {X: 20, Y: 150}, {X: -50, Y: 20}, {X: 20, Y: -50}} {
		d := Default(Shape)
		d.Shape = Line
		d.Points = []Point{{X: 20, Y: 20}, end}
		asset, bounds, err := d.Artwork()
		if err != nil || bounds.W() <= 0 || bounds.H() <= 0 {
			t.Fatal("valid line rejected", end, err)
		}
		if _, err := asset.Prepare(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestShapeResizeRegeneratesBoundsWithoutScalingStroke(t *testing.T) {
	for _, name := range Shapes {
		for _, style := range []StrokeStyle{StrokeSolid, StrokeDashed, StrokeDotted} {
			for _, filled := range []bool{false, true} {
				t.Run(name+"/"+string(style)+"/"+map[bool]string{false: "outline", true: "filled"}[filled], func(t *testing.T) {
					d := Default(Shape)
					d.Shape, d.StrokeStyle, d.Filled = ShapeKind(name), style, filled
					d.Points = []Point{{X: 200, Y: 160}, {X: 20, Y: 40}}
					_, original, err := d.Artwork()
					if err != nil {
						t.Fatal(err)
					}
					width, height := original.W()*1.7, original.H()*.65
					if d.Shape == Magnifier {
						height = original.H() * 1.7
					}
					resized, err := d.ResizeShape(width, height)
					if err != nil {
						t.Fatal(err)
					}
					_, bounds, err := resized.Artwork()
					if err != nil || math.Abs(bounds.W()-width) > .001 || math.Abs(bounds.H()-height) > .001 {
						t.Fatal("regenerated shape does not fit requested size", bounds, err)
					}
					if resized.Width != d.Width || resized.Padding != d.Padding || d.Points[1] != (Point{X: 20, Y: 40}) {
						t.Fatal("resize changed physical stroke or original geometry")
					}
				})
			}
		}
	}
}
