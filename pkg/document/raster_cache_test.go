package document

import (
	"bytes"
	"image"
	"reflect"
	"testing"
	"time"
)

func TestSourceRastersAreBoundedAndUseLRU(t *testing.T) {
	source := &Source{format: FormatPDF, pages: make([]pageData, 20)}
	for page := range source.pages {
		picture := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
		source.pages[page].preview = picture
		source.rasterBytes.Add(rasterBytes(picture))
		source.touchRaster(page, false)
		if source.rasterBytes.Load() > maxSourceRasterBytes {
			t.Fatal("source cache exceeded its byte budget")
		}
	}
	if source.pages[0].preview != nil || source.pages[19].preview == nil {
		t.Fatal("source cache evicted the newest page")
	}
	source.touchRaster(4, false)
	source.pages[0].thumbnail = image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	source.rasterBytes.Add(rasterBytes(source.pages[0].thumbnail))
	source.touchRaster(0, true)
	if source.pages[4].preview == nil || source.pages[5].preview != nil {
		t.Fatal("recently used raster was not retained")
	}
}

func TestEvictedPDFPreviewReloadsWithoutChangingSourceOrHistory(t *testing.T) {
	source, err := Load("pages.pdf", bytes.NewReader(minimalPDF(t, 2)))
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
	source.mu.Lock()
	source.trimRasters(0)
	source.mu.Unlock()
	after, err := doc.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	if before == after || !reflect.DeepEqual(before, after) {
		t.Fatal("eviction failed to regenerate identical source pixels")
	}
	if doc.Dirty() || doc.CanUndo() || doc.SourceRevision() != 0 {
		t.Fatal("cache eviction modified document history")
	}
	size, err := doc.PageSize(0)
	if err != nil || size != before.Bounds().Size() {
		t.Fatal("eviction lost logical page dimensions", err)
	}
}

func TestSharedRasterLimitScalesWithLiveSources(t *testing.T) {
	previous := rasterSourceCount.Load()
	defer rasterSourceCount.Store(previous)
	cases := []struct {
		count int64
		want  int64
	}{
		{0, maxSourceRasterBytes},
		{1, maxSourceRasterBytes},
		{2, 64 << 20},
		{4, maxSharedRasterBytes / 4},
		{100, minSourceRasterBytes},
	}
	for _, tc := range cases {
		rasterSourceCount.Store(tc.count)
		if got := sourceRasterLimit(); got != tc.want {
			t.Fatalf("sourceRasterLimit with %d sources = %d, want %d", tc.count, got, tc.want)
		}
	}
}

func TestSourceTrimPreviews(t *testing.T) {
	source := &Source{format: FormatPDF, pages: make([]pageData, 3)}
	source.pages[0].preview = image.NewRGBA(image.Rect(0, 0, 100, 100))
	source.pages[0].thumbnail = image.NewRGBA(image.Rect(0, 0, 20, 20))
	source.rasterBytes.Store(rasterBytes(source.pages[0].preview) + rasterBytes(source.pages[0].thumbnail))

	source.TrimPreviews()
	if source.pages[0].preview != nil {
		t.Fatal("preview was not trimmed")
	}
	if source.pages[0].thumbnail == nil {
		t.Fatal("thumbnail was trimmed")
	}
	if source.rasterBytes.Load() != rasterBytes(source.pages[0].thumbnail) {
		t.Fatalf("unexpected rasterBytes: %d", source.rasterBytes.Load())
	}

	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	source.pages[0].preview = image.NewRGBA(image.Rect(0, 0, 100, 100))
	source.rasterBytes.Add(rasterBytes(source.pages[0].preview))
	doc.TrimInactive()
	if source.pages[0].preview != nil {
		t.Fatal("document TrimInactive did not trim preview")
	}

	source.pages[0].preview = image.NewRGBA(image.Rect(0, 0, 100, 100))
	source.pages[1].preview = image.NewRGBA(image.Rect(0, 0, 100, 100))
	source.rasterBytes.Add(rasterBytes(source.pages[0].preview) + rasterBytes(source.pages[1].preview))

	doc.TrimPagePreview(0)
	if source.pages[0].preview != nil {
		t.Fatal("TrimPagePreview did not trim target page")
	}
	if source.pages[1].preview == nil {
		t.Fatal("TrimPagePreview trimmed non-target page")
	}
}

func TestReopenedPDFSourceRejoinsRasterBudget(t *testing.T) {
	source, err := Load("pages.pdf", bytes.NewReader(minimalPDF(t, 1)))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	registered := rasterSourceCount.Load()
	source.Close()
	if rasterSourceCount.Load() != registered-1 {
		t.Fatal("closed source stayed registered")
	}
	if _, err := source.Preview(0); err != nil {
		t.Fatal(err)
	}
	if rasterSourceCount.Load() != registered || !source.rasterRegistered {
		t.Fatal("reopened source did not rejoin the shared raster budget")
	}
}

func TestTrimDoesNotWaitForBusySource(t *testing.T) {
	source := &Source{format: FormatPDF, pages: make([]pageData, 2)}
	for page := range source.pages {
		source.pages[page].preview = image.NewRGBA(image.Rect(0, 0, 8, 8))
		source.rasterBytes.Add(rasterBytes(source.pages[page].preview))
	}
	source.mu.Lock() // A native render in progress.
	done := make(chan struct{})
	go func() {
		source.TrimPagePreview(0)
		source.TrimPreviews()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("trim waited for the source render lock")
	}
	// Page 1 is used after the trims were queued and must survive them.
	source.touchRaster(1, false)
	source.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for {
		source.mu.Lock()
		trimmed := source.pages[0].preview == nil
		kept := source.pages[1].preview != nil
		source.mu.Unlock()
		if trimmed {
			if !kept {
				t.Fatal("queued trim dropped a preview used after the request")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("queued trim never ran")
		}
		time.Sleep(time.Millisecond)
	}
}
