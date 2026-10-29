package gui

import (
	"bytes"
	"emanuelelongo/proview/internal/markdown"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func readingDocument(t *testing.T) *markdown.Document {
	t.Helper()
	data, err := os.ReadFile("../../internal/markdown/testdata/reading.md")
	if err != nil {
		t.Fatal(err)
	}
	d, err := markdown.Load("reading.md", "", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestMarkdownWorkspacePreservesReadingStateAndDisablesEditing(t *testing.T) {
	g, _ := newDropTestGui(t)
	app := g.app
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	d := readingDocument(t)
	g.addMarkdown(d, storage.NewFileURI("/tmp/reading.md"))
	if g.document != nil || !g.hasOpenContent() || !g.markdownPanel.Visible() || g.imageScroll.Visible() || g.pdfScroll.Visible() {
		t.Fatal("wrong document mode")
	}
	if g.window.Title() != "reading.md — Proview" || !g.saveItem.Disabled || !g.saveAsItem.Disabled || !g.signatureItem.Disabled || !g.cropAction.button.Disabled() {
		t.Fatal("editing enabled or title missing")
	}
	for _, a := range g.annotationActions {
		if !a.button.Disabled() {
			t.Fatal("annotation enabled")
		}
	}
	original := g.activeDocument
	g.zoomIn()
	if g.markdownView.scale != 1.125 {
		t.Fatal("reading zoom did not change")
	}
	g.markdownView.scroll.ScrollToOffset(fyne.NewPos(0, 100))
	offset := g.markdownView.scroll.Offset
	g.addDocument(testDocument(t, 60, 40), nil, nil)
	if g.showingMarkdown() || g.markdownPanel.Visible() {
		t.Fatal("Markdown remained active")
	}
	g.selectDocument(original)
	if !g.showingMarkdown() || g.markdownView.doc != d || g.markdownView.scale != 1.125 || g.markdownView.scroll.Offset != offset {
		t.Fatalf("reading state lost: %v != %v", g.markdownView.scroll.Offset, offset)
	}
	g.copyArea()
	if app.Clipboard().Content() != string(d.Source) {
		t.Fatal("copy lost original source")
	}
	g.requestCloseDocument()
	if g.showingMarkdown() || len(g.documents) != 1 || g.document == nil {
		t.Fatal("close did not restore image")
	}
	g.requestCloseDocument()
}
func TestMarkdownRendersNativeBlocksAndResponsiveThemes(t *testing.T) {
	g, driver := newDropTestGui(t)
	app := g.app
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	g.addMarkdown(readingDocument(t), nil)
	for _, variant := range []fyne.ThemeVariant{theme.VariantLight, theme.VariantDark} {
		app.Settings().SetTheme(&testThemeVariant{Theme: theme.DefaultTheme(), variant: variant})
		for _, width := range []float32{1000, 500} {
			g.window.Resize(fyne.NewSize(width, 740))
			g.window.Canvas().Capture()
			settleMarkdownDiagrams(t, g, driver)
			if g.markdownView.content.Size().Width > 900 || g.markdownView.content.Size().Width < 100 {
				t.Fatal("invalid reading width")
			}
			if g.markdownView.scroll.Content.MinSize().Height <= g.markdownView.scroll.Size().Height {
				t.Fatal("long document cannot scroll")
			}
		}
	}
	texts := ""
	scroll := g.markdownView.scroll
	for offset := float32(0); offset < scroll.Content.MinSize().Height; offset += max(1, scroll.Size().Height) {
		scroll.ScrollToOffset(fyne.NewPos(0, offset))
		for _, object := range test.LaidOutObjects(g.markdownPanel) {
			if rich, ok := object.(*markdownSelectableText); ok {
				texts += rich.String() + "\n"
			}
		}
	}
	scroll.ScrollToTop()
	for _, want := range []string{"Markdown in Proview", "fmt.Println", "HTML source", "Unknown languages", "E = mc^2"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("missing rendered content %q", want)
		}
	}
	if path := os.Getenv("PROVIEW_MARKDOWN_PREVIEW"); path != "" {
		app.Settings().SetTheme(&testThemeVariant{Theme: theme.DefaultTheme(), variant: theme.VariantLight})
		g.window.Resize(fyne.NewSize(1000, 1000))
		g.markdownView.scroll.ScrollToTop()
		g.markdownView.content.Refresh()
		settleMarkdownDiagrams(t, g, driver)
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, g.window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
		g.markdownView.openLink("#code-example")
		settleMarkdownDiagrams(t, g, driver)
		lower, err := os.Create(path + ".code.png")
		if err != nil {
			t.Fatal(err)
		}
		defer lower.Close()
		if err := png.Encode(lower, g.window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
		app.Settings().SetTheme(&testThemeVariant{Theme: theme.DefaultTheme(), variant: theme.VariantDark})
		settleMarkdownDiagrams(t, g, driver)
		dark, err := os.Create(path + ".dark.png")
		if err != nil {
			t.Fatal(err)
		}
		defer dark.Close()
		if err := png.Encode(dark, g.window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
		for _, variant := range []fyne.ThemeVariant{theme.VariantLight, theme.VariantDark} {
			app.Settings().SetTheme(&testThemeVariant{Theme: theme.DefaultTheme(), variant: variant})
			settleMarkdownDiagrams(t, g, driver)
			g.markdownView.openLink("#mermaid-diagrams")
			frame, err := os.Create(fmt.Sprintf("%s.mermaid-%d.png", path, variant))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(frame, g.window.Canvas().Capture())
			frame.Close()
			if err != nil {
				t.Fatal(err)
			}
		}

	}
	g.requestCloseDocument()
}

type testThemeVariant struct {
	fyne.Theme
	variant fyne.ThemeVariant
}

func (t *testThemeVariant) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return t.Theme.Color(n, t.variant)
}

func TestMarkdownOpeningQueueAndPDFIsolation(t *testing.T) {
	g, driver := newDropTestGui(t)
	path, err := filepath.Abs("../../internal/markdown/testdata/reading.md")
	if err != nil {
		t.Fatal(err)
	}
	g.openStartupFiles([]string{path, path})
	for g.working || g.openingFiles || len(g.pendingFiles) > 0 {
		driver.runNext(t)
	}
	if !g.showingMarkdown() || len(g.documents) != 2 || g.markdownView.doc.Name != "reading.md" {
		t.Fatal("Markdown opening queue failed")
	}
	g.receiveDocument(workspacePDF(t, 1), nil, nil, func() {})
	if len(g.editors.windows) != 2 || !g.showingMarkdown() || g.editors.windows[1].document == nil {
		t.Fatal("PDF opening replaced Markdown workspace")
	}
	g.editors.windows[1].closeWindow()
	if !g.handleMarkdownKey(fyne.KeyPageDown) || g.markdownView.scroll.Offset.Y <= 0 {
		t.Fatal("keyboard scrolling failed")
	}
	g.markdownView.openLink("#code-example")
	if g.markdownView.scroll.Offset.Y <= 0 {
		t.Fatal("heading link failed")
	}
	g.closeWindow()
}

func TestMarkdownCodeScrollRoutesBothAxes(t *testing.T) {
	for _, longLine := range []bool{false, true} {
		t.Run(fmt.Sprint("overflow=", longLine), func(t *testing.T) {
			app := test.NewApp()
			defer app.Quit()
			g := newGui(app)
			g.window.Resize(fyne.NewSize(800, 600))
			g.window.Show()
			code := "short code"
			if longLine {
				code = strings.Repeat("long code ", 100)
			}
			source := "# Scroll\n\n```text\n" + code + "\n```\n\n" + strings.Repeat("Another paragraph.\n\n", 40)
			d, err := markdown.Load("scroll.md", "", strings.NewReader(source))
			if err != nil {
				t.Fatal(err)
			}
			g.addMarkdown(d, nil)
			var surface *markdownHorizontalScrollSurface
			for _, object := range test.LaidOutObjects(g.markdownPanel) {
				if found, ok := object.(*markdownHorizontalScrollSurface); ok {
					surface = found
					break
				}
			}
			if surface == nil {
				t.Fatal("code scroll routing surface missing")
			}
			for _, width := range []float32{800, 500} {
				g.window.Resize(fyne.NewSize(width, 600))
				g.window.Canvas().Capture()
				foundCopy := false
				for _, object := range test.LaidOutObjects(g.markdownPanel) {
					if button, ok := object.(*iconButton); ok && button.tooltip == "Copy code" {
						foundCopy = true
						textRight := app.Driver().AbsolutePositionForObject(surface).X + surface.Size().Width
						buttonLeft := app.Driver().AbsolutePositionForObject(button).X
						if textRight > buttonLeft {
							t.Fatal("code viewport overlaps the copy button column")
						}
					}
				}
				if !foundCopy {
					t.Fatal("code copy button missing")
				}
			}
			position := app.Driver().AbsolutePositionForObject(surface).Add(fyne.NewPos(20, 20))
			test.Scroll(g.window.Canvas(), position, 0, -40)
			if g.markdownView.scroll.Offset.Y <= 0 || surface.horizontal.Offset.X != 0 {
				t.Fatal("vertical wheel over code did not scroll the document")
			}
			g.markdownView.scroll.ScrollToTop()
			test.Scroll(g.window.Canvas(), position, -40, 0)
			if g.markdownView.scroll.Offset.Y != 0 {
				t.Fatal("horizontal scroll moved the document")
			}
			if longLine && surface.horizontal.Offset.X <= 0 {
				t.Fatal("horizontal code overflow no longer scrolls")
			}
			if !longLine && surface.horizontal.Offset.X != 0 {
				t.Fatal("short code unexpectedly moved horizontally")
			}
			if _, ok := any(surface).(fyne.Tappable); ok {
				t.Fatal("scroll surface intercepts copy/click actions")
			}
			if _, ok := any(surface).(fyne.Draggable); ok {
				t.Fatal("scroll surface intercepts scrollbar dragging")
			}
			g.requestCloseDocument()
		})
	}
}
