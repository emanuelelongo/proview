package gui

import (
	"emanuelelongo/proview/internal/markdown"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"strings"
	"testing"
)

func TestMarkdownFontsShareResourcesAndKeepFallback(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.app.Settings().SetTheme(theme.DefaultTheme())
	defer g.clearMarkdown()
	var source fyne.Resource
	for i := 0; i < 2; i++ {
		doc, err := markdown.Load("fonts.md", "", strings.NewReader("Caffè, città and ordinary text.\n"))
		if err != nil {
			t.Fatal(err)
		}
		g.addMarkdown(doc, nil)
		g.window.Show()
		g.window.Canvas().Capture()
		for _, object := range g.markdownView.textBlocks[0].renderer.WidgetRenderer.Objects() {
			if text, ok := object.(*canvas.Text); ok && strings.TrimSpace(text.Text) != "" {
				if text.FontSource == nil {
					t.Fatal("font resource was not shared")
				}
				if source != nil && source != text.FontSource {
					t.Fatal("documents use different copies of the same font")
				}
				source = text.FontSource
			}
		}
	}
	if source == nil || !markdownFontCovers(source, "Caffè") || markdownFontCovers(source, "\U0010FFFF") {
		t.Fatal("font coverage check does not preserve native fallback")
	}
	if markdownFontCovers(fyne.NewStaticResource("invalid", []byte("invalid")), "text") {
		t.Fatal("invalid font accepted")
	}
}

func TestMarkdownSwitchReusesViewAndInvalidatesInactiveTheme(t *testing.T) {
	g, _ := newDropTestGui(t)
	defer g.clearMarkdown()
	g.window.Show()
	load := func(label string) *markdown.Document {
		d, err := markdown.Load(label, "", strings.NewReader("# Heading\n\nRetained **text**.\n"))
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	g.addMarkdown(load("first.md"), nil)
	first, view := g.activeDocument, g.markdownView
	block := view.textBlocks[0]
	block.selectRange(0, len(block.text))
	g.addMarkdown(load("second.md"), nil)
	second, other := g.activeDocument, g.markdownView
	for range 4 {
		g.selectDocument(first)
		if g.markdownView != view || view.textBlocks[0] != block || view.selectedText() != "" {
			t.Fatal("switch rebuilt text or retained the old selection")
		}
		g.selectDocument(second)
		if g.markdownView != other {
			t.Fatal("second view was rebuilt")
		}
	}
	g.setMarkdownTheme("light")
	g.selectDocument(first)
	if g.markdownView != view || view.readingMode != "light" || view.textBlocks[0] == block {
		t.Fatal("inactive view did not refresh for a changed reading theme")
	}
}
