//go:build largeimageexperiment && cgo

// imageprobe measures an isolated JPEG region decoder and latest-request worker.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"emanuelelongo/proview/internal/imageregion"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	file := flag.String("file", "", "local JPEG source")
	mode := flag.String("mode", "preview", "preview, region, stress or full (Go baseline)")
	out := flag.String("out", "", "optional PNG output")
	x := flag.Int("x", 0, "region source x")
	y := flag.Int("y", 0, "region source y")
	width := flag.Int("width", 1600, "region width")
	height := flag.Int("height", 1000, "region height")
	scale := flag.Int("scale", 1, "region JPEG scale denominator")
	events := flag.Int("events", 300, "stress viewport event count at 2 ms intervals")
	interval := flag.Duration("interval", 2*time.Millisecond, "stress time between viewport events")
	settle := flag.Duration("settle", 80*time.Millisecond, "worker quiet period before decode")
	flag.Parse()
	data, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	source, err := imageregion.Open(data)
	if err != nil {
		return err
	}
	var picture image.Image
	started := time.Now()
	report := map[string]any{"mode": *mode, "source": source.Size(), "compressed_bytes": len(data)}
	switch *mode {
	case "full":
		// Reopen the same file; this baseline intentionally measures Go's decoder.
		f, e := os.Open(*file)
		if e != nil {
			return e
		}
		defer f.Close()
		picture, err = jpeg.Decode(f)
	case "preview":
		picture, err = source.Decode(context.Background(), imageregion.Request{Rect: image.Rectangle{Max: source.Size()}, Denominator: 8})
	case "region":
		picture, err = source.Decode(context.Background(), imageregion.Request{Rect: image.Rect(*x, *y, *x+*width, *y+*height), Denominator: *scale})
	case "stress":
		var decodes, canceled atomic.Int64
		worker := imageregion.NewWorker(*settle, func(ctx context.Context, r imageregion.Request) (*image.RGBA, error) {
			decodes.Add(1)
			pixels, e := source.Decode(ctx, r)
			if e == context.Canceled {
				canceled.Add(1)
			}
			return pixels, e
		})
		defer worker.Close()
		var last uint64
		var maxSubmit time.Duration
		for i := 0; i < *events; i++ {
			left := (i * 137) % max(1, source.Size().X-*width)
			top := (i * 73) % max(1, source.Size().Y-*height)
			before := time.Now()
			last = worker.Submit(imageregion.Request{Rect: image.Rect(left, top, left+*width, top+*height), Denominator: *scale})
			maxSubmit = max(maxSubmit, time.Since(before))
			time.Sleep(*interval)
		}
		settled := time.Now()
		select {
		case result := <-worker.Results():
			if result.Sequence != last || !worker.Current(last) {
				return fmt.Errorf("stale frame delivered")
			}
			picture, err = result.Image, result.Err
			report["final_sequence"] = result.Sequence
			report["detail_after_last_event_ms"] = float64(time.Since(settled).Microseconds()) / 1000
		case <-time.After(10 * time.Second):
			return fmt.Errorf("worker failed to settle")
		}
		report["max_submit_us"] = float64(maxSubmit.Nanoseconds()) / 1000
		report["decodes_started"] = decodes.Load()
		report["decodes_canceled"] = canceled.Load()
	default:
		return fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		return err
	}
	report["elapsed_ms"] = float64(time.Since(started).Microseconds()) / 1000
	report["output_bounds"] = picture.Bounds()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	report["go_heap_alloc_bytes"] = stats.HeapAlloc
	report["go_total_alloc_bytes"] = stats.TotalAlloc
	if *out != "" {
		f, e := os.Create(*out)
		if e != nil {
			return e
		}
		e = png.Encode(f, picture)
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
