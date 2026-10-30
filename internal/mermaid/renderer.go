// Package mermaid adapts sketch to bounded, offline image rendering.
// It has no Fyne or platform display dependencies.
package mermaid

import (
	"context"
	"crypto/sha256"
	"errors"
	"image"
	"image/color"
	"math"
	"sync"

	"tuios.dev/sketch"
	"tuios.dev/sketch/raster"
)

const (
	MaxSourceBytes  = 16 << 10
	MaxEdges        = 128
	MaxImagePixels  = 2_000_000
	maxCachePixels  = 8_000_000
	maxCacheEntries = 32
	maxRenderSlots  = 4
)

var (
	ErrLimit = errors.New("diagram exceeds rendering limits")
	// Allow bounded concurrent rendering so multiple diagrams load in parallel
	// without serial head-of-line delays or unbounded memory usage.
	renderSlot = make(chan struct{}, maxRenderSlots)
)

type Options struct {
	Width      int
	FontScale  float64
	PixelScale float64
	Dark       bool
	Background color.NRGBA
}

type Result struct {
	Image         *image.RGBA
	Width, Height float64
}

type cacheKey struct {
	source  [32]byte
	options Options
}
type cacheEntry struct {
	result Result
	used   uint64
}

type inFlightCall struct {
	done   chan struct{}
	result Result
	err    error
}

// Renderer owns a document-local cache. Published images are immutable.
type Renderer struct {
	mu       sync.Mutex
	cache    map[cacheKey]cacheEntry
	inFlight map[cacheKey]*inFlightCall
	pixels   int
	clock    uint64
}

func NewRenderer() *Renderer {
	return &Renderer{
		cache:    make(map[cacheKey]cacheEntry),
		inFlight: make(map[cacheKey]*inFlightCall),
	}
}

// Clear drops cached diagram rasters. In-flight renders are left to their
// callers; cancelled renders never publish into the cache.
func (r *Renderer) Clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.cache)
	r.pixels = 0
}

// CachedPixels reports retained diagram raster pixels.
func (r *Renderer) CachedPixels() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pixels
}

func normalise(options Options) Options {
	options.Width = max(128, min(2048, options.Width/16*16))
	if math.IsNaN(options.FontScale) || math.IsInf(options.FontScale, 0) || options.FontScale <= 0 {
		options.FontScale = 1
	}
	options.FontScale = math.Max(.75, math.Min(2, options.FontScale))
	if math.IsNaN(options.PixelScale) || math.IsInf(options.PixelScale, 0) || options.PixelScale <= 0 {
		options.PixelScale = 1
	}
	options.PixelScale = math.Max(1, math.Min(4, options.PixelScale))
	if options.Background.A != 0 {
		options.Background.A = 255
	}
	return options
}

func (r *Renderer) joinOrLead(key cacheKey) (*inFlightCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if call, ok := r.inFlight[key]; ok {
		return call, false
	}
	call := &inFlightCall{done: make(chan struct{})}
	r.inFlight[key] = call
	return call, true
}

func (r *Renderer) finishLead(key cacheKey, call *inFlightCall) {
	r.mu.Lock()
	delete(r.inFlight, key)
	r.mu.Unlock()
	close(call.done)
}

