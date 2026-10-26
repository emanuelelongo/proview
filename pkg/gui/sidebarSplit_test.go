package gui

import (
	"emanuelelongo/proview/internal/markdown"
	"image/color"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

type sidebarRefreshCounter struct {
	*canvas.Rectangle
	refreshes int
}

func (c *sidebarRefreshCounter) Refresh() {
	c.refreshes++
	c.Rectangle.Refresh()
}

func TestSidebarDragOnlyRelayoutsChildren(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	left := &sidebarRefreshCounter{Rectangle: canvas.NewRectangle(color.White)}
	right := &sidebarRefreshCounter{Rectangle: canvas.NewRectangle(color.Black)}
	changes := 0
	split := newFixedSidebarSplit(left, right, minSidebarWidth, func() { changes++ })
	split.Resize(fyne.NewSize(1000, 700))
	renderer := test.WidgetRenderer(split).(*fixedSidebarSplitRenderer)
	left.refreshes, right.refreshes = 0, 0
	renderer.divider.onDrag(40)
	if split.width != 180 || left.Size().Width != 180 || right.Position().X != 186 || right.Size().Width != 814 {
		t.Fatal("sidebar drag did not resize the document viewport")
	}
	if left.refreshes != 0 || right.refreshes != 0 || changes != 1 {
		t.Fatal("sidebar drag refreshed child content instead of only changing geometry")
	}
	renderer.divider.onDrag(-1000)
	changes = 0
	renderer.divider.onDrag(-10)
	if changes != 0 {
		t.Fatal("drag beyond the width limit repeated layout work")
	}
	split.Refresh()
	if left.refreshes == 0 || right.refreshes == 0 {
		t.Fatal("explicit refresh no longer propagates theme changes")
	}
}

func TestMarkdownSidebarDragRetainsCardsAndThumbnail(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.closeWindow()
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	g.addDocument(testDocument(t, 40, 30), nil, nil)
	g.addMarkdown(readingDocument(t), nil)
	entry := g.activeDocument
	for entry.thumbnail == nil {
		driver.runNext(t)
	}
	thumbnail := entry.thumbnail
	cards := append([]fyne.CanvasObject(nil), g.documentList.Objects...)
	renderer := test.WidgetRenderer(g.sidebarSplit).(*fixedSidebarSplitRenderer)
	for _, delta := range []float32{20, 40, -10, -30} {
		renderer.divider.onDrag(delta)
		if entry.thumbnail != thumbnail || entry.thumbnailCancel != nil {
			t.Fatal("sidebar drag regenerated the Markdown thumbnail")
		}
		for i, card := range cards {
			if g.documentList.Objects[i] != card {
				t.Fatal("sidebar drag rebuilt document cards")
			}
		}
	}
	card := cards[len(cards)-1].(*fyne.Container)
	preview := card.Objects[0].(*canvas.Image)
	if preview.MinSize() != thumbnailSizeForSidebar(thumbnail.Bounds().Size(), g.sidebarSplit.width) {
		t.Fatal("retained preview did not follow sidebar width")
	}
}

func BenchmarkMarkdownSidebarResize(b *testing.B) {
	base := test.NewApp()
	driver := &dropTestDriver{Driver: base.Driver(), callbacks: make(chan func(), 16)}
	app := &dropTestApp{App: base, driver: driver}
	fyne.SetCurrentApp(app)
	g := newGui(app)
	b.Cleanup(func() { g.closeWindow(); app.Quit() })
	g.window.Resize(fyne.NewSize(1000, 740))
	g.window.Show()
	source := "# Resizing a long document\n\n" + strings.Repeat("## A section\n\nThis paragraph wraps over several lines while the sidebar changes width. It contains **styled words** and `inline code`.\n\n```go\nfunc example() {\n    println(123)\n}\n```\n\n", 60)
	doc, err := markdown.Load("long.md", "", strings.NewReader(source))
	if err != nil {
		b.Fatal(err)
	}
	g.addMarkdown(doc, nil)
	g.addMarkdown(doc, nil)
	for g.documents[0].thumbnailCancel != nil || g.documents[1].thumbnailCancel != nil {
		select {
		case callback := <-driver.callbacks:
			callback()
		case <-time.After(5 * time.Second):
			b.Fatal("thumbnail generation timed out")
		}
	}
	renderer := test.WidgetRenderer(g.sidebarSplit).(*fixedSidebarSplitRenderer)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderer.divider.onDrag(2)
		renderer.divider.onDrag(-2)
	}
}
