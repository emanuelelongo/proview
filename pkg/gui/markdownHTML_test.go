package gui

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emanuelelongo/proview/internal/markdown"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

func TestMarkdownHTMLLogoAndFlatCodePanels(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.window.Resize(fyne.NewSize(700, 850))
	g.window.Show()
	path, err := filepath.Abs("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	source := "<p align=\"center\"><img alt=\"Application icon\" src=\"/assets/app-icon.png?raw=true\" width=\"200\"></p>\n\n<p align=\"center\">Welcome to <strong>the project</strong>.</p>\n\n```text\n" + strings.Repeat("long code line ", 20) + "\n```\n"
	doc, err := markdown.Load("README.md", path, strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(doc, nil)
	defer g.clearMarkdown()
	for _, mode := range []string{"light", "dark"} {
		g.setMarkdownTheme(mode)
		g.window.Canvas().Capture()
		images := 0
		for _, object := range test.LaidOutObjects(g.markdownPanel) {
			if img, ok := object.(*canvas.Image); ok && img.Image == doc.Images["assets/app-icon.png?raw=true"] {
				images++
				if img.MinSize().Width != 200 {
					t.Fatalf("HTML image width = %g", img.MinSize().Width)
				}
			}
			if surface, ok := object.(*markdownHorizontalScrollSurface); ok {
				surface.horizontal.ScrollToOffset(fyne.NewPos(80, 0))
			}
		}
		if images == 0 {
			t.Fatal("HTML logo was not rendered")
		}
	}
	if path := os.Getenv("PROVIEW_HTML_PREVIEW"); path != "" {
		g.setMarkdownTheme("light")
		g.window.Canvas().Capture()
		for _, object := range test.LaidOutObjects(g.markdownPanel) {
			if surface, ok := object.(*markdownHorizontalScrollSurface); ok {
				surface.horizontal.ScrollToOffset(fyne.NewPos(80, 0))
			}
		}
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
