package modelgpu

import (
	"bytes"
	"context"
	"emanuelelongo/proview/internal/model3d"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func meshFixture(t *testing.T) *model3d.Mesh {
	t.Helper()
	data, err := os.ReadFile("../model3d/testdata/cube.stl")
	if err != nil {
		t.Fatal(err)
	}
	mesh, err := model3d.Load("cube.stl", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return mesh
}
func closeRenderer(t *testing.T, r *Renderer) {
	t.Helper()
	r.Close()
	r.Close()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("renderer did not release its worker/context")
	}
}
func TestSoftwareFallbackAndLifecycle(t *testing.T) {
	t.Setenv("PROVIEW_STL_RENDERER", "cpu")
	r := NewRenderer()
	defer closeRenderer(t, r)
	mesh := meshFixture(t)
	ctx := context.Background()
	camera := model3d.DefaultCamera()
	frame, err := r.Render(ctx, mesh, camera, 200, 160)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := model3d.RenderHighQuality(ctx, mesh, camera, 200, 160)
	if err != nil {
		t.Fatal(err)
	}
	if r.Backend() != "CPU" || !bytes.Equal(frame.Pix, reference.Pix) {
		t.Fatal("software fallback changed the camera/frame")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := r.Render(canceled, mesh, camera, 200, 160); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := r.Render(ctx, mesh, camera, 2400, 2400); err == nil {
		t.Fatal("oversized frame accepted")
	}
	camera.Yaw = math.NaN()
	if _, err := r.Render(ctx, mesh, camera, 200, 160); err == nil {
		t.Fatal("invalid camera accepted")
	}
	r.Close()
	if _, err := r.Render(ctx, mesh, model3d.DefaultCamera(), 200, 160); !errors.Is(err, context.Canceled) {
		t.Fatal("closed renderer accepted request")
	}
}
func TestNativeGPUFramesAndIndependentContexts(t *testing.T) {
	if os.Getenv("PROVIEW_TEST_STL_GPU") != "1" {
		t.Skip("set PROVIEW_TEST_STL_GPU=1 on a GPU-equipped macOS host")
	}
	t.Setenv("PROVIEW_STL_RENDERER", "")
	mesh := meshFixture(t)
	first, second := NewRenderer(), NewRenderer()
	defer closeRenderer(t, first)
	defer closeRenderer(t, second)
	camera := model3d.DefaultCamera()
	original, err := first.Render(context.Background(), mesh, camera, 200, 160)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first.Backend(), "GPU:") {
		t.Fatalf("GPU was not used: %s", first.Backend())
	}
	previous := bytes.Clone(original.Pix)
	camera.PanX = .2
	shifted, err := second.Render(context.Background(), mesh, camera, 200, 160)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(second.Backend(), "GPU:") || bytes.Equal(previous, shifted.Pix) {
		t.Fatal("second context or camera failed")
	}
	camera.Wireframe = true
	wire, err := first.Render(context.Background(), mesh, camera, 200, 160)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(wire.Pix, shifted.Pix) || !bytes.Equal(previous, original.Pix) {
		t.Fatal("wireframe or frame ownership failed")
	}
	// Resizing recreates attachments, and replacing a mesh invalidates its VBO.
	replacement := meshFixture(t)
	if _, err := first.Render(context.Background(), replacement, camera, 300, 200); err != nil {
		t.Fatal(err)
	}
	// GPU and CPU should agree on orientation, model placement and background.
	camera = model3d.DefaultCamera()
	cpu, err := model3d.RenderHighQuality(context.Background(), mesh, camera, 200, 160)
	if err != nil {
		t.Fatal(err)
	}
	mismatches := 0
	for i := 0; i < len(cpu.Pix); i += 4 {
		cpuObject := cpu.Pix[i] != model3d.BackgroundColor.R
		gpuObject := original.Pix[i] != model3d.BackgroundColor.R
		if cpuObject != gpuObject {
			mismatches++
		}
	}
	if mismatches > 200 {
		t.Fatalf("GPU camera differs from CPU: %d pixels", mismatches)
	}
	model3d.DrawGrid(original, mesh, camera, model3d.UnitMillimeter, "Isometric")
	gridPixels := 0
	for i := 0; i < len(original.Pix); i += 4 {
		if original.Pix[i] == 59 && original.Pix[i+1] == 69 && original.Pix[i+2] == 78 ||
			original.Pix[i] == 78 && original.Pix[i+1] == 91 && original.Pix[i+2] == 100 {
			gridPixels++
		}
	}
	if gridPixels == 0 {
		t.Fatal("reference grid did not appear on the GPU frame")
	}
}
