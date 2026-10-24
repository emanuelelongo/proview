package mermaid

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"
)

const sample = "flowchart TD\n A[Caffè] --> B{Save?}\n B -->|Yes| C[Done]\n B -->|No| A"

func TestRenderImageThemesZoomAndCache(t *testing.T) {
	r := NewRenderer()
	ctx := context.Background()
	options := Options{Width: 640, FontScale: 1, PixelScale: 2}
	light, err := r.Render(ctx, sample, options)
	if err != nil {
		t.Fatal(err)
	}
	if light.Image == nil || light.Width <= 0 || light.Height <= 0 {
		t.Fatal("diagram has no dimensions")
	}
	if light.Image.Bounds().Dx()*light.Image.Bounds().Dy() > MaxImagePixels {
		t.Fatal("image budget exceeded")
	}
	again, err := r.Render(ctx, sample, options)
	if err != nil || again.Image != light.Image {
		t.Fatal("unchanged rendering was not reused", err)
	}
	options.Dark = true
	dark, err := r.Render(ctx, sample, options)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(light.Image.Pix, dark.Image.Pix) {
		t.Fatal("dark theme did not change pixels")
	}
	options.Dark = false
	options.FontScale = 2
	zoom, err := r.Render(ctx, sample, options)
	if err != nil {
		t.Fatal(err)
	}
	if zoom.Height <= light.Height {
		t.Fatal("zoom did not increase diagram text/layout size")
	}
}
func TestRenderRejectsInvalidOrExcessiveSources(t *testing.T) {
	r := NewRenderer()
	for _, source := range []string{"", "unknownDiagram\nA --> B", "flowchart LR\n A[Broken", strings.Repeat("x", MaxSourceBytes+1)} {
		if result, err := r.Render(context.Background(), source, Options{}); err == nil || result.Image != nil {
			t.Fatalf("accepted invalid source %q", source[:min(50, len(source))])
		}
	}
	nodes := make([]string, 16)
	for i := range nodes {
		nodes[i] = fmt.Sprintf("N%d", i)
	}
	source := "flowchart LR\n" + strings.Join(nodes, " & ") + " --> " + strings.Join(nodes, " & ")
	if _, err := r.Render(context.Background(), source, Options{}); err == nil || !strings.Contains(Message(err), "limit") {
		t.Fatal("expanded edge limit was not enforced", err)
	}
}
func TestCancelledRenderDoesNotWaitForActiveWork(t *testing.T) {
	for i := 0; i < maxRenderSlots; i++ {
		renderSlot <- struct{}{}
		defer func() { <-renderSlot }()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := NewRenderer().Render(ctx, sample, Options{}); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled work remained blocked")
	}
}
func TestConcurrentRenderUsesImmutableCachedResult(t *testing.T) {
	r := NewRenderer()
	var wg sync.WaitGroup
	results := make(chan Result, 8)
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := r.Render(context.Background(), sample, Options{Width: 512})
			results <- result
			failures <- err
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	first := <-results
	for result := range results {
		if result.Image != first.Image {
			t.Fatal("concurrent callers received duplicate images")
		}
	}
}
func TestCacheEvictsOlderDiagramsWithinBounds(t *testing.T) {
	r := NewRenderer()
	for i := 0; i < maxCacheEntries+2; i++ {
		if _, err := r.Render(context.Background(), fmt.Sprintf("flowchart LR\n A[Document %d] --> B[Read]", i), Options{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.cache) > maxCacheEntries || r.pixels > maxCachePixels {
		t.Fatal("cache exceeded its bounds")
	}
}

func TestParallelRenderProcessesDistinctDiagramsConcurrently(t *testing.T) {
	r := NewRenderer()
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			_, err := r.Render(ctx, fmt.Sprintf("flowchart LR\n A[Task %d] --> B[Done]", idx), Options{Width: 300})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpaquePageBackgroundKeepsUnboxedSequenceLabelsReadable(t *testing.T) {
	background := color.NRGBA{R: 245, G: 245, B: 245, A: 255}
	source := "sequenceDiagram\n A->>B: Open Markdown\n B-->>A: Show diagram"
	result, err := NewRenderer().Render(context.Background(), source, Options{Width: 640, Background: background})
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(result.Image.At(0, 0)).(color.NRGBA); got != background {
		t.Fatalf("contrast backdrop is not the page colour: %v", got)
	}
}
