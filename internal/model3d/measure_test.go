package model3d

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"
)

func TestPickUsesVisibleSurfaceAndOriginalCoordinates(t *testing.T) {
	m := &Mesh{Min: Vector{-2, -2, 0}, Max: Vector{2, 2, 2}, triangles: []Triangle{
		{Vertices: [3]Vector{{-2, -2, 0}, {2, -2, 0}, {0, 2, 0}}},
		{Vertices: [3]Vector{{-2, -2, 2}, {2, -2, 2}, {0, 2, 2}}},
	}}
	m.prepareNormals()
	c := Camera{Pitch: math.Pi / 2, Zoom: 1}
	p, ok := Pick(m, c, 400, 400, 200, 200)
	if !ok || math.Abs(p.Z-2) > 1e-8 || math.Abs(p.X) > 1e-8 || math.Abs(p.Y) > 1e-8 {
		t.Fatalf("picked %v, %t; want top surface at original origin", p, ok)
	}
	if _, ok := Pick(m, c, 400, 400, 0, 0); ok {
		t.Fatal("picked outside the mesh")
	}
	if got := Distance(Vector{}, Vector{3, 4, 12}); got != 13 {
		t.Fatalf("distance = %v", got)
	}
}

func TestPickMatchesProjectionAcrossViewsPanAndZoom(t *testing.T) {
	m := &Mesh{Min: Vector{-10, -10, -10}, Max: Vector{10, 10, 10}, triangles: []Triangle{
		{Vertices: [3]Vector{{-1, -1, 1}, {1, -1, 1}, {0, 1, 1}}},
		{Vertices: [3]Vector{{-1, 1, -1}, {1, 1, -1}, {0, 1, 1}}},
		{Vertices: [3]Vector{{1, -1, -1}, {1, 1, -1}, {1, 0, 1}}},
	}}
	m.prepareNormals()
	views := []struct {
		name   string
		camera Camera
		point  Vector
	}{
		{"top", Camera{Pitch: math.Pi / 2, Zoom: 3, PanX: .1, PanY: -.08}, Vector{0, 0, 1}},
		{"front", Camera{Zoom: 3, PanX: -.07, PanY: .12}, Vector{0, 1, 0}},
		{"right", Camera{Yaw: math.Pi / 2, Zoom: 3, PanX: .04, PanY: .06}, Vector{1, 0, 0}},
	}
	for _, view := range views {
		projected := Project(m, view.camera, view.point, 600, 480)
		picked, ok := Pick(m, view.camera, 600, 480, projected.X, projected.Y)
		if !ok || Distance(picked, view.point) > 1e-6 {
			t.Fatalf("%s: picked %v, %t; want %v", view.name, picked, ok, view.point)
		}
	}
}

func TestGridUsesAdaptivePhysicalSpacingAndBackground(t *testing.T) {
	m := &Mesh{Min: Vector{-5, -5, 0}, Max: Vector{5, 5, 1}, triangles: []Triangle{
		{Vertices: [3]Vector{{-5, -5, 1}, {5, -5, 1}, {0, 5, 1}}},
	}}
	m.prepareNormals()
	c := Camera{Pitch: math.Pi / 2, Zoom: 1}
	step := GridStep(m, c, 400, 400)
	if step <= 0 || FormatMetricLength(step) != "2 mm" {
		t.Fatalf("grid step = %v", step)
	}
	c.Zoom = .1
	zoomedStep := GridStep(m, c, 400, 400)
	if zoomedStep <= step || FormatMetricLength(zoomedStep) != "2 cm" {
		t.Fatalf("zoomed grid step = %v", zoomedStep)
	}
	c.Zoom = 1
	frame, err := Render(context.Background(), m, c, 400, 400)
	if err != nil {
		t.Fatal(err)
	}
	center := frame.RGBAAt(200, 200)
	DrawGrid(frame, m, c, "Top")
	if frame.RGBAAt(200, 200) != center {
		t.Fatal("grid painted over visible model surface")
	}
	changed := false
	for y := range 400 {
		for x := range 400 {
			if pixel := frame.RGBAAt(x, y); (pixel.R == 59 && pixel.G == 69 && pixel.B == 78) || (pixel.R == 78 && pixel.G == 91 && pixel.B == 100) {
				changed = true
				break
			}
		}
		if changed {
			break
		}
	}
	if !changed {
		t.Fatal("grid did not appear in the background")
	}
}

func TestMetricLengthSwitchesUnits(t *testing.T) {
	for _, test := range []struct {
		mm   float64
		want string
	}{{9, "9 mm"}, {10, "1 cm"}, {250, "25 cm"}, {1000, "1 m"}, {2500, "2.5 m"}} {
		if got := FormatMetricLength(test.mm); got != test.want {
			t.Fatalf("%v mm = %q, want %q", test.mm, got, test.want)
		}
	}
}

