package mermaid

import (
	"context"
	"testing"
)

func TestRendererClearReleasesCachedRasters(t *testing.T) {
	r := NewRenderer()
	if _, err := r.Render(context.Background(), sample, Options{Width: 320}); err != nil {
		t.Fatal(err)
	}
	if r.CachedPixels() == 0 {
		t.Fatal("rendered diagram was not cached")
	}
	r.Clear()
	if r.CachedPixels() != 0 || len(r.cache) != 0 {
		t.Fatal("Clear retained cached diagram rasters")
	}
	if _, err := r.Render(context.Background(), sample, Options{Width: 320}); err != nil || r.CachedPixels() == 0 {
		t.Fatal("renderer is not reusable after Clear", err)
	}
	var nilRenderer *Renderer
	nilRenderer.Clear()
}