// Render checks cancellation before and after upstream work. Cancellation while
// waiting is immediate; an active sketch call completes under its input limits.
func (r *Renderer) Render(ctx context.Context, source string, options Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(source) > MaxSourceBytes {
		return Result{}, ErrLimit
	}
	options = normalise(options)
	key := cacheKey{sha256.Sum256([]byte(source)), options}
	if cached, ok := r.lookup(key); ok {
		return cached, nil
	}

	call, leader := r.joinOrLead(key)
	if !leader {
		select {
		case <-call.done:
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			return call.result, call.err
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	defer r.finishLead(key, call)

	select {
	case renderSlot <- struct{}{}:
	case <-ctx.Done():
		call.err = ctx.Err()
		return Result{}, ctx.Err()
	}
	defer func() { <-renderSlot }()
	if err := ctx.Err(); err != nil {
		call.err = err
		return Result{}, err
	}
	if cached, ok := r.lookup(key); ok {
		call.result = cached
		return cached, nil
	}
	width, fontSize := float64(options.Width), 14*options.FontScale
	disabled := false
	svg, err := sketch.Render(source, sketch.RenderOptions{
		MaxWidth: &width, FontSize: &fontSize, Limit: MaxSourceBytes, MaxEdges: MaxEdges,
		OutputLimit: 1 << 20, Links: &disabled, Images: &disabled,
	})
	if err != nil {
		call.err = err
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		call.err = err
		return Result{}, err
	}
	if !validSize(svg.Width, svg.Height) {
		call.err = ErrLimit
		return Result{}, ErrLimit
	}
	// Keep allocations bounded even for an unusually tall or wide diagram.
	scale := math.Min(options.PixelScale, math.Min(4096/svg.Width, 4096/svg.Height))
	scale = math.Min(scale, math.Sqrt(float64(MaxImagePixels-8192)/(svg.Width*svg.Height)))
	if scale < .5 {
		call.err = ErrLimit
		return Result{}, ErrLimit
	}
	paletteName := "pele"
	if options.Dark {
		paletteName = "flexoki-dark"
	}
	palette, _ := sketch.ThemeNamed(paletteName)
	paint := *palette
	if options.Background.A != 0 {
		paint.Bg = options.Background
	}
	// Readable contrast needs actual background pixels, not transparent black.
	img, err := raster.Draw(svg.Data, raster.Options{Scale: scale, Theme: &paint, Background: true, Readable: true, MaxPixels: MaxImagePixels})
	if err != nil {
		call.err = err
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		call.err = err
		return Result{}, err
	}
	result := Result{Image: img, Width: svg.Width, Height: svg.Height}
	call.result = result
	r.remember(key, result)
	return result, nil
}

func validSize(width, height float64) bool {
	return width > 0 && height > 0 && !math.IsNaN(width) && !math.IsNaN(height) && !math.IsInf(width, 0) && !math.IsInf(height, 0)
}
func (r *Renderer) lookup(key cacheKey) (Result, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[key]
	if !ok {
		return Result{}, false
	}
	r.clock++
	entry.used = r.clock
	r.cache[key] = entry
	return entry.result, true
}
func (r *Renderer) remember(key cacheKey, result Result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pixels := result.Image.Bounds().Dx() * result.Image.Bounds().Dy()
	if _, exists := r.cache[key]; exists {
		return
	}
	for len(r.cache) >= maxCacheEntries || r.pixels+pixels > maxCachePixels {
		var oldest cacheKey
		oldestUse := ^uint64(0)
		for k, entry := range r.cache {
			if entry.used < oldestUse {
				oldest, oldestUse = k, entry.used
			}
		}
		previous := r.cache[oldest].result.Image
		r.pixels -= previous.Bounds().Dx() * previous.Bounds().Dy()
		delete(r.cache, oldest)
	}
	if r.cache == nil {
		r.cache = make(map[cacheKey]cacheEntry)
	}
	r.clock++
	r.cache[key] = cacheEntry{result: result, used: r.clock}
	r.pixels += pixels
}

// Message keeps renderer failures useful without exposing implementation details.
func Message(err error) string {
	if errors.Is(err, ErrLimit) || errors.Is(err, raster.ErrTooLarge) {
		return "This diagram is too large to display. Its source is available below."
	}
	var problem *sketch.Error
	if errors.As(err, &problem) {
		switch problem.Code {
		case sketch.CodeLimit:
			return "This diagram exceeds the rendering limits. Its source is available below."
		case sketch.CodeUnsupported:
			return "This diagram type is not supported. Its source is available below."
		case sketch.CodeSyntax, sketch.CodeSemantic:
			return "The Mermaid syntax could not be interpreted. Its source is available below."
		}
	}
	return "The diagram could not be rendered. Its source is available below."
}
