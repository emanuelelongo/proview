package annotations

import (
	"math"
	"math/rand"
	"testing"
)

func TestDenseSelfIntersectingDrawingStaysWithinStroke(t *testing.T) {
	const seed = int64(0)
	rng := rand.New(rand.NewSource(seed))
	data := Default(Drawing)
	data.PageWidth, data.PageHeight = 200, 200
	data.Width = 3
	data.Points = []Point{{100, 100}}
	for i := 0; i < 80; i++ {
		previous := data.Points[len(data.Points)-1]
		angle := rng.Float64() * 2 * math.Pi
		length := 4 + rng.Float64()*18
		data.Points = append(data.Points, Point{
			X: math.Max(5, math.Min(195, previous.X+math.Cos(angle)*length)),
			Y: math.Max(5, math.Min(195, previous.Y+math.Sin(angle)*length)),
		})
	}
	asset, bounds, err := data.Artwork()
	if err != nil {
		t.Fatal(err)
	}
	picture, err := asset.Render(500)
	if err != nil {
		t.Fatal(err)
	}
	pixelWorld := math.Max(bounds.W()/float64(picture.Bounds().Dx()), bounds.H()/float64(picture.Bounds().Dy()))
	limit := data.Width/2 + math.Max(8, 2*pixelWorld)
	for y := picture.Bounds().Min.Y; y < picture.Bounds().Max.Y; y++ {
		for x := picture.Bounds().Min.X; x < picture.Bounds().Max.X; x++ {
			_, _, _, alpha := picture.At(x, y).RGBA()
			if alpha < 0x8000 {
				continue
			}
			point := Point{
				X: bounds.X0 + (float64(x-picture.Bounds().Min.X)+.5)*bounds.W()/float64(picture.Bounds().Dx()),
				Y: bounds.Y0 + (float64(y-picture.Bounds().Min.Y)+.5)*bounds.H()/float64(picture.Bounds().Dy()),
			}
			nearest := math.Inf(1)
			for i := 1; i < len(data.Points); i++ {
				nearest = math.Min(nearest, pointSegmentDistance(point, data.Points[i-1], data.Points[i]))
			}
			if nearest > limit {
				t.Fatalf("created pixel (%g,%g) distance %g from stroke", point.X, point.Y, nearest)
			}
		}
	}
	for i := 1; i < len(data.Points); i++ {
		for step := 0; step <= 10; step++ {
			fraction := float64(step) / 10
			point := Point{X: data.Points[i-1].X + fraction*(data.Points[i].X-data.Points[i-1].X), Y: data.Points[i-1].Y + fraction*(data.Points[i].Y-data.Points[i-1].Y)}
			x := int((point.X - bounds.X0) / bounds.W() * float64(picture.Bounds().Dx()))
			y := int((point.Y - bounds.Y0) / bounds.H() * float64(picture.Bounds().Dy()))
			visible := false
			for py := max(0, y-2); py <= min(picture.Bounds().Dy()-1, y+2); py++ {
				for px := max(0, x-2); px <= min(picture.Bounds().Dx()-1, x+2); px++ {
					_, _, _, alpha := picture.At(px, py).RGBA()
					visible = visible || alpha >= 0x8000
				}
			}
			if !visible {
				t.Fatalf("lost stroke at %#v", point)
			}
		}
	}
}

func pointSegmentDistance(p, a, b Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	t := 0.0
	if length := dx*dx + dy*dy; length > 0 {
		t = math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/length))
	}
	return math.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}
