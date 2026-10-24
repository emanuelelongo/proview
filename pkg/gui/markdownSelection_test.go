package gui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestMarkdownSelectionAcrossStyledWrappedBlocks(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	window := app.NewWindow("Selection")
	v := &markdownView{editor: &Gui{app: app, window: window}, scale: 1, themeReady: true}
	v.editor.markdownView = v
	first := v.selectable(&widget.TextSegment{Text: "Caffè ", Style: widget.RichTextStyleInline}, &widget.TextSegment{Text: "bold words wrap onto another line", Style: widget.RichTextStyleStrong})
	second := v.selectable(&widget.TextSegment{Text: "  code(\"è\")\nnext()\n", Style: widget.RichTextStyleCodeInline})
	second.Wrapping = fyne.TextWrapOff
	window.SetContent(container.NewVBox(v.styleText(&first.RichText, first), v.styleText(&second.RichText, second)))
	window.Resize(fyne.NewSize(180, 300))
	window.Show()
	window.Canvas().Capture()
	fragments := first.fragments()
	if len(fragments) < 3 {
		t.Fatalf("text did not wrap: %d fragments", len(fragments))
	}
	f := fragments[0]
	start := f.position.Add(fyne.NewPos(0, f.size.Height/2))
	first.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary, PointEvent: fyne.PointEvent{Position: start}})
	end := second.fragments()[0]
	position := end.position.Add(fyne.NewPos(end.width(end.text), end.size.Height/2))
	absolute := app.Driver().AbsolutePositionForObject(second).Add(position)
	first.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: absolute}})
	first.DragEnd()
	v.editor.copyArea()
	want := first.text + "\n\n" + end.text
	if got := app.Clipboard().Content(); got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
	if len(first.renderer.highlights) == 0 || len(second.renderer.highlights) == 0 {
		t.Fatal("selected text is not highlighted")
	}
	// Reversing a drag must preserve reading order and UTF-8 boundaries.
	v.selection.anchor, v.selection.end = v.selection.end, v.selection.anchor
	if v.selectedText() != want {
		t.Fatal("reverse selection changed copied text")
	}
	if !v.clearSelection() || v.selectedText() != "" || len(first.renderer.highlights) != 0 {
		t.Fatal("selection did not clear")
	}
}

func TestMarkdownSelectionHitTestingKeepsUTF8AndCodeWhitespace(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	v := &markdownView{scale: 1, themeReady: true}
	b := v.selectable(&widget.TextSegment{Text: "  città café\n\treturn 1\n", Style: widget.RichTextStyleCodeInline})
	b.Wrapping = fyne.TextWrapOff
	window := app.NewWindow("Code")
	window.SetContent(v.styleText(&b.RichText, b))
	window.Resize(fyne.NewSize(400, 150))
	window.Show()
	window.Canvas().Capture()
	fragments := b.fragments()
	if len(fragments) == 0 {
		t.Fatal("code text did not render")
	}
	for _, f := range fragments {
		for i := range f.text {
			p := b.point(f.position.Add(fyne.NewPos(f.width(f.text[:i]), f.size.Height/2)))
			if p.offset != f.start+i {
				t.Fatalf("hit at byte %d in %q returned %d", i, f.text, p.offset)
			}
		}
	}
	v.selection = &markdownTextSelection{anchor: markdownTextPoint{block: 0}, end: markdownTextPoint{block: 0, offset: len(b.text)}}
	if got := v.selectedText(); !strings.HasPrefix(got, "  città") || got != b.text {
		t.Fatalf("code whitespace changed: %q", got)
	}
}