func TestMajorGridLinesAreThickerAndStayBehindModel(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := range 20 {
		for x := range 20 {
			frame.Set(x, y, BackgroundColor)
		}
	}
	frame.Set(10, 10, color.RGBA{255, 255, 255, 255})
	gridLine(frame, Vector{5, 2, 0}, Vector{5, 17, 0}, [3]byte{59, 69, 78}, false)
	gridLine(frame, Vector{10, 2, 0}, Vector{10, 17, 0}, [3]byte{78, 91, 100}, true)
	if frame.RGBAAt(4, 5) != BackgroundColor || frame.RGBAAt(9, 5) == BackgroundColor ||
		frame.RGBAAt(11, 5) == BackgroundColor {
		t.Fatal("major grid line is not thicker than a minor line")
	}
	if frame.RGBAAt(10, 10) != (color.RGBA{255, 255, 255, 255}) {
		t.Fatal("major grid line painted over the mesh")
	}
}

func TestMeasurementSnapsToCornersAndBoundaryButNotFlatSeams(t *testing.T) {
	m := &Mesh{Min: Vector{-1, -1, 0}, Max: Vector{1, 1, 0}, triangles: []Triangle{
		{Vertices: [3]Vector{{-1, -1, 0}, {1, -1, 0}, {1, 1, 0}}},
		{Vertices: [3]Vector{{-1, -1, 0}, {1, 1, 0}, {-1, 1, 0}}},
	}}
	m.prepareNormals()
	c := Camera{Pitch: math.Pi / 2, Zoom: 1}
	const w, h = 400, 400
	corner := Vector{-1, -1, 0}
	cornerPixel := Project(m, c, corner, w, h)
	centerPixel := Project(m, c, Vector{}, w, h)
	dx, dy := centerPixel.X-cornerPixel.X, centerPixel.Y-cornerPixel.Y
	length := math.Hypot(dx, dy)
	clicked := Vector{cornerPixel.X + 6*dx/length, cornerPixel.Y + 6*dy/length, 0}
	got, ok := PickSnapped(m, c, w, h, clicked.X, clicked.Y, 1)
	if !ok || Distance(got, corner) > 1e-6 {
		t.Fatalf("corner snap = %v, %t", got, ok)
	}
	edgePixel := Project(m, c, Vector{0, -1, 0}, w, h)
	dx, dy = centerPixel.X-edgePixel.X, centerPixel.Y-edgePixel.Y
	length = math.Hypot(dx, dy)
	clicked = Vector{edgePixel.X + 5*dx/length, edgePixel.Y + 5*dy/length, 0}
	got, ok = PickSnapped(m, c, w, h, clicked.X, clicked.Y, 1)
	if !ok || math.Abs(got.Y+1) > 1e-6 {
		t.Fatalf("boundary edge snap = %v, %t", got, ok)
	}
	farther := Vector{edgePixel.X + 14*dx/length, edgePixel.Y + 14*dy/length, 0}
	withoutScale, _ := PickSnapped(m, c, w, h, farther.X, farther.Y, 1)
	withScale, ok := PickSnapped(m, c, w, h, farther.X, farther.Y, .5)
	if !ok || math.Abs(withScale.Y+1) > 1e-6 || math.Abs(withoutScale.Y+1) < 1e-3 {
		t.Fatalf("display scale did not preserve the visible snap radius: %v versus %v", withScale, withoutScale)
	}
	plain, _ := Pick(m, c, w, h, centerPixel.X+2, centerPixel.Y)
	got, ok = PickSnapped(m, c, w, h, centerPixel.X+2, centerPixel.Y, 1)
	if !ok || Distance(got, plain) > 1e-6 {
		t.Fatalf("flat triangulation seam attracted measurement: %v versus %v", got, plain)
	}
}

func TestMeasurementSnapsToSharpSharedEdge(t *testing.T) {
	m := &Mesh{Min: Vector{-1, -1, 0}, Max: Vector{1, 1, 1}, triangles: []Triangle{
		{Vertices: [3]Vector{{-1, -1, 0}, {1, -1, 0}, {0, 1, 0}}},
		{Vertices: [3]Vector{{-1, -1, 0}, {1, -1, 0}, {0, -1, 1}}},
	}}
	m.prepareNormals()
	c := Camera{Pitch: math.Pi / 2, Zoom: 1}
	const w, h = 400, 400
	edge := Project(m, c, Vector{0, -1, 0}, w, h)
	inside := Project(m, c, Vector{0, 0, 0}, w, h)
	dx, dy := inside.X-edge.X, inside.Y-edge.Y
	length := math.Hypot(dx, dy)
	point, ok := PickSnapped(m, c, w, h, edge.X+5*dx/length, edge.Y+5*dy/length, 1)
	if !ok || math.Abs(point.Y+1) > 1e-6 {
		t.Fatalf("sharp shared edge did not attract measurement: %v, %t", point, ok)
	}
}
