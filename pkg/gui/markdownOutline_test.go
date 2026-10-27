package gui

import (
	"emanuelelongo/proview/internal/markdown"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func outlineDocument(t *testing.T, source string) *markdown.Document {
	t.Helper()
	d, err := markdown.Load("outline.md", "", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func outlineRows(v *markdownView) []*markdownOutlineRow {
	var rows []*markdownOutlineRow
	var walk func(fyne.CanvasObject)
	walk = func(object fyne.CanvasObject) {
		if row, ok := object.(*markdownOutlineRow); ok {
			rows = append(rows, row)
			return
		}
		switch o := object.(type) {
		case *fyne.Container:
			for _, child := range o.Objects {
				walk(child)
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(o).Objects() {
				walk(child)
			}
		}
	}
	walk(v.outline)
	return rows
}

func TestMarkdownOutlineIncludesAllHeadingLevelsAndTracksNavigation(t *testing.T) {
	var source strings.Builder
	for level := 1; level <= 6; level++ {
		fmt.Fprintf(&source, "%s Section **%d**\n\n%s\n\n", strings.Repeat("#", level), level, strings.Repeat("Reading paragraph with enough content to scroll.\n\n", 14))
	}
	source.WriteString("## Repeat\n\nText.\n\n## Repeat\n\n```md\n# This is code\n```\n\n<h2>Literal HTML</h2>\n\n" + strings.Repeat("Following paragraph.\n\n", 20))
	g, _ := newDropTestGui(t)
	g.window.Show()
	g.addMarkdown(outlineDocument(t, source.String()), nil)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(1100, 740))
	g.window.Canvas().Capture()
	v := g.markdownView
	test.Tap(v.outlineToggle.button)
	if len(v.headings) != 8 || v.outline.Length() != 8 || v.activeHeading != 0 {
		t.Fatalf("unexpected outline: %d headings, active=%d", len(v.headings), v.activeHeading)
	}
	for i := range 6 {
		if v.headings[i].level != i+1 || v.headings[i].title != fmt.Sprintf("Section %d", i+1) {
			t.Fatalf("heading %d lost level or inline text", i)
		}
	}
	if v.headings[6].title != "Repeat" || v.headings[7].title != "Repeat" {
		t.Fatal("duplicate headings were lost")
	}
	if g.toolPropertyPanel.Visible() || g.rightPanel.MinSize().Width != markdownOutlineRailWidth {
		t.Fatal("outline did not replace the tool rail")
	}
	v.outline.Select(3)
	if v.activeHeading != 3 {
		t.Fatalf("outline click did not select the section: %d", v.activeHeading)
	}
	start := v.scroll.Offset
	v.scroll.ScrollToOffset(fyne.NewPos(0, start.Y+150))
	var row *markdownOutlineRow
	for _, candidate := range outlineRows(v) {
		if candidate.index == 3 {
			row = candidate
			break
		}
	}
	if row == nil {
		t.Fatal("current heading row is not visible")
	}
	position := g.app.Driver().AbsolutePositionForObject(row).Add(fyne.NewPos(90, 18))
	test.TapCanvas(g.window.Canvas(), position)
	if v.scroll.Offset != start || !row.marker.Visible() {
		t.Fatal("clicking the current heading did not return to its start")
	}
	v.scroll.ScrollToTop()
	v.scroll.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: -(v.headings[2].text.pageY - markdownPageMargin)}})
	if v.activeHeading != 2 {
		t.Fatal("wheel scrolling did not update the current-section indicator")
	}
	v.openLink("#section-5")
	if v.activeHeading != 4 {
		t.Fatal("internal anchor did not update the indicator")
	}
	g.handleMarkdownKey(fyne.KeyEnd)
	if v.activeHeading != 7 {
		t.Fatal("End did not activate the final section")
	}
	g.handleMarkdownKey(fyne.KeyHome)
	if v.activeHeading != 0 {
		t.Fatal("Home did not activate the first section")
	}
	if path := os.Getenv("PROVIEW_MARKDOWN_OUTLINE_PREVIEW"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, g.window.Canvas().Capture())
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	before := v.scroll.Offset
	g.startCrop()
	if !g.cropAction.button.Disabled() || v.scroll.Offset != before || g.working {
		t.Fatal("Crop is still available for Markdown")
	}
	for _, item := range g.menuDocumentItems {
		if item.Label == "Crop" && !item.Disabled {
			t.Fatal("Crop menu remains enabled")
		}
	}
}

