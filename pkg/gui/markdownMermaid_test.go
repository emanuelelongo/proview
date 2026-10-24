package gui

import (
	"bytes"
	"context"
	"emanuelelongo/proview/internal/markdown"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func settleMarkdownDiagrams(t *testing.T, g *Gui, driver *dropTestDriver) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if !g.showingMarkdown() {
			return
		}
		g.markdownView.scroll.Refresh()
		running := false
		for _, diagram := range g.markdownView.diagrams {
			running = running || diagram.running
		}
		if !running {
			return
		}
		driver.runNext(t)
	}
	t.Fatal("diagram layout did not settle")
}
func openTestMermaid(t *testing.T, g *Gui, source string) {
	t.Helper()
	d, err := markdown.Load("diagrams.md", "", strings.NewReader("# Diagrams\n\n```mermaid\n"+source+"\n```\n\nFollowing paragraph."))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(d, nil)
}
func TestMermaidPreviewRendersAndUpdatesThemeZoomAndWidth(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	g.app.Settings().SetTheme(&testThemeVariant{Theme: theme.DefaultTheme(), variant: theme.VariantLight})
	openTestMermaid(t, g, "flowchart TD\n A[Markdown] --> B{Render?}\n B -->|Yes| C[Preview]")
	settleMarkdownDiagrams(t, g, driver)
	if len(g.markdownView.diagrams) != 1 {
		t.Fatal("diagram not detected")
	}
	preview := g.markdownView.diagrams[0]
	if preview.err != nil || preview.result.Image == nil || !preview.image.Visible() || preview.status.Visible() {
		t.Fatal("diagram preview unavailable", preview.err)
	}
	height := preview.result.Height
	oldContext := g.markdownView.diagramContext
	g.zoomIn()
	settleMarkdownDiagrams(t, g, driver)
	preview = g.markdownView.diagrams[0]
	if oldContext.Err() != context.Canceled || preview.result.Height <= height {
		t.Fatal("zoom did not cancel old work and rerender")
	}
	light := preview.result.Image
	g.app.Settings().SetTheme(&testThemeVariant{Theme: theme.DefaultTheme(), variant: theme.VariantDark})
	settleMarkdownDiagrams(t, g, driver)
	if !preview.requested.Dark || bytes.Equal(light.Pix, preview.result.Image.Pix) {
		t.Fatalf("diagram did not follow dark theme: dark=%v identical=%v requested=%v", preview.requested.Dark, bytes.Equal(light.Pix, preview.result.Image.Pix), preview.requested)
	}
	width := preview.requested.Width
	g.window.Resize(fyne.NewSize(550, 740))
	settleMarkdownDiagrams(t, g, driver)
	if preview.requested.Width >= width || preview.result.Image == nil {
		t.Fatal("diagram did not adapt to narrower viewport")
	}
	ctx := g.markdownView.diagramContext
	g.requestCloseDocument()
	driver.drain()
	if ctx.Err() != context.Canceled || g.showingMarkdown() {
		t.Fatal("closing retained diagram work")
	}
}
func TestMermaidFailurePreservesVisibleSourceAndFollowingContent(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(900, 740))
	g.window.Show()
	openTestMermaid(t, g, "flowchart LR\n A[Broken")
	settleMarkdownDiagrams(t, g, driver)
	preview := g.markdownView.diagrams[0]
	if preview.err == nil || preview.image.Visible() || !preview.status.Visible() {
		t.Fatal("invalid Mermaid rendered as a diagram")
	}
	sourceVisible := preview.fallback.Visible()
	sourceAccordion := false
	text := ""
	for _, object := range test.LaidOutObjects(g.markdownPanel) {
		if _, ok := object.(*widget.Accordion); ok {
			sourceAccordion = true
		}
		switch rich := object.(type) {
		case *widget.RichText:
			text += rich.String() + "\n"
		case *markdownSelectableText:
			text += rich.String() + "\n"
		}
	}
	if !sourceVisible || sourceAccordion || !strings.Contains(text, "A[Broken") || !strings.Contains(text, "Following paragraph.") {
		t.Fatal("fallback lost source or following Markdown")
	}
}
func TestMermaidClosingWhileRenderingDoesNotPublishLateResult(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.window.Resize(fyne.NewSize(900, 740))
	g.window.Show()
	openTestMermaid(t, g, "flowchart LR\n A --> B")
	ctx := g.markdownView.diagramContext
	g.requestCloseDocument()
	driver.drain()
	if ctx.Err() != context.Canceled || g.hasOpenContent() || g.markdownPanel.Visible() {
		t.Fatal("closed diagram view remained active")
	}
}

func TestMermaidWorkspaceRestoresOffsetAfterDeferredLayout(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(900, 600))
	g.window.Show()
	openTestMermaid(t, g, "flowchart TD\n A --> B --> C --> D --> E --> F --> G --> H")
	first := g.activeDocument
	other, err := markdown.Load("other.md", "", strings.NewReader("# Other\n\nAnother document."))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(other, nil)
	g.selectDocument(first)
	settleMarkdownDiagrams(t, g, driver)
	g.markdownView.scroll.ScrollToBottom()
	offset := g.markdownView.scroll.Offset
	if offset.Y <= 0 {
		t.Fatal("test diagram does not overflow the viewport")
	}
	g.selectDocument(g.documents[1])
	g.selectDocument(first)
	settleMarkdownDiagrams(t, g, driver)
	if g.markdownView.scroll.Offset != offset {
		t.Fatalf("deferred diagram layout lost reading position: got %v want %v", g.markdownView.scroll.Offset, offset)
	}
	g.selectDocument(g.documents[1])
	g.selectDocument(first)
	if !g.handleMarkdownKey(fyne.KeyHome) {
		t.Fatal("reader navigation was not accepted")
	}
	settleMarkdownDiagrams(t, g, driver)
	if g.markdownView.scroll.Offset.Y != 0 {
		t.Fatal("late layout overrode explicit reader navigation")
	}

}

func TestMermaidCodeButtonCopiesSourceWithoutSourceSection(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(900, 700))
	g.window.Show()
	openTestMermaid(t, g, "flowchart LR\n    A[Start] --> B[Done]")
	preview := g.markdownView.diagrams[0]
	test.Tap(preview.copyCode)
	if g.app.Clipboard().Content() != preview.source {
		t.Fatal("copy code is unavailable while loading")
	}
	settleMarkdownDiagrams(t, g, driver)
	if preview.fallback.Visible() || !preview.copyCode.Visible() {
		t.Fatal("successful diagram exposes source or hides its copy action")
	}
	for _, object := range test.LaidOutObjects(g.markdownPanel) {
		if _, ok := object.(*widget.Accordion); ok {
			t.Fatal("Mermaid source section is still present")
		}
	}
	g.app.Clipboard().SetContent("")
	test.Tap(preview.copyCode)
	if g.app.Clipboard().Content() != preview.source {
		t.Fatal("copy code changed whitespace or contents")
	}
}
