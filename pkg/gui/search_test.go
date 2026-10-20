package gui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"testing"

	"codeberg.org/go-pdf/fpdf"
	"emanuelelongo/proview/internal/textselect"
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
)

type searchOCR struct{ calls int }

func TestClosingSearchRestoresDocumentViewport(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	doc := testDocument(t, 100, 100)
	defer doc.Close()
	g.document = doc
	g.window.Show()
	g.window.Content().Refresh()
	position, size := g.mainSplit.Position(), g.mainSplit.Size()
	g.openSearch()
	if g.mainSplit.Size().Height >= size.Height {
		t.Fatal("search did not reserve space")
	}
	g.closeSearch()
	if g.search.doc != nil || g.search.textCache != nil {
		t.Fatal("closed search retained its document or text index")
	}
	if g.mainSplit.Position() != position || g.mainSplit.Size() != size {
		t.Fatalf("viewport not restored: %v %v, want %v %v", g.mainSplit.Position(), g.mainSplit.Size(), position, size)
	}
}

func (e *searchOCR) Recognize(ctx context.Context, im image.Image) (textselect.Layout, error) {
	e.calls++
	return textselect.Layout{Width: 100, Height: 100, Words: []textselect.Word{{Text: "found", Quad: textselect.Rect(.1, .2, .4, .3)}}}, ctx.Err()
}

