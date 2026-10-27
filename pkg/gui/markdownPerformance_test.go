package gui

import (
	"emanuelelongo/proview/internal/markdown"
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
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

func markdownLongCodeSource(lines int) string {
	var source strings.Builder
	source.WriteString("# Long code\n\nAn introductory paragraph.\n\n```go\n")
	for i := range lines {
		fmt.Fprintf(&source, "func example%d() string { return \"città highlighted text\" }\n", i)
	}
	source.WriteString("```\n\n## End\n\nFinal paragraph.\n")
	return source.String()
}

func TestMarkdownLongCodeSelectionScrollAndReflow(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	g.addMarkdown(outlineDocument(t, markdownLongCodeSource(300)), nil)
	defer g.clearMarkdown()
	v := g.markdownView
	var code *markdownSelectableText
	for _, block := range v.textBlocks {
		if block.Wrapping == fyne.TextWrapOff {
			code = block
			break
		}
	}
	if code == nil || !code.positioned {
		t.Fatal("code has no retained page geometry")
	}
	rendered := func() string {
		var text strings.Builder
		for _, object := range code.renderer.Objects() {
			if line, ok := object.(*canvas.Text); ok {
				text.WriteString(line.Text)
			}
		}
		return text.String()
	}
	if strings.Contains(rendered(), "example299") {
		t.Fatal("distant code lines entered the initial render tree")
	}
	fragments := code.fragments()
	first, last := fragments[0], fragments[len(fragments)-1]
	code.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary, PointEvent: fyne.PointEvent{Position: first.position}})
	code.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: code.absolutePosition().Add(last.position).Add(fyne.NewPos(last.width(last.text)+1, last.size.Height/2))}})
	code.DragEnd()
	if v.selection.anchor.offset != 0 || v.selection.end.offset < len(code.text)-1 {
		t.Fatal("offscreen drag lost the end of the code")
	}
	selected := v.selectedText()
	if !strings.Contains(selected, "example299") {
		t.Fatal("copy omitted culled code")
	}
	v.scroll.ScrollToOffset(fyne.NewPos(0, code.pagePosition.Y+last.position.Y-100))
	if !strings.Contains(rendered(), "example299") || strings.Contains(rendered(), "example0()") {
		t.Fatal("code lines did not follow programmatic navigation")
	}
	visibleHighlights := 0
	for _, object := range code.renderer.Objects() {
		if _, ok := object.(*canvas.Rectangle); ok {
			visibleHighlights++
		}
	}
	if visibleHighlights == 0 || visibleHighlights >= len(code.renderer.highlights) {
		t.Fatal("selection highlights were lost or were not culled")
	}
	// Native minimum-size checks can request the same layout each frame as new
	// lines enter the viewport. A selected long block must not allocate per frame.
	code.renderer.Layout(code.Size())
	code.renderer.Objects()
	if allocs := testing.AllocsPerRun(20, func() { code.renderer.Layout(code.Size()); code.renderer.MinSize(); code.renderer.Objects() }); allocs > 0 {
		t.Fatalf("unchanged selected code layout allocates: %g", allocs)
	}
	g.window.Resize(fyne.NewSize(700, 740))
	g.window.Canvas().Capture()
	if v.selectedText() != selected {
		t.Fatal("reflow changed the selected source")
	}
	// Geometry and hit testing must be rebuilt after reflow, including UTF-8.
	fragments = code.fragments()
	for _, f := range []markdownTextFragment{fragments[0], fragments[len(fragments)-1]} {
		p := code.point(f.position.Add(fyne.NewPos(0, f.size.Height/2)))
		if p.offset != f.start {
			t.Fatalf("reflow retained stale fragment geometry: %d != %d", p.offset, f.start)
		}
	}
	if !v.clearSelection() || len(code.renderer.highlights) != 0 {
		t.Fatal("clearing left code highlights behind")
	}
	v.scroll.ScrollToTop()
	if !strings.Contains(rendered(), "example0") || strings.Contains(rendered(), "example299") {
		t.Fatal("clearing selection broke code culling")
	}
}

func BenchmarkMarkdownSelection(b *testing.B) {
	for _, workload := range []string{"drag", "selected-frame"} {
		b.Run(workload, func(b *testing.B) {
			app := test.NewApp()
			defer app.Quit()
			g := newGui(app)
			g.window.Resize(fyne.NewSize(1000, 740))
			g.window.Show()
			doc, err := markdown.Load("benchmark.md", "", strings.NewReader(markdownLongCodeSource(300)))
			if err != nil {
				b.Fatal(err)
			}
			g.showMarkdown(doc)
			defer g.clearMarkdown()
			v := g.markdownView
			var code *markdownSelectableText
			for _, block := range v.textBlocks {
				if block.Wrapping == fyne.TextWrapOff {
					code = block
					break
				}
			}
			fragments := code.fragments()
			last := fragments[len(fragments)-1]
			for _, fragment := range fragments[max(0, len(fragments)-12):] {
				if fragment.size.Width > last.size.Width {
					last = fragment
				}
			}
			code.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary, PointEvent: fyne.PointEvent{Position: fragments[0].position}})
			end := code.absolutePosition().Add(last.position).Add(fyne.NewPos(0, last.size.Height/2))
			code.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: end}})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if workload == "drag" {
					code.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: end.Add(fyne.NewPos(float32(i%max(1, int(last.size.Width))), 0))}})
				} else {
					code.renderer.Layout(code.Size())
					code.renderer.MinSize()
					code.renderer.Objects()
				}
			}
			b.StopTimer()
		})
	}
}
