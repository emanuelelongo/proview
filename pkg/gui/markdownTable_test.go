package gui

import (
	"emanuelelongo/proview/internal/markdown"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func markdownTableLayouts(v *markdownView) []*markdownTableViewportLayout {
	var tables []*markdownTableViewportLayout
	for _, block := range v.content.Objects {
		if c, ok := block.(*fyne.Container); ok {
			if l, ok := c.Layout.(*markdownTableViewportLayout); ok {
				tables = append(tables, l)
			}
		}
	}
	return tables
}

func TestMarkdownTablesFitContentAndReflow(t *testing.T) {
	source := "# Tables\n\n| Status | Count |\n| --- | ---: |\n| OK | 23 |\n| Failure | 3 |\n\n" +
		"| Operation | Result | Evidence |\n| --- | :---: | --- |\n" +
		"| Create resource | **OK** | " + strings.Repeat("The resource was created successfully. ", 5) + " |\n" +
		"| Delete | Failure | Missing permissions. |\n\nFollowing paragraph.\n"
	d, err := markdown.Load("tables.md", "", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	g, _ := newDropTestGui(t)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(1300, 740))
	g.window.Show()
	g.addMarkdown(d, nil)
	for _, variant := range []fyne.ThemeVariant{theme.VariantLight, theme.VariantDark} {
		g.app.Settings().SetTheme(&testThemeVariant{Theme: theme.DefaultTheme(), variant: variant})
		for _, width := range []float32{1300, 800, 1300} {
			g.window.Resize(fyne.NewSize(width, 740))
			g.window.Canvas().Capture()
			v := g.markdownView
			tables := markdownTableLayouts(v)
			if len(tables) != 2 {
				t.Fatal("tables missing")
			}
			short, long := tables[0].table, tables[1].table
			if short.minimumSize.Width >= v.content.Size().Width*.75 {
				t.Fatal("short table was stretched to the page width")
			}
			if long.widths[2] <= long.widths[1]*2 {
				t.Fatal("long description did not get more space than the short result")
			}
			if long.minimumSize.Width > v.content.Size().Width+1 {
				t.Fatal("ordinary words caused unnecessary horizontal overflow")
			}
			for _, l := range tables {
				for _, row := range l.table.rows {
					for col, cell := range row {
						if cell.Position().X != l.table.rows[0][col].Position().X || cell.Size().Width != l.table.rows[0][col].Size().Width {
							t.Fatal("columns do not line up across rows")
						}
						if cell.Size().Height < cell.MinSize().Height {
							t.Fatal("wrapped cell is clipped")
						}
					}
				}
			}
			if path := os.Getenv("PROVIEW_MARKDOWN_TABLE_PREVIEW"); path != "" && width == 1300 {
				file, err := os.Create(path + "-" + strconv.Itoa(int(variant)) + ".png")
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
	}
	before := markdownTableLayouts(g.markdownView)[0].table.minimumSize.Width
	g.zoomIn()
	g.window.Canvas().Capture()
	if after := markdownTableLayouts(g.markdownView)[0].table.minimumSize.Width; after <= before {
		t.Fatal("natural table width did not follow reading zoom")
	}
}

func TestMarkdownWideTableScrollRoutingAndSelectionGeometry(t *testing.T) {
	source := "# Wide table\n\n| Resource | Reference |\n| --- | ---: |\n| Resource | [" + strings.Repeat("reference", 30) + "](https://example.com) |\n\n" +
		strings.Repeat("Following paragraph.\n\n", 40)
	d, err := markdown.Load("wide.md", "", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	g, _ := newDropTestGui(t)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(700, 740))
	g.window.Show()
	g.addMarkdown(d, nil)
	v := g.markdownView
	tables := markdownTableLayouts(v)
	if len(tables) != 1 {
		t.Fatal("table missing")
	}
	l := tables[0]
	if l.table.minimumSize.Width <= v.content.Size().Width {
		t.Fatal("long identifier did not overflow")
	}
	link := l.table.rows[1][1].text.Segments[0].(*widget.HyperlinkSegment)
	if link.Alignment != fyne.TextAlignTrailing {
		t.Fatal("link lost column alignment")
	}
	var surface *markdownHorizontalScrollSurface
	for _, object := range test.LaidOutObjects(v.page) {
		if s, ok := object.(*markdownHorizontalScrollSurface); ok {
			surface = s
			break
		}
	}
	if surface == nil {
		t.Fatal("table scroll surface missing")
	}
	position := g.app.Driver().AbsolutePositionForObject(surface).Add(fyne.NewPos(30, 30))
	test.Scroll(g.window.Canvas(), position, 0, -40)
	if v.scroll.Offset.Y <= 0 || l.scroll.Offset.X != 0 {
		t.Fatal("vertical wheel over table did not scroll document")
	}
	v.scroll.ScrollToTop()
	test.Scroll(g.window.Canvas(), position, -80, 0)
	if l.scroll.Offset.X <= 0 || v.scroll.Offset.Y != 0 {
		t.Fatal("horizontal wheel did not scroll only the table")
	}
	cell := l.table.rows[1][0].object.(*markdownSelectableText)
	expected := g.app.Driver().AbsolutePositionForObject(cell)
	actual := cell.absolutePosition()
	if math.Abs(float64(expected.X-actual.X)) > .1 || math.Abs(float64(expected.Y-actual.Y)) > .1 {
		t.Fatalf("selection geometry did not follow horizontal scroll: %v != %v", actual, expected)
	}
	if len(cell.fragments()) == 0 {
		t.Fatal("horizontal scroll lost selectable text")
	}
}