func TestSearchScansAllPagesAndReusesOCR(t *testing.T) {
	pdf := fpdf.New("P", "pt", "A4", "")
	for _, text := range []string{"First found", "Nothing here", "Last FOUND"} {
		pdf.AddPage()
		pdf.SetFont("Helvetica", "", 14)
		pdf.Text(30, 40, text)
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("search.pdf", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	engine := &searchOCR{}
	pages := []int{}
	walkSearchPages(context.Background(), doc.RenderingSnapshot(), "found", engine, nil, func(page int, hits []searchHit, err error, last, limited bool) {
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range hits {
			pages = append(pages, hit.page)
			if len(hit.quads) != 1 {
				t.Fatal("missing match geometry")
			}
		}
		if last != (page == 2) || limited {
			t.Fatal("incorrect scan completion")
		}
	})
	if len(pages) != 2 || pages[0] != 0 || pages[1] != 2 || engine.calls != 0 {
		t.Fatalf("pages=%v OCR calls=%d", pages, engine.calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	walkSearchPages(ctx, doc.RenderingSnapshot(), "found", engine, nil, func(int, []searchHit, error, bool, bool) { t.Fatal("canceled search published") })
	imageDoc := testDocument(t, 100, 100)
	defer imageDoc.Close()
	for pass := 0; pass < 2; pass++ {
		var cached map[int]textselect.Layout
		if pass == 1 {
			cached = map[int]textselect.Layout{0: {Words: []textselect.Word{{Text: "found"}}}}
		}
		walkSearchPages(context.Background(), imageDoc.RenderingSnapshot(), "found", engine, cached, func(page int, hits []searchHit, err error, last, limited bool) {
			if err != nil || len(hits) != 1 || !last {
				t.Fatalf("image search: hits=%v err=%v", hits, err)
			}
		})
	}
	if engine.calls != 1 {
		t.Fatal("cached OCR was not reused")
	}
}

func TestSearchNavigationEscapeAndInvalidation(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	doc := testDocument(t, 100, 100)
	defer doc.Close()
	g.document = doc
	g.imageSignatureLayer.SetDocument(doc, 0, image.Pt(100, 100))
	g.openSearch()
	s := g.search
	s.entry.Text = "found"
	s.doc = doc
	s.key = g.searchKey()
	s.hits = []searchHit{{page: 0, quads: []textselect.Quad{textselect.Rect(.1, .1, .3, .2)}}, {page: 0, quads: []textselect.Quad{textselect.Rect(.5, .5, .7, .6)}}}
	g.showSearchHit(0)
	g.nextSearch(true)
	if s.current != 1 || g.imageSignatureLayer.searchHighlight == nil || !g.imageSignatureLayer.searchHighlight.Visible() {
		t.Fatal("previous match did not wrap/highlight")
	}
	g.nextSearch(false)
	if s.current != 0 {
		t.Fatal("next match did not wrap")
	}
	if doc.Dirty() {
		t.Fatal("search modified history")
	}
	s.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if s.bar.Visible() || len(s.hits) != 0 || g.imageSignatureLayer.searchHighlight.Visible() {
		t.Fatal("Escape left search state")
	}
	// An in-flight search must be canceled immediately when its document closes.
	s.bar.Show()
	s.entry.SetText("pending")
	oldSerial := s.serial
	g.closeSearch()
	if s.serial == oldSerial || s.running {
		t.Fatal("closing did not invalidate pending callbacks")
	}
}

func TestSearchIndexReusesRecognitionAcrossQueriesAndInvalidatesGeometry(t *testing.T) {
	doc := testDocument(t, 100, 100)
	defer doc.Close()
	engine := &searchOCR{}
	index := &searchTextCache{}
	scan := func(query string) {
		t.Helper()
		walkSearchPages(context.Background(), doc.RenderingSnapshot(), query, engine, nil, func(_ int, _ []searchHit, err error, _, _ bool) {
			if err != nil {
				t.Error(err)
			}
		}, index)
	}
	scan("found")
	scan("absent")
	if engine.calls != 1 {
		t.Fatal("query change reran OCR")
	}
	if err := doc.TransformImage("right"); err != nil {
		t.Fatal(err)
	}
	scan("found")
	if engine.calls != 2 {
		t.Fatal("source geometry reused stale search layout")
	}
	if !doc.Undo() {
		t.Fatal("undo failed")
	}
	scan("found")
	if engine.calls != 2 {
		t.Fatal("undo did not reuse immutable original layout")
	}
}

func TestSearchTextCacheEvictsOldEntriesAndRejectsOversizedLayouts(t *testing.T) {
	index := &searchTextCache{}
	for n := 0; n < searchTextCachePages+1; n++ {
		index.put(fmt.Sprint(n), textselect.Layout{})
	}
	if _, ok := index.get("0"); ok {
		t.Fatal("old cache entry was retained past capacity")
	}
	if _, ok := index.get("128"); !ok {
		t.Fatal("newest cache entry was evicted")
	}
	index.put("oversized", textselect.Layout{Words: make([]textselect.Word, searchTextCacheBytes/128+1)})
	if _, ok := index.get("oversized"); ok {
		t.Fatal("oversized text layout exceeded memory budget")
	}
}

func TestSearchUsesOCRCharacterGeometry(t *testing.T) {
	doc := testDocument(t, 100, 100)
	defer doc.Close()
	word := textselect.Word{Text: "banana", Quad: textselect.Rect(.1, .1, .7, .2)}
	for i := range word.Text {
		word.Characters = append(word.Characters, textselect.Character{Start: i, End: i + 1, Quad: textselect.Rect(.1+float64(i)*.1, .1, .2+float64(i)*.1, .2)})
	}
	layout := textselect.Layout{Width: 100, Height: 100, Words: []textselect.Word{word}}
	walkSearchPages(context.Background(), doc.RenderingSnapshot(), "ana", nil, map[int]textselect.Layout{0: layout}, func(page int, hits []searchHit, err error, last, limited bool) {
		if err != nil || len(hits) != 2 || len(hits[0].quads) != 3 || len(hits[1].quads) != 3 {
			t.Fatalf("hits=%+v err=%v", hits, err)
		}
		if hits[0].quads[0] != word.Characters[1].Quad || hits[1].quads[0] != word.Characters[3].Quad {
			t.Fatal("substring highlights were not narrowed")
		}
	})
}

func TestSearchCacheAccountsForCharacterGeometry(t *testing.T) {
	index := &searchTextCache{}
	index.put("characters", textselect.Layout{Words: []textselect.Word{{Text: "huge", Characters: make([]textselect.Character, searchTextCacheBytes/80+1)}}})
	if _, ok := index.get("characters"); ok {
		t.Fatal("character geometry bypassed memory budget")
	}
}
