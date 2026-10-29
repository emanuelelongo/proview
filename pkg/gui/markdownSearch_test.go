package gui

import (
	"strings"
	"testing"
	"time"

	"emanuelelongo/proview/internal/markdown"
	"fyne.io/fyne/v2"
)

func TestMarkdownSearchNavigationAndSelection(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.clearMarkdown()
	defer g.closeSearch()
	source := "# Caffè **pronto**\n\n" + strings.Repeat("Paragraph of filler text.\n\n", 40) + "| Item | Value |\n| --- | --- |\n| CAFÉ | pronto |\n\n```go\nprintln(\"caffè pronto\")\n```\n"
	doc, err := markdown.Load("search.md", "", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	g.showMarkdown(doc)
	g.window.Resize(fyne.NewSize(800, 500))
	g.window.Show()
	g.window.Canvas().Capture()
	for _, item := range g.menuDocumentItems {
		if (item.Label == "Find" || item.Label == "Find Next" || item.Label == "Find Previous") && item.Disabled {
			t.Fatalf("%s disabled", item.Label)
		}
	}
	g.openSearch()
	g.search.entry.SetText("CAFFÈ PRONTO")
	for g.search.running {
		driver.runNext(t)
	}
	if len(g.search.hits) != 2 || g.search.current != 0 {
		t.Fatalf("hits=%d current=%d", len(g.search.hits), g.search.current)
	}
	v := g.markdownView
	first := v.textBlocks[0]
	if len(first.renderer.highlights) == 0 || v.selectedText() != "" {
		t.Fatal("search must highlight independently of selection")
	}
	first.selectRange(0, len("Caffè"))
	g.nextSearch(true)
	if g.search.current != 1 || v.scroll.Offset.Y == 0 || v.selectedText() != "Caffè" {
		t.Fatal("navigation did not scroll or changed selection")
	}
	g.nextSearch(false)
	if g.search.current != 0 {
		t.Fatal("navigation did not wrap")
	}
	g.search.entry.SetText("café pronto")
	for g.search.running {
		driver.runNext(t)
	}
	if len(g.search.hits) != 1 || g.search.hits[0].markdown.anchor.block == g.search.hits[0].markdown.end.block {
		t.Fatal("phrase across table cells not found")
	}
	v.zoom(1)
	for g.search.running {
		driver.runNext(t)
	}
	if len(g.search.hits) != 1 || v.searchMatch == nil {
		t.Fatal("search lost after reflow")
	}
	g.search.entry.SetText("absent")
	for g.search.running {
		driver.runNext(t)
	}
	if len(g.search.hits) != 0 || v.searchMatch != nil {
		t.Fatal("no-results query left stale highlight")
	}
	g.search.entry.SetText("pronto")
	g.closeSearch()
	if v.searchMatch != nil || g.search.markdown != nil || g.search.bar.Visible() {
		t.Fatal("search did not close")
	}
}

func TestMarkdownSearchRevealsHorizontalCodeAndStrikethrough(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.clearMarkdown()
	defer g.closeSearch()
	source := "~~caffè~~\n\n```\n" + strings.Repeat("wide ", 100) + "needle\n```\n"
	doc, err := markdown.Load("code.md", "", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	g.showMarkdown(doc)
	g.window.Resize(fyne.NewSize(800, 500))
	g.window.Show()
	g.window.Canvas().Capture()
	g.openSearch()
	g.search.entry.SetText("caffè")
	for g.search.running {
		driver.runNext(t)
	}
	if len(g.search.hits) != 1 {
		t.Fatal("strikethrough text was not found")
	}
	v := g.markdownView
	v.selection = g.search.hits[0].markdown
	if v.selectedText() != "caffè" {
		t.Fatalf("strikethrough byte offsets: %q", v.selectedText())
	}
	v.clearSelection()
	g.search.entry.SetText("needle")
	for g.search.running {
		driver.runNext(t)
	}
	if len(g.search.hits) != 1 {
		t.Fatal("code match not found")
	}
	block := v.textBlocks[g.search.hits[0].markdown.anchor.block]
	if block.horizontal == nil || block.horizontal.Offset.X <= 0 {
		t.Fatal("offscreen code match was not revealed horizontally")
	}
	// Closing Find clears only its highlight, preserving the user's selection.
	block.selectRange(0, 4)
	g.closeSearch()
	if v.selectedText() != "wide" || block.renderer.searchStart != 0 || block.renderer.searchEnd != 0 || len(block.renderer.highlights) == 0 {
		t.Fatal("closing Find changed the user's selection")
	}
}

func TestMarkdownSearchRejectsQueuedResults(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.clearMarkdown()
	defer g.closeSearch()
	doc, err := markdown.Load("search.md", "", strings.NewReader("first second"))
	if err != nil {
		t.Fatal(err)
	}
	g.showMarkdown(doc)
	g.openSearch()
	g.search.entry.SetText("first")
	var stale func()
	select {
	case stale = <-driver.callbacks:
	case <-time.After(5 * time.Second):
		t.Fatal("search did not finish")
	}
	g.search.entry.SetText("second")
	stale()
	if len(g.search.hits) != 0 || !g.search.running {
		t.Fatal("obsolete query published results")
	}
	var completion func()
	select {
	case completion = <-driver.callbacks:
	case <-time.After(5 * time.Second):
		t.Fatal("replacement search did not finish")
	}
	g.closeSearch()
	completion()
	if len(g.search.hits) != 0 || g.markdownView.searchMatch != nil || g.search.bar.Visible() {
		t.Fatal("closed search published results")
	}
}
