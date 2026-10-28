package document

import (
	"emanuelelongo/proview/internal/pdfengine"
	"reflect"
	"testing"
)

func TestSourceTextCacheBoundsAndRecency(t *testing.T) {
	var cache textLayoutCache
	for i := 0; i < sourceTextCachePages; i++ {
		cache.put(i, TextLayout{})
	}
	cache.get(0)
	cache.put(sourceTextCachePages, TextLayout{})
	if _, ok := cache.get(0); !ok {
		t.Fatal("recent page evicted")
	}
	if _, ok := cache.get(1); ok {
		t.Fatal("old page retained")
	}
	if len(cache.entries) != sourceTextCachePages {
		t.Fatal("page limit exceeded")
	}
	large := TextLayout{Glyphs: make([]pdfengine.Glyph, 20000)}
	for i := 0; i < 20; i++ {
		cache.put(i, large)
	}
	if cache.bytes > sourceTextCacheBytes || len(cache.entries) >= 20 {
		t.Fatal("byte limit exceeded")
	}
	oversized := TextLayout{Glyphs: make([]pdfengine.Glyph, sourceTextCacheBytes/128+1)}
	cache.put(1000, oversized)
	if _, ok := cache.get(1000); ok {
		t.Fatal("oversized layout retained")
	}
}

func TestTextGeometryReloadAfterEvictionAndCopyIsolation(t *testing.T) {
	doc := redactionDocument(t)
	original, err := doc.TextGeometry(0)
	if err != nil || len(original.Glyphs) == 0 {
		t.Fatalf("missing geometry: %v", err)
	}
	modified, err := doc.TextGeometry(0)
	if err != nil {
		t.Fatal(err)
	}
	modified.Glyphs[0].Rune = '!'
	cached, err := doc.TextGeometry(0)
	if err != nil || !reflect.DeepEqual(cached, original) {
		t.Fatal("caller modified cached glyphs")
	}
	source, index, err := doc.pageSource(0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= sourceTextCachePages; i++ {
		source.textLayouts.put(index+i, TextLayout{})
	}
	if _, ok := source.textLayouts.get(index); ok {
		t.Fatal("page not evicted")
	}
	reloaded, err := doc.TextGeometry(0)
	if err != nil || !reflect.DeepEqual(reloaded, original) {
		t.Fatalf("geometry changed after reload: %v", err)
	}
	copied, err := doc.TextGeometry(0)
	if err != nil || !reflect.DeepEqual(copied, original) {
		t.Fatal("cache changed by caller")
	}
}