func TestMarkdownOutlineSurvivesReflowThemeAndWorkspaceSwitches(t *testing.T) {
	g, callbacks := newDropTestGui(t)
	g.window.Show()
	g.addMarkdown(outlineDocument(t, "# Before\n\n```mermaid\nflowchart TD\n A --> B --> C --> D --> E\n```\n\n## After\n\n"+strings.Repeat("Following paragraph.\n\n", 30)), nil)
	defer g.clearMarkdown()
	g.window.Canvas().Capture()
	g.markdownView.navigateHeading(1)
	settleMarkdownDiagrams(t, g, callbacks)
	if g.markdownView.headings[1].text.pageY > g.markdownView.scroll.Offset.Y+markdownPageMargin+1 && g.markdownView.activeHeading != 0 {
		t.Fatal("diagram completion did not update the section at the retained reading position")
	}
	first := g.activeDocument
	v := g.markdownView
	test.Tap(v.outlineToggle.button)
	v.navigateHeading(len(v.headings) - 1)
	if v.activeHeading != len(v.headings)-1 {
		t.Fatal("following heading was not reached")
	}
	g.zoomIn()
	g.setMarkdownTheme("dark")
	g.window.Resize(fyne.NewSize(850, 740))
	g.window.Canvas().Capture()
	settleMarkdownDiagrams(t, g, callbacks)
	v = g.markdownView
	v.navigateHeading(len(v.headings) - 1)
	index, offset := v.activeHeading, v.scroll.Offset
	g.addDocument(testDocument(t, 80, 60), nil, nil)
	if !g.toolPropertyPanel.Visible() || g.rightPanel.Objects[0] != g.toolPropertyPanel || g.cropAction.button.Disabled() {
		t.Fatal("switching to an image did not restore editing controls")
	}
	g.selectDocument(first)
	settleMarkdownDiagrams(t, g, callbacks)
	if g.markdownView.activeHeading != index || g.markdownView.scroll.Offset != offset || g.toolPropertyPanel.Visible() {
		t.Fatal("switching back lost reading position or outline state")
	}
	g.addMarkdown(outlineDocument(t, "Plain document without headings."), nil)
	if len(g.markdownView.headings) != 0 || g.markdownView.outline.Length() != 0 {
		t.Fatal("document without headings retained a stale outline")
	}
	g.requestCloseDocument()
	g.addDocument(testDocument(t, 40, 40), nil, nil)
	if !g.toolPropertyPanel.Visible() {
		t.Fatal("closing Markdown left the tool panel hidden")
	}
}

func TestMarkdownOutlineVirtualizesLongIndexes(t *testing.T) {
	var source strings.Builder
	for i := range 400 {
		if i == 0 {
			source.WriteString("Heading 0\n---------\n\nParagraph.\n\n")
			continue
		}
		fmt.Fprintf(&source, "## Heading %d\n\nParagraph.\n\n", i)
	}
	g, _ := newDropTestGui(t)
	g.window.Show()
	g.addMarkdown(outlineDocument(t, source.String()), nil)
	defer g.clearMarkdown()
	g.window.Canvas().Capture()
	v := g.markdownView
	if len(v.headings) != 400 || v.headings[0].level != 2 {
		t.Fatal("outline was truncated")
	}
	test.Tap(v.outlineToggle.button)
	count := len(outlineRows(v))
	if count > 50 {
		t.Fatalf("index rendered %d rows outside its viewport", count)
	}
	offset := v.scroll.Offset
	position := g.app.Driver().AbsolutePositionForObject(v.outline).Add(fyne.NewPos(30, 30))
	test.Scroll(g.window.Canvas(), position, 0, -160)
	if v.outline.GetScrollOffset() <= 0 || v.scroll.Offset != offset {
		t.Fatal("floating index did not scroll independently of the document")
	}
	v.navigateHeading(399)
	if v.activeHeading != 399 {
		t.Fatal("last heading is not reachable")
	}
}

