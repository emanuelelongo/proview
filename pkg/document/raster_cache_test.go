package document

import (
	"bytes"
	"image"
	"reflect"
	"testing"
)

func TestSourceRastersAreBoundedAndUseLRU(t *testing.T) {
	source := &Source{format: FormatPDF, pages: make([]pageData, 20)}
	for page := range source.pages {
		picture := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
		source.pages[page].preview = picture
		source.rasterBytes += rasterBytes(picture)
		source.touchRaster(page, false)
		if source.rasterBytes > maxSourceRasterBytes {
			t.Fatal("source cache exceeded its byte budget")
		}
	}
	if source.pages[0].preview != nil || source.pages[19].preview == nil {
		t.Fatal("source cache evicted the newest page")
	}
	source.touchRaster(4, false)
	source.pages[0].thumbnail = image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	source.rasterBytes += rasterBytes(source.pages[0].thumbnail)
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

func TestSourceTrimPreviews(t *testing.T) {
	source := &Source{format: FormatPDF, pages: make([]pageData, 3)}
	source.pages[0].preview = image.NewRGBA(image.Rect(0, 0, 100, 100))
	source.pages[0].thumbnail = image.NewRGBA(image.Rect(0, 0, 20, 20))
	source.rasterBytes = rasterBytes(source.pages[0].preview) + rasterBytes(source.pages[0].thumbnail)

	source.TrimPreviews()
	if source.pages[0].preview != nil {
		t.Fatal("preview was not trimmed")
	}
	if source.pages[0].thumbnail == nil {
		t.Fatal("thumbnail was trimmed")
	}
	if source.rasterBytes != rasterBytes(source.pages[0].thumbnail) {
		t.Fatalf("unexpected rasterBytes: %d", source.rasterBytes)
	}

	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	source.pages[0].preview = image.NewRGBA(image.Rect(0, 0, 100, 100))
	source.rasterBytes += rasterBytes(source.pages[0].preview)
	doc.TrimInactive()
	if source.pages[0].preview != nil {
		t.Fatal("document TrimInactive did not trim preview")
	}

	source.pages[0].preview = image.NewRGBA(image.Rect(0, 0, 100, 100))
	source.pages[1].preview = image.NewRGBA(image.Rect(0, 0, 100, 100))
	source.rasterBytes += rasterBytes(source.pages[0].preview) + rasterBytes(source.pages[1].preview)

	doc.TrimPagePreview(0)
	if source.pages[0].preview != nil {
		t.Fatal("TrimPagePreview did not trim target page")
	}
	if source.pages[1].preview == nil {
		t.Fatal("TrimPagePreview trimmed non-target page")
	}
}
