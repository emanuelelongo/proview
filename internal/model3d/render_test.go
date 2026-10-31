package model3d

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
)

func TestRenderCameraAndWireframe(t *testing.T) {
	m, err := Load("triangle.stl", strings.NewReader(asciiTriangle))
	if err != nil {
		t.Fatal(err)
	}
	camera := Camera{Pitch: math.Pi / 2, Zoom: 1}
	first, err := Render(context.Background(), m, camera, 200, 160)
	if err != nil {
		t.Fatal(err)
	}
	colored := 0
	for i := 0; i < len(first.Pix); i += 4 {
		if first.Pix[i] != BackgroundColor.R || first.Pix[i+1] != BackgroundColor.G || first.Pix[i+2] != BackgroundColor.B {
			colored++
			if first.Pix[i] != first.Pix[i+1] || first.Pix[i] != first.Pix[i+2] {
				t.Fatal("model material is not white/grayscale")
			}
		}
	}
	if colored < 100 {
		t.Fatal("model was not drawn")
	}
	camera.Wireframe = true
	wire, err := Render(context.Background(), m, camera, 200, 160)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Pix, wire.Pix) {
		t.Fatal("wireframe did not change rendering")
	}
	camera.Wireframe = false
	camera.PanX = .2
	shifted, err := Render(context.Background(), m, camera, 200, 160)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Pix, shifted.Pix) {
		t.Fatal("camera pan did not change rendering")
	}
	// The pixel buffer belongs to each frame; subsequent renders cannot mutate it.
	previous := bytes.Clone(first.Pix)
	camera.Yaw = .2
	if _, err := Render(context.Background(), m, camera, 200, 160); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(previous, first.Pix) {
		t.Fatal("published frame mutated")
	}
}
func TestRenderCancellationAndInvalidViewport(t *testing.T) {
	m, _ := Load("triangle.stl", strings.NewReader(asciiTriangle))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Render(ctx, m, DefaultCamera(), 200, 160); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := Render(context.Background(), m, DefaultCamera(), 100000, 160); err == nil {
		t.Fatal("unbounded viewport accepted")
	}
}

func TestHighQualityAntialiasingAndLimits(t *testing.T) {
	m, err := Load("triangle.stl", strings.NewReader(asciiTriangle))
	if err != nil {
		t.Fatal(err)
	}
	camera := DefaultCamera()
	frame, err := RenderHighQuality(context.Background(), m, camera, 200, 160)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := Render(context.Background(), m, camera, 200, 160)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Bounds() != ordinary.Bounds() || bytes.Equal(frame.Pix, ordinary.Pix) {
		t.Fatal("supersampling did not improve edges at original dimensions")
	}
	mixed := false
	for i := 0; i < len(frame.Pix); i += 4 {
		// White material is gray; partially covered edge pixels mix with the tinted background.
		if frame.Pix[i] != BackgroundColor.R && frame.Pix[i] != frame.Pix[i+2] {
			mixed = true
		}
		if frame.Pix[i+3] != 255 {
			t.Fatal("snapshot is not opaque")
		}
	}
	if !mixed {
		t.Fatal("no antialiased edge pixels")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RenderHighQuality(ctx, m, camera, 200, 160); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := RenderHighQuality(context.Background(), m, camera, 2400, 2400); err == nil {
		t.Fatal("unbounded supersampling buffer accepted")
	}
}
func TestSmoothNormalsPreserveSharpEdges(t *testing.T) {
	m := &Mesh{Min: Vector{-1, -1, -1}, Max: Vector{1, 1, 1}, triangles: []Triangle{
		{Vertices: [3]Vector{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}},
		{Vertices: [3]Vector{{0, 0, 0}, {0, 1, 0}, {-1, 0, .2}}},
	}}
	m.prepareNormals()
	n := Vector{float64(m.packed[9]), float64(m.packed[10]), float64(m.packed[11])}
	if n.X <= 0 || n.Z <= 0 {
		t.Fatalf("shallow facets did not share a smooth normal: %+v", n)
	}
	// A cube corner must fail the renderer's smooth-normal threshold.
	cubeData, err := os.ReadFile("testdata/cube.stl")
	if err != nil {
		t.Fatal(err)
	}
	cube, err := Load("cube.stl", bytes.NewReader(cubeData))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < cube.TriangleCount(); i++ {
		offset := i * 21
		face := Vector{float64(cube.packed[offset+18]), float64(cube.packed[offset+19]), float64(cube.packed[offset+20])}
		for corner := range 3 {
			n := Vector{float64(cube.packed[offset+9+corner*3]), float64(cube.packed[offset+10+corner*3]), float64(cube.packed[offset+11+corner*3])}
			if face.Sub(n).Dot(face.Sub(n)) > 1e-8 {
				t.Fatal("sharp cube corner would be smoothed")
			}
		}

	}
}

func BenchmarkRenderCube(b *testing.B) {
	data, err := os.ReadFile("testdata/cube.stl")
	if err != nil {
		b.Fatal(err)
	}
	mesh, err := Load("cube.stl", bytes.NewReader(data))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Render(context.Background(), mesh, DefaultCamera(), 640, 480); err != nil {
			b.Fatal(err)
		}
	}
}
