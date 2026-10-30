package document

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"
	"time"
)

func TestRotatedPreviewIsCachedAndReplacesSourceRaster(t *testing.T) {
	source, err := Load("pages.pdf", bytes.NewReader(minimalPDF(t, 2)))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(doc.Close)
	if err = doc.TransformPDFPage(0, "right"); err != nil {
		t.Fatal(err)
	}
	first, err := doc.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := first.(*image.RGBA); !ok {
		t.Fatalf("rotated preview is %T, want *image.RGBA for direct texture upload", first)
	}
	second, err := doc.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("rotated preview was recomputed instead of reused")
	}
	ref := doc.pages[0]
	waitForTrims(t, ref.source)
	ref.source.mu.Lock()
	unrotated := ref.source.pages[ref.index].preview
	ref.source.mu.Unlock()
	if unrotated != nil {
		t.Fatal("unrotated source raster stayed resident next to the rotated copy")
	}
	doc.TrimPagePreview(0)
	if doc.derived.get(derivedKey{source: ref.source, index: ref.index, turns: ref.turns}) != nil {
		t.Fatal("TrimPagePreview kept the derived preview")
	}
	_, raster := doc.MemoryUsage()
	if raster < 0 {
		t.Fatal("negative raster accounting")
	}
}

func waitForTrims(t *testing.T, source *Source) {
	t.Helper()
	for range 1000 {
		source.trimMu.Lock()
		idle := !source.trimRunning && len(source.trimQueue) == 0
		source.trimMu.Unlock()
		if idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFilledFormPDFIsReusedForSameValues(t *testing.T) {
	var cache derivedRasterCache
	source := &Source{}
	calls := 0
	fill := func() ([]byte, error) { calls++; return []byte{1, 2, 3}, nil }
	for range 3 {
		if _, err := cache.filledPDF(source, 7, fill); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("filled PDF generated %d times for unchanged values", calls)
	}
	if _, err := cache.filledPDF(source, 8, fill); err != nil || calls != 2 {
		t.Fatalf("changed values reused stale PDF: calls=%d err=%v", calls, err)
	}
	if cache.retainedBytes() != 3 {
		t.Fatalf("retained bytes = %d", cache.retainedBytes())
	}
	cache.trim(true)
	if cache.retainedBytes() != 0 {
		t.Fatal("trim kept filled PDF bytes")
	}
}

func TestFormsFingerprintIgnoresMapOrder(t *testing.T) {
	source := &Source{}
	a := &Document{formValues: map[formKey][]string{{source, 1}: {"x"}, {source, 2}: {"y"}}}
	b := &Document{formValues: map[formKey][]string{{source, 2}: {"y"}, {source, 1}: {"x"}}}
	c := &Document{formValues: map[formKey][]string{{source, 1}: {"y"}, {source, 2}: {"x"}}}
	if a.formsFingerprint(source) != b.formsFingerprint(source) {
		t.Fatal("fingerprint depends on insertion order")
	}
	if a.formsFingerprint(source) == c.formsFingerprint(source) {
		t.Fatal("fingerprint ignores field identity")
	}
	if a.formsFingerprint(&Source{}) != 0 {
		t.Fatal("other sources must not be affected")
	}
}

func TestLoadBytesKeepsCallerBuffer(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	source, err := LoadBytes("image.png", "", data)
	if err != nil {
		t.Fatal(err)
	}
	if &source.original[0] != &data[0] {
		t.Fatal("LoadBytes copied the input buffer")
	}
}

func TestAnnotatedSourceDoesNotKeepSecondPDFContext(t *testing.T) {
	data := exportEditable(t, editablePDF(t, 0))
	source, err := LoadBytes("annotated.pdf", "", data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.Close)
	if source.annotationBase == nil {
		t.Fatal("fixture has no imported annotations")
	}
	if source.pdf != nil || source.rasterRegistered {
		t.Fatal("outer annotated source keeps a MuPDF context or raster budget share")
	}
	if !source.annotationBase.rasterRegistered {
		t.Fatal("annotation base is not in the raster budget")
	}
}

func TestRasterBytesCountsCompactFormats(t *testing.T) {
	rect := image.Rect(0, 0, 100, 100)
	if got := rasterBytes(image.NewYCbCr(rect, image.YCbCrSubsampleRatio420)); got != 15000 {
		t.Fatalf("YCbCr 4:2:0 = %d bytes, want 15000", got)
	}
	if got := rasterBytes(image.NewGray(rect)); got != 10000 {
		t.Fatalf("Gray = %d bytes", got)
	}
	if got := rasterBytes(image.NewRGBA64(rect)); got != 80000 {
		t.Fatalf("RGBA64 = %d bytes", got)
	}
}

func TestFlattenOnWhiteMatchesDrawOver(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := 0; i < len(src.Pix); i += 4 {
		a := uint8(i * 7)
		c := color.RGBAModel.Convert(color.NRGBA{uint8(i), uint8(i * 3), uint8(i * 5), a}).(color.RGBA)
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	want := image.NewRGBA(src.Rect)
	draw.Draw(want, want.Rect, image.White, image.Point{}, draw.Src)
	draw.Draw(want, want.Rect, src, image.Point{}, draw.Over)
	flattenOnWhite(src)
	if !bytes.Equal(src.Pix, want.Pix) {
		t.Fatal("in-place white compositing differs from draw.Over")
	}
}
