package document

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"reflect"
	"slices"
	"testing"
)

func TestInactiveImageDocumentReloadsPreview(t *testing.T) {
	original := image.NewRGBA(image.Rect(0, 0, 8, 6))
	original.Set(2, 3, color.RGBA{R: 200, G: 10, B: 30, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, original); err != nil {
		t.Fatal(err)
	}
	source, err := Load("photo.png", bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	before, err := doc.Preview(0)
	if err != nil {
		t.Fatal(err)
	}

	doc.TrimInactive()
	source.mu.Lock()
	trimmed := source.pages[0].preview
	rasterBytes := source.rasterBytes.Load()
	source.mu.Unlock()
	if trimmed != nil || rasterBytes != 0 {
		t.Fatal("inactive image document retained its decoded raster")
	}

	after, err := doc.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("lazy reload changed image pixels")
	}
	size, err := doc.PageSize(0)
	if err != nil || size != original.Bounds().Size() {
		t.Fatalf("page size after trim = %v, want %v (%v)", size, original.Bounds().Size(), err)
	}
	if doc.Dirty() || doc.CanUndo() {
		t.Fatal("trimming rasters changed document history")
	}
}

func TestInactiveGIFRestoresAnimationLazily(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	animated := &gif.GIF{
		Image: []*image.Paletted{
			image.NewPaletted(image.Rect(0, 0, 2, 2), palette),
			image.NewPaletted(image.Rect(0, 0, 2, 2), palette),
		},
		Delay: []int{5, 5},
	}
	animated.Image[1].SetColorIndex(0, 0, 1)
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, animated); err != nil {
		t.Fatal(err)
	}
	source, err := Load("anim.gif", bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	if doc.Animation() == nil {
		t.Fatal("animated GIF did not load an animation")
	}

	doc.TrimInactive()
	source.animationMu.Lock()
	trimmed := source.animation
	source.animationMu.Unlock()
	if trimmed != nil {
		t.Fatal("inactive GIF retained decoded animation frames")
	}
	if doc.Animation() == nil {
		t.Fatal("animation was not restored lazily")
	}
	if _, err := doc.Preview(0); err != nil {
		t.Fatal(err)
	}
}

func TestPruneTextSourcesDropsUnreachableEdits(t *testing.T) {
	source, err := Load("pages.pdf", bytes.NewReader(minimalPDF(t, 1)))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()

	stale := &Source{format: FormatPDF, original: []byte("%PDF-1.4"), pages: []pageData{{preview: image.NewRGBA(image.Rect(0, 0, 4, 4))}}}
	doc.textSources = append(doc.textSources, stale)
	key := derivedKey{source: stale, index: 0}
	doc.derived.put(key, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	_, _ = doc.derived.filledPDF(stale, 1, func() ([]byte, error) { return []byte{1}, nil })
	doc.pruneTextSources()
	if slices.Contains(doc.textSources, stale) {
		t.Fatal("unreachable edited source was not pruned")
	}
	if stale.pages[0].preview != nil {
		t.Fatal("pruned source was not closed")
	}
	if doc.derived.get(key) != nil || len(doc.derived.filled) != 0 {
		t.Fatal("pruned source remained in derived caches")
	}

	// The main source and current page sources must survive pruning.
	doc.textSources = append(doc.textSources, source)
	doc.pruneTextSources()
	if !slices.Contains(doc.textSources, source) {
		t.Fatal("current source was pruned")
	}
}

func TestSourceCloseReleasesDisposableRasters(t *testing.T) {
	source, err := Load("pages.pdf", bytes.NewReader(minimalPDF(t, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Preview(0); err != nil {
		t.Fatal(err)
	}
	source.mu.Lock()
	retained := source.rasterBytes.Load()
	source.mu.Unlock()
	if retained == 0 {
		t.Fatal("preview did not populate the raster cache")
	}
	source.Close()
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.rasterBytes.Load() != 0 || source.pages[0].preview != nil || source.pages[0].thumbnail != nil {
		t.Fatal("Close retained disposable rasters")
	}
}

func TestDocumentMemoryUsageCountsEachSourceOnce(t *testing.T) {
	source, err := Load("pages.pdf", bytes.NewReader(minimalPDF(t, 2)))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	if _, err := doc.Preview(0); err != nil {
		t.Fatal(err)
	}
	sourceBytes, rasterBytes := doc.MemoryUsage()
	if sourceBytes != int64(len(source.original)) {
		t.Fatalf("source bytes = %d, want %d", sourceBytes, len(source.original))
	}
	if rasterBytes == 0 {
		t.Fatal("raster bytes were not reported")
	}
}

func TestPlainPDFSkipsAnnotationAndFormRenderers(t *testing.T) {
	source, err := Load("plain.pdf", bytes.NewReader(minimalPDF(t, 2)))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if !source.noForms || len(source.annotPages) != 0 || source.hasPDFAnnotations || source.annotationBase != nil {
		t.Fatalf("plain PDF classified as annotated: forms=%v pages=%v", !source.noForms, source.annotPages)
	}
	if _, err := source.Preview(1); err != nil {
		t.Fatal(err)
	}
	if source.formRenderer != nil {
		t.Fatal("plain PDF opened a second native renderer")
	}
}
