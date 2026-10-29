package gui

import (
	"emanuelelongo/proview/internal/markdown"
	"fyne.io/fyne/v2"
	"strings"
	"testing"
)

func TestMarkdownDetailsToggleAndRetainedState(t *testing.T) {
	g, _ := newDropTestGui(t)
	defer g.clearMarkdown()
	doc, err := markdown.Load("details.md", "", strings.NewReader("<details>\n<summary>More information</summary>\n\n## Hidden heading\n\nHidden **body**.\n\n<details open>\n<summary>Nested</summary>\n\nNested body.\n\n</details>\n\n</details>\n\nAfterwards.\n"))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(doc, nil)
	g.window.Resize(fyne.NewSize(800, 600))
	g.window.Show()
	original := g.activeDocument
	v := g.markdownView
	if len(v.headings) != 0 || len(v.textBlocks) != 2 || v.textBlocks[0].text != "More information" {
		t.Fatal("closed content rendered")
	}
	node := doc.Root.FirstChild().(*markdown.Details)
	// Header taps and the arrow share the same action.
	var clickSummary func(fyne.CanvasObject) bool
	clickSummary = func(object fyne.CanvasObject) bool {
		if tap, ok := object.(*thumbnailTap); ok {
			tap.Tapped(&fyne.PointEvent{})
			return true
		}
		if box, ok := object.(*fyne.Container); ok {
			for _, child := range box.Objects {
				if clickSummary(child) {
					return true
				}
			}
		}
		return false
	}
	if !clickSummary(v.content.Objects[0]) {
		t.Fatal("summary tap target missing")
	}
	if !v.detailsOpen[node] || len(v.headings) != 1 {
		t.Fatal("section did not expand")
	}
	found := false
	for _, block := range v.textBlocks {
		if block.text == "Nested body." {
			found = true
		}
	}
	if !found {
		t.Fatal("open nested section missing")
	}
	v.zoom(1)
	if !v.detailsOpen[node] {
		t.Fatal("zoom lost disclosure state")
	}
	g.addDocument(testDocument(t, 40, 30), nil, nil)
	g.activateDocument(original)
	if !g.markdownView.detailsOpen[node] {
		t.Fatal("document switching lost disclosure state")
	}
}
