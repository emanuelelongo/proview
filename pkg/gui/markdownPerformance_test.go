package gui

import (
	"emanuelelongo/proview/internal/markdown"
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

func TestMarkdownTableScrollingBoundsRenderedText(t *testing.T) {
	var source strings.Builder
	source.WriteString("# Table scrolling\n\n| Operation | Observed | Expected | Result | Evidence |\n| --- | --- | --- | --- | --- |\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&source, "| Operation %d | Unavailable | Available | **Failure** | %s |\n", i,
			strings.Repeat("The operation requires permission to create this resource. ", 5))
	}
	source.WriteString("\nLast paragraph.\n")
	d, err := markdown.Load("scroll.md", "", strings.NewReader(source.String()))
	if err != nil {
		t.Fatal(err)
	}
	g, _ := newDropTestGui(t)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	g.addMarkdown(d, nil)
	v := g.markdownView
	height := v.page.MinSize().Height
	rendered := func() string {
		var text strings.Builder
		for _, object := range test.LaidOutObjects(v.page) {
			if line, ok := object.(*canvas.Text); ok {
				text.WriteString(line.Text)
			}
		}
		return text.String()
	}
	initial := rendered()
	if !strings.Contains(initial, "Operation 0") || strings.Contains(initial, "Operation 79") {
		t.Fatal("render tree does not match the opening viewport")
	}
	for i := 0; i < 50; i++ {
		v.scroll.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: -20}})
	}
	if v.scroll.Offset.Y <= 0 || v.page.MinSize().Height != height {
		t.Fatal("scrolling changed layout or did not move")
	}
	// ScrollToOffset does not emit OnScrolled in Fyne. Interior jumps, as used
	// by keyboard navigation and anchors, must still expose their destination.
	var destination *markdownSelectableText
	for _, block := range v.textBlocks {
		if block.text == "Operation 40" {
			destination = block
			break
		}
	}
	if destination == nil {
		t.Fatal("navigation destination missing")
	}
	v.scroll.ScrollToOffset(fyne.NewPos(0, destination.pagePosition.Y-20))
	if text := rendered(); !strings.Contains(text, "Operation 40") || strings.Contains(text, "Operation 0") {
		t.Fatal("programmatic navigation left stale text in the renderer tree")
	}
	v.scroll.ScrollToBottom()
	tail := rendered()
	if !strings.Contains(tail, "Operation 79") || !strings.Contains(tail, "Last paragraph.") || strings.Contains(tail, "Operation 0") {
		t.Fatal("scrolling did not replace the rendered text with the tail")
	}
	// Selection geometry and source remain available outside the render tree.
	first, last := v.textBlocks[0], v.textBlocks[len(v.textBlocks)-1]
	if first.absolutePosition().Y >= last.absolutePosition().Y || len(first.fragments()) == 0 {
		t.Fatal("offscreen selection geometry was lost")
	}
	v.selection = &markdownTextSelection{anchor: markdownTextPoint{}, end: markdownTextPoint{block: last.index, offset: len(last.text)}}
	if text := v.selectedText(); !strings.HasPrefix(text, first.text) || !strings.HasSuffix(text, last.text) {
		t.Fatal("selection omitted offscreen text")
	}
	v.clearSelection()
	v.scroll.ScrollToTop()
	if got := rendered(); got != initial || v.page.MinSize().Height != height {
		t.Fatal("returning to the top changed text or page geometry")
	}
	// Reflow must update the retained geometry and the rendered viewport together.
	g.window.Resize(fyne.NewSize(600, 740))
	g.window.Canvas().Capture()
	v.scroll.ScrollToBottom()
	if !strings.Contains(rendered(), "Last paragraph.") {
		t.Fatal("resizing lost the end of the document")
	}
}
