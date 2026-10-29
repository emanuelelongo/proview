package gui

import (
	"image/png"
	"os"
	"strings"
	"testing"

	"emanuelelongo/proview/internal/markdown"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

func TestMarkdownFrontMatterCalloutsAndWrappedLinks(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.window.Resize(fyne.NewSize(700, 850))
	g.window.Show()
	source := "---\nid: hidden-metadata\ntitle: Internal metadata title\nslug: /long/internal/path\n---\n\n# Migration guide\n\n:::warning\nThe migration needs **downtime**. Read the [maintenance window and service impact documentation](guide.md) before continuing.\n\n- Save the configuration.\n- Check the new cluster.\n:::\n\n## " + strings.Repeat("long-path/", 16) + "\n\nA long link after preceding text: [" + strings.Repeat("reference", 30) + "](guide.md).\n"
	doc, err := markdown.Load("formatting.md", "", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(doc, nil)
	defer g.clearMarkdown()
	for _, mode := range []string{"light", "dark"} {
		g.setMarkdownTheme(mode)
		for _, width := range []float32{700, 450} {
			g.window.Resize(fyne.NewSize(width, 850))
			g.window.Canvas().Capture()
			v := g.markdownView
			if len(v.headings) != 2 || v.headings[0].title != "Migration guide" {
				t.Fatal("metadata leaked into headings")
			}
			foundWarning, foundWrappedLink := false, false
			for _, block := range v.textBlocks {
				if strings.Contains(block.text, "hidden-metadata") || strings.Contains(block.text, ":::") {
					t.Fatal("source delimiters or metadata leaked into text")
				}
				if block.text == "WARNING" {
					foundWarning = true
				}
				for _, object := range block.renderer.WidgetRenderer.Objects() {
					if text, ok := object.(*canvas.Text); ok && strings.TrimSpace(text.Text) != "" {
						if text.Position().X+text.Size().Width > block.Size().Width+1 {
							t.Fatalf("text overflows at width %g: %q, right %g, block %g", width, text.Text, text.Position().X+text.Size().Width, block.Size().Width)
						}
					}
				}
				if len(block.links) == 0 {
					continue
				}
				link := &block.links[0]
				clicked := 0
				link.onTapped = func() { clicked++ }
				rows := map[float32]bool{}
				for _, fragment := range block.fragments() {
					if fragment.start < link.start || fragment.start >= link.end {
						continue
					}
					position := fragment.position.Add(fyne.NewPos(2, fragment.size.Height/2))
					if action := block.linkAt(position); action != nil {
						action()
						rows[fragment.position.Y] = true
					}
				}
				if clicked > 1 && len(rows) > 1 {
					foundWrappedLink = true
				}
			}
			if !foundWarning || !foundWrappedLink {
				t.Fatal("missing warning or clickable multiline link")
			}
		}
	}
	if path := os.Getenv("PROVIEW_FORMATTING_PREVIEW"); path != "" {
		g.window.Resize(fyne.NewSize(700, 850))
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, g.window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}
}
