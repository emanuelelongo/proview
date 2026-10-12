// stlprobe renders a model with the prototype viewer's software renderer.
package main

import (
	"context"
	"emanuelelongo/proview/internal/model3d"
	"emanuelelongo/proview/internal/modelgpu"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"
)

func main() {
	file := flag.String("file", "", "STL input file")
	output := flag.String("out", "build/stl-prototype/preview.png", "PNG output")
	wire := flag.Bool("wireframe", false, "Render wireframe")
	view := flag.String("view", "isometric", "isometric, top, bottom, front, rear, left or right")
	gpu := flag.Bool("gpu", false, "Try the GPU renderer (CPU fallback if unavailable)")
	frames := flag.Int("frames", 1, "Number of camera frames to measure")
	flag.Parse()
	if *frames < 1 || *frames > 1000 {
		log.Fatal("-frames must be 1–1000")
	}
	if *file == "" {
		log.Fatal("-file is required")
	}
	input, err := os.Open(*file)
	if err != nil {
		log.Fatal(err)
	}
	start := time.Now()
	mesh, err := model3d.Load(filepath.Base(*file), input)
	input.Close()
	if err != nil {
		log.Fatal(err)
	}
	loaded := time.Since(start)
	camera := model3d.DefaultCamera()
	camera.Wireframe = *wire
	switch *view {
	case "isometric":
	case "front":
		camera.Yaw, camera.Pitch = 0, 0
	case "right":
		camera.Yaw, camera.Pitch = math.Pi/2, 0
	case "left":
		camera.Yaw, camera.Pitch = -math.Pi/2, 0
	case "rear":
		camera.Yaw, camera.Pitch = math.Pi, 0
	case "bottom":
		camera.Yaw, camera.Pitch = 0, -math.Pi/2
	case "top":
		camera.Yaw, camera.Pitch = 0, math.Pi/2
	default:
		log.Fatal("invalid view")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start = time.Now()
	var frame *image.RGBA
	var renderer *modelgpu.Renderer
	backend := "CPU"
	if *gpu {
		renderer = modelgpu.NewRenderer()
		defer renderer.Close()
	}
	for i := 0; i < *frames; i++ {
		camera.Yaw += .005
		if renderer != nil {
			frame, err = renderer.Render(ctx, mesh, camera, 1000, 700)
			backend = renderer.Backend()
		} else {
			frame, err = model3d.RenderHighQuality(ctx, mesh, camera, 1000, 700)
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		log.Fatal(err)
	}
	rendered := time.Since(start)
	if err = os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
		log.Fatal(err)
	}
	target, err := os.Create(*output)
	if err != nil {
		log.Fatal(err)
	}
	if err = png.Encode(target, frame); err != nil {
		target.Close()
		log.Fatal(err)
	}
	if err = target.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s; %d triangles; load %s; %d frames %s (average %s); output %s\n", backend, len(mesh.Triangles), loaded, *frames, rendered, rendered/time.Duration(*frames), *output)
}
