package gui

import (
	"strings"
	"testing"

	"emanuelelongo/proview/internal/markdown"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

func TestMarkdownSourceIsLocalToOpenDocument(t *testing.T) {
	g, _ := newDropTestGui(t)
	defer g.clearMarkdown()
	load := func(name, source string) *markdown.Document {
		t.Helper()
		d, err := markdown.Load(name, "", strings.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	source := "---\ntitle: Example\n---\n# Heading\n\n**bold** [link](https://example.com)\n\n```go\n  value := 1\n```\n"
	first := load("first.md", source)
	g.addMarkdown(first, nil)
	g.window.Resize(fyne.NewSize(800, 500))
	g.window.Show()
	if g.markdownView.sourceMode || g.markdownSourceItem.Checked || g.markdownSourceItem.Disabled {
		t.Fatal("wrong default source action state")
	}
	g.markdownSourceItem.Action()
	if !g.markdownView.sourceMode || !g.markdownSourceItem.Checked {
		t.Fatal("source action did not switch")
	}
	if blocks := g.markdownView.textBlocks; len(blocks) != 1 || blocks[0].text != source {
		t.Fatal("source syntax or whitespace changed")
	}
	if !g.markdownView.textBlocks[0].Segments[0].(*widget.TextSegment).Style.TextStyle.Monospace {
		t.Fatal("source is not monospace")
	}
	original := g.activeDocument
	g.zoomIn()
	if !g.markdownView.sourceMode {
		t.Fatal("zoom reset source mode")
	}
	g.addMarkdown(load("second.md", "# Second"), nil)
	if g.markdownView.sourceMode || g.markdownSourceItem.Checked {
		t.Fatal("source mode leaked to another document")
	}
	g.activateDocument(original)
	if !g.markdownView.sourceMode || !g.markdownSourceItem.Checked {
		t.Fatal("switching lost the retained document mode")
	}
	g.markdownSourceItem.Action()
	if g.markdownView.sourceMode || len(g.markdownView.headings) != 1 {
		t.Fatal("rendered mode did not return")
	}
	g.markdownSourceItem.Action()
	g.closeDocumentEntry(g.activeDocument)
	g.addMarkdown(first, nil)
	if g.markdownView.sourceMode {
		t.Fatal("reopening inherited source mode")
	}
}
