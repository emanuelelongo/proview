package model3d

import (
	"context"
	"math"
	"testing"
)

func TestPickUsesVisibleSurfaceAndOriginalCoordinates(t *testing.T) {
	m := &Mesh{Min: Vector{-2, -2, 0}, Max: Vector{2, 2, 2}, Triangles: []Triangle{
		{Vertices: [3]Vector{{-2, -2, 0}, {2, -2, 0}, {0, 2, 0}}},
		{Vertices: [3]Vector{{-2, -2, 2}, {2, -2, 2}, {0, 2, 2}}},
	}}
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
	m := &Mesh{Min: Vector{-10, -10, -10}, Max: Vector{10, 10, 10}, Triangles: []Triangle{
		{Vertices: [3]Vector{{-1, -1, 1}, {1, -1, 1}, {0, 1, 1}}},
		{Vertices: [3]Vector{{-1, 1, -1}, {1, 1, -1}, {0, 1, 1}}},
		{Vertices: [3]Vector{{1, -1, -1}, {1, 1, -1}, {1, 0, 1}}},
	}}
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
		if !ok || Distance(picked, view.point) > 1e-8 {
			t.Fatalf("%s: picked %v, %t; want %v", view.name, picked, ok, view.point)
		}
	}
}

func TestGridUsesAdaptivePhysicalSpacingAndBackground(t *testing.T) {
	m := &Mesh{Min: Vector{-5, -5, 0}, Max: Vector{5, 5, 1}, Triangles: []Triangle{
		{Vertices: [3]Vector{{-5, -5, 1}, {5, -5, 1}, {0, 5, 1}}},
	}}
	c := Camera{Pitch: math.Pi / 2, Zoom: 1}
	step, label := GridStep(m, c, 400, 400, UnitMillimeter)
	if step <= 0 || label != "mm" {
		t.Fatalf("grid step = %v %q", step, label)
	}
	cmStep, cmLabel := GridStep(m, c, 400, 400, UnitCentimeter)
	if cmStep <= 0 || cmLabel != "mm" {
		t.Fatalf("centimeter source grid = %v %q", cmStep, cmLabel)
	}
	frame, err := Render(context.Background(), m, c, 400, 400)
	if err != nil {
		t.Fatal(err)
	}
	center := frame.RGBAAt(200, 200)
	DrawGrid(frame, m, c, UnitMillimeter, "Top")
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
