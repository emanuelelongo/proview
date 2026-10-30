package document

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"reflect"
	"strings"
	"sync"
	"testing"

	"emanuelelongo/proview/pkg/annotations"
)

func previewLensDocument(t testing.TB) (*Document, OverlayID) {
	t.Helper()
	ctx, err := newAppearanceContext(595, 842)
	if err != nil {
		t.Fatal(err)
	}
	page, _, _, err := ctx.PageDict(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	var content strings.Builder
	for y := 20; y < 820; y += 12 {
		for x := 20; x < 580; x += 10 {
			fmt.Fprintf(&content, "0 g %d %d 1 6 re f\n", x, y)
		}
	}
	ref, err := addPDFStream(ctx, []byte(content.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	page["Contents"] = *ref
	raw, err := writePDFContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	source, err := Load("preview.pdf", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(doc.Close)
	data := annotations.Default(annotations.Shape)
	data.Shape, data.Magnification = annotations.Magnifier, 2
	data.PageWidth, data.PageHeight = 595, 842
	data.Points = []annotations.Point{{X: 100, Y: 100}, {X: 220, Y: 220}}
	asset, _, err := data.Artwork()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := asset.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	lens, err := doc.AddAnnotation(0, NormalizedRect{X: .2, Y: .2, Width: .2, Height: .14}, data, prepared)
	if err != nil {
		t.Fatal(err)
	}
	return doc, lens.ID
}

func TestPreviewRendererReusesLensSampleAndInvalidatesContent(t *testing.T) {
	doc, id := previewLensDocument(t)
	renderer := &PreviewRenderer{}
	check := func() {
		t.Helper()
		actual, err := renderer.RenderLens(doc.RenderingSnapshot(), id, 144)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := doc.RenderLens(id, 144)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatal("cached lens differs from fresh rendering")
		}
	}
	check()
	sample := renderer.lens.image
	if got, want := renderer.RetainedRasterBytes(), rasterBytes(sample); got != want {
		t.Fatalf("lens cache bytes = %d, want %d", got, want)
	}
	// A lens move changes the sampled rectangle, not the underlying page.
	doc.signatures[0].Rect.X += .1
	check()
	if renderer.lens.image != sample {
		t.Fatal("lens movement rerendered the page")
	}
	if got := renderer.RetainedRasterBytes(); got != rasterBytes(sample) {
		t.Fatalf("reused lens cache bytes = %d, want %d", got, rasterBytes(sample))
	}
	mark, err := doc.AddRedaction(0, []TextQuad{{UL: TextPoint{X: .35, Y: .2}, UR: TextPoint{X: .45, Y: .2}, LL: TextPoint{X: .35, Y: .3}, LR: TextPoint{X: .45, Y: .3}}})
	if err != nil {
		t.Fatal(err)
	}
	check()
	if renderer.lens.image == sample {
		t.Fatal("redaction did not invalidate lens sample")
	}
	if err := doc.RemoveRedaction(mark.ID); err != nil {
		t.Fatal(err)
	}
	check()
	sample = renderer.lens.image
	// Earlier artwork must invalidate sampling; later artwork must not.
	data := annotations.Default(annotations.Highlight)
	data.PageWidth, data.PageHeight = 595, 842
	data.Quads = []annotations.Quad{{{X: 100, Y: 100}, {X: 400, Y: 100}, {X: 400, Y: 400}, {X: 100, Y: 400}}}
	addAnnotationGeometry(t, doc, data)
	check()
	if renderer.lens.image != sample {
		t.Fatal("later artwork invalidated the lens sample")
	}
	doc.signatures[0], doc.signatures[1] = doc.signatures[1], doc.signatures[0]
	check()
	if renderer.lens.image == sample {
		t.Fatal("earlier artwork did not invalidate lens sample")
	}
	sample = renderer.lens.image
	doc.pages[0].turns = 1
	check()
	if renderer.lens.image == sample {
		t.Fatal("rotation did not invalidate lens sample")
	}
}

func TestHighlightPreviewsSkipLaterObjectsAndReuseBase(t *testing.T) {
	doc, _ := previewLensDocument(t)
	data := annotations.Default(annotations.Highlight)
	data.PageWidth, data.PageHeight = 595, 842
	data.Quads = []annotations.Quad{{{X: 100, Y: 100}, {X: 400, Y: 100}, {X: 400, Y: 130}, {X: 100, Y: 130}}}
	addAnnotationGeometry(t, doc, data)
	doc.signatures[0], doc.signatures[1] = doc.signatures[1], doc.signatures[0]
	renderer := &PreviewRenderer{}
	first, err := renderer.HighlightPreviews(doc.RenderingSnapshot(), 0, 288)
	if err != nil {
		t.Fatal(err)
	}
	base := renderer.base.image
	// Invalid later artwork proves fragment collection never renders it.
	doc.signatures[1].annotation = nil
	doc.signatures[1].pdfAppearance = []byte("invalid appearance")
	second, err := renderer.HighlightPreviews(doc.RenderingSnapshot(), 0, 288)
	if err != nil {
		t.Fatal(err)
	}
	if renderer.base.image != base || !reflect.DeepEqual(first, second) {
		t.Fatal("later object changed highlight fragments or rerendered base")
	}
	if got, want := renderer.RetainedRasterBytes(), rasterBytes(base)+fragmentBytes(first); got != want {
		t.Fatalf("preview cache bytes = %d, want %d", got, want)
	}
	// A different page transform changes source pixels.
	doc.pages[0].turns = 1
	if _, err := renderer.HighlightPreviews(doc.RenderingSnapshot(), 0, 288); err != nil {
		t.Fatal(err)
	}
	if renderer.base.image == base {
		t.Fatal("source transform did not invalidate base")
	}
}

func BenchmarkLensDragPreview(b *testing.B) {
	doc, id := previewLensDocument(b)
	for _, cached := range []bool{false, true} {
		b.Run(fmt.Sprintf("cached=%v", cached), func(b *testing.B) {
			renderer := &PreviewRenderer{}
			if cached {
				if _, err := renderer.RenderLens(doc, id, 144); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				doc.signatures[0].Rect.X = .2 + float64(i%10)*.01
				var picture image.Image
				var err error
				if cached {
					picture, err = renderer.RenderLens(doc, id, 144)
				} else {
					picture, err = doc.RenderLens(id, 144)
				}
				if err != nil || picture == nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestPreviewRendererInvalidatesFormValuesAndUndo(t *testing.T) {
	doc := formDocument(t)
	fields, err := doc.FormFields(0)
	if err != nil {
		t.Fatal(err)
	}
	renderer := &PreviewRenderer{}
	first, err := renderer.RenderBasePageDPI(doc.RenderingSnapshot(), 0, 288)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetFormValue(0, fields[0].ID, "Updated"); err != nil {
		t.Fatal(err)
	}
	second, err := renderer.RenderBasePageDPI(doc.RenderingSnapshot(), 0, 288)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(first, second) {
		t.Fatal("form edit reused stale source pixels")
	}
	if !doc.Undo() {
		t.Fatal("form undo failed")
	}
	restored, err := renderer.RenderBasePageDPI(doc.RenderingSnapshot(), 0, 288)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, restored) {
		t.Fatal("undo reused edited source pixels")
	}
}

// Include highlight composition, as the GUI does before updating the lens.
func BenchmarkHighlightedLensDragPreview(b *testing.B) {
	doc, id := previewLensDocument(b)
	for n := 0; n < 12; n++ {
		data := annotations.Default(annotations.Highlight)
		data.PageWidth, data.PageHeight = 595, 842
		y := float64(100 + 35*n)
		data.Quads = []annotations.Quad{{{X: 50, Y: y}, {X: 500, Y: y}, {X: 500, Y: y + 10}, {X: 50, Y: y + 10}}}
		asset, bounds, err := data.Artwork()
		if err != nil {
			b.Fatal(err)
		}
		prepared, err := asset.Prepare()
		if err != nil {
			b.Fatal(err)
		}
		if _, err := doc.AddAnnotation(0, NormalizedRect{X: bounds.X0 / 595, Y: bounds.Y0 / 842, Width: bounds.W() / 595, Height: bounds.H() / 842}, data, prepared); err != nil {
			b.Fatal(err)
		}
	}
	lens := doc.signatures[0]
	copy(doc.signatures, doc.signatures[1:])
	doc.signatures[len(doc.signatures)-1] = lens
	renderer := &PreviewRenderer{}
	render := func() {
		if _, err := renderer.HighlightPreviews(doc, 0, 288); err != nil {
			b.Fatal(err)
		}
		if _, err := renderer.RenderLens(doc, id, 144); err != nil {
			b.Fatal(err)
		}
	}
	render()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc.signatures[len(doc.signatures)-1].Rect.X = .2 + float64(i%10)*.01
		render()
	}
}

func TestHighlightPreviewCacheTracksOnlyItsDependencies(t *testing.T) {
	doc, id := previewLensDocument(t)
	data := annotations.Default(annotations.Highlight)
	data.PageWidth, data.PageHeight = 595, 842
	data.Quads = []annotations.Quad{{{X: 100, Y: 100}, {X: 400, Y: 100}, {X: 400, Y: 130}, {X: 100, Y: 130}}}
	addAnnotationGeometry(t, doc, data)
	doc.signatures[0], doc.signatures[1] = doc.signatures[1], doc.signatures[0]
	highlightID := doc.signatures[0].ID
	renderer := &PreviewRenderer{}
	first, err := renderer.HighlightPreviews(doc.RenderingSnapshot(), 0, 144)
	if err != nil {
		t.Fatal(err)
	}
	// A returned map is caller-owned; frames themselves are immutable and shared.
	original := first[highlightID]
	delete(first, highlightID)
	doc.signatures[1].Rect.X += .1
	second, err := renderer.HighlightPreviews(doc.RenderingSnapshot(), 0, 144)
	if err != nil {
		t.Fatal(err)
	}
	if second[highlightID] != original {
		t.Fatal("later lens movement recomposed an unchanged highlight")
	}
	// Earlier lenses affect Multiply blending and must invalidate the fragments.
	doc.signatures[0], doc.signatures[1] = doc.signatures[1], doc.signatures[0]
	before, err := renderer.HighlightPreviews(doc.RenderingSnapshot(), 0, 144)
	if err != nil {
		t.Fatal(err)
	}
	doc.signatures[0].Rect.Y += .1
	after, err := renderer.HighlightPreviews(doc.RenderingSnapshot(), 0, 144)
	if err != nil {
		t.Fatal(err)
	}
	if after[highlightID] == before[highlightID] {
		t.Fatal("earlier lens move retained stale highlight")
	}
	expected, err := doc.HighlightPreviews(0, 144)
	if err != nil || !reflect.DeepEqual(after, expected) {
		t.Fatal("cached fragments differ from fresh painter order", err)
	}
	// Color/property edits replace immutable data and artwork.
	updated := data.Clone()
	updated.Color.R = 30
	asset, bounds, err := updated.Artwork()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := asset.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	rect := NormalizedRect{X: bounds.X0 / 595, Y: bounds.Y0 / 842, Width: bounds.W() / 595, Height: bounds.H() / 842}
	if err := doc.ReplaceAnnotation(highlightID, rect, updated, prepared); err != nil {
		t.Fatal(err)
	}
	edited, err := renderer.HighlightPreviews(doc.RenderingSnapshot(), 0, 144)
	if err != nil || edited[highlightID] == after[highlightID] {
		t.Fatal("property edit did not invalidate fragments", err)
	}
	if _, err := renderer.RenderLens(doc.RenderingSnapshot(), id, 144); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewRendererConcurrentWorkers(t *testing.T) {
	doc, id := previewLensDocument(t)
	data := annotations.Default(annotations.Highlight)
	data.PageWidth, data.PageHeight = 595, 842
	data.Quads = []annotations.Quad{{{X: 100, Y: 100}, {X: 400, Y: 100}, {X: 400, Y: 130}, {X: 100, Y: 130}}}
	addAnnotationGeometry(t, doc, data)
	snapshot := doc.RenderingSnapshot()
	renderer := &PreviewRenderer{}
	var workers sync.WaitGroup
	for task := 0; task < 3; task++ {
		workers.Go(func() {
			for range 3 {
				var err error
				switch task {
				case 0:
					_, err = renderer.RenderBasePageDPI(snapshot, 0, 288)
				case 1:
					_, err = renderer.RenderLens(snapshot, id, 144)
				case 2:
					var frames map[OverlayID]image.Image
					frames, err = renderer.HighlightPreviews(snapshot, 0, 288)
					clear(frames) // Caller mutation must not corrupt cached maps.
				}
				if err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	workers.Wait()
	frames, err := renderer.HighlightPreviews(snapshot, 0, 288)
	if err != nil || len(frames) != 1 {
		t.Fatal("concurrent callers corrupted highlight cache", err)
	}
}