func TestMarkdownOutlineOverlayKeepsDocumentGeometryAndInput(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.window.Show()
	source := "# Start\n\n" + strings.Repeat("A paragraph of reading content with enough words to wrap at narrow widths.\n\n", 25) + "## Middle\n\n" + strings.Repeat("Another reading paragraph.\n\n", 25) + "## End\n\n" + strings.Repeat("Final paragraph.\n\n", 25)
	g.addMarkdown(outlineDocument(t, source), nil)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(1100, 740))
	g.window.Canvas().Capture()
	v := g.markdownView
	preview := func(state string) {
		if path := os.Getenv("PROVIEW_MARKDOWN_OUTLINE_TOGGLE_PREVIEW"); path != "" {
			file, err := os.Create(path + "-" + state + ".png")
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(file, g.window.Canvas().Capture())
			file.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if g.markdownOutlineExpanded || v.outlineContent.Visible() || g.rightPanel.MinSize().Width != markdownOutlineRailWidth || v.outlineToggle.button.Icon.Name() != tablerIcon("list-tree").Name() {
		t.Fatal("Markdown did not start with the compact list-tree toggle")
	}
	v.navigateHeading(1)
	preview("closed")
	windowSize, readingWidth := g.window.Canvas().Size(), v.scroll.Size().Width
	offset, contentSize := v.scroll.Offset, v.content.Size()
	textSizes := make([]fyne.Size, len(v.textBlocks))
	for i, block := range v.textBlocks {
		textSizes[i] = block.Size()
	}
	body, content := v.body, v.content
	buttonPosition := g.app.Driver().AbsolutePositionForObject(v.outlineToggle.button)
	test.TapCanvas(g.window.Canvas(), buttonPosition.Add(fyne.NewPos(18, 18)))
	if !g.markdownOutlineExpanded || !v.outlinePanel.Visible() || g.rightPanel.MinSize().Width != markdownOutlineRailWidth || v.scroll.Size().Width != readingWidth {
		t.Fatal("toggle did not reveal the floating outline without changing the reading width")
	}
	if v.activeHeading != 1 || g.window.Canvas().Size() != windowSize || v.body != body || v.content != content {
		t.Fatal("opening the outline reset reading state or rebuilt/resized the document")
	}
	preview("open")
	if v.scroll.Offset != offset || v.content.Size() != contentSize {
		t.Fatal("opening the overlay changed scroll position or document layout")
	}
	for i, block := range v.textBlocks {
		if block.Size() != textSizes[i] {
			t.Fatal("opening the overlay reflowed text")
		}
	}
	surface := v.outlinePanel.Objects[0].(*markdownOutlineSurface)
	background := test.WidgetRenderer(surface).(*markdownOutlineSurfaceRenderer).background
	alpha := color.NRGBAModel.Convert(background.FillColor).(color.NRGBA).A
	if alpha == 0 || alpha == 255 || g.window.Canvas().Overlays().Top() != nil {
		t.Fatal("outline is not a translucent nonmodal canvas layer")
	}
	documentPosition := g.app.Driver().AbsolutePositionForObject(v.scroll.Scroll).Add(fyne.NewPos(25, 100))
	test.Scroll(g.window.Canvas(), documentPosition, 0, -40)
	if v.scroll.Offset.Y <= offset.Y {
		t.Fatal("overlay blocked scrolling the uncovered reading area")
	}
	offset = v.scroll.Offset
	indexPosition := g.app.Driver().AbsolutePositionForObject(v.outline).Add(fyne.NewPos(30, 30))
	test.Scroll(g.window.Canvas(), indexPosition, 0, -40)
	if v.scroll.Offset != offset {
		t.Fatal("scrolling inside the index leaked into the document")
	}
	openPosition := g.app.Driver().AbsolutePositionForObject(v.outlineToggle.button)
	if openPosition != buttonPosition {
		t.Fatalf("toggle moved with the panel: %v != %v", openPosition, buttonPosition)
	}
	test.Tap(v.outlineToggle.button)
	if g.markdownOutlineExpanded || v.outlineContent.Visible() || v.scroll.Size().Width != readingWidth {
		t.Fatal("closing the overlay changed the reading width")
	}
	v.navigateHeading(2)
	if v.activeHeading != 2 {
		t.Fatal("collapsed outline stopped tracking the current section")
	}
	test.Tap(v.outlineToggle.button)
	currentVisible := false
	for _, row := range outlineRows(v) {
		if row.index == 2 && row.marker.Visible() {
			currentVisible = true
		}
	}
	if !currentVisible {
		t.Fatal("reopening did not reveal the current-section indicator")
	}
	g.zoomIn()
	g.setMarkdownTheme("dark")
	if !g.markdownOutlineExpanded || !v.outlineContent.Visible() {
		t.Fatal("zoom or theme reset the outline visibility")
	}
	entry := g.activeDocument
	g.addDocument(testDocument(t, 40, 40), nil, nil)
	g.selectDocument(entry)
	if !g.markdownOutlineExpanded || !g.markdownView.outlineContent.Visible() {
		t.Fatal("workspace switching reset the expanded outline")
	}
	g.window.Resize(fyne.NewSize(900, 650))
	g.window.Canvas().Capture()
	v = g.markdownView
	if v.outlinePanel.Position().X+v.outlinePanel.Size().Width > v.scroll.Size().Width || v.outlinePanel.Position().Y+v.outlinePanel.Size().Height > v.scroll.Size().Height {
		t.Fatal("resizing left the overlay outside the viewport")
	}
	if !g.cancelCurrentOperation() || g.markdownOutlineExpanded || v.outlinePanel.Visible() {
		t.Fatal("Escape did not dismiss the overlay")
	}
	v.scroll.ScrollToTop()
	g.window.Canvas().Capture()
	for _, click := range []struct {
		name     string
		position fyne.Position
	}{
		{"paper margin", g.app.Driver().AbsolutePositionForObject(v.scroll.Scroll).Add(fyne.NewPos(25, 30))},
		{"heading text", v.textBlocks[0].absolutePosition().Add(fyne.NewPos(10, 10))},
	} {
		v.toggleOutline()
		test.TapCanvas(g.window.Canvas(), click.position)
		if g.markdownOutlineExpanded || v.outlinePanel.Visible() {
			t.Fatalf("click on %s did not dismiss the outline", click.name)
		}
	}
	v.toggleOutline()
	v.openLink("#middle")
	if g.markdownOutlineExpanded || v.activeHeading != 1 {
		t.Fatal("anchor link did not dismiss the outline and navigate")
	}
}
