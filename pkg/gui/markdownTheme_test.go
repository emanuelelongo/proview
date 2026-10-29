package gui

import (
	"emanuelelongo/proview/internal/markdown"
	"fmt"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
	"github.com/alecthomas/chroma/v2"
	"image/color"
	"strconv"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestMarkdownThemePreferenceAcrossWindows(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.window.Show()
	g.addMarkdown(readingDocument(t), nil)
	defer g.clearMarkdown()
	other := g.newEditor()
	other.window.Show()
	other.addMarkdown(readingDocument(t), nil)
	defer other.clearMarkdown()
	g.zoomIn()
	g.markdownView.scroll.ScrollToOffset(fyne.NewPos(0, 150))
	offset, scale := g.markdownView.scroll.Offset, g.markdownView.scale
	appTheme := g.app.Settings().Theme()
	for _, choice := range []struct {
		mode    string
		variant fyne.ThemeVariant
		index   int
	}{{"light", theme.VariantLight, 1}, {"dark", theme.VariantDark, 2}} {
		g.markdownThemeItems[choice.index].Action()
		if g.app.Preferences().String(markdownThemePreferenceKey) != choice.mode || g.app.Settings().Theme() != appTheme {
			t.Fatal("Markdown preference was not saved independently of the application theme")
		}
		for _, editor := range []*Gui{g, other} {
			v := editor.markdownView
			fg, bg, _ := v.themeState()
			wantFG := theme.DefaultTheme().Color(theme.ColorNameForeground, choice.variant)
			wantBG := theme.DefaultTheme().Color(theme.ColorNameBackground, choice.variant)
			if color.NRGBAModel.Convert(fg) != color.NRGBAModel.Convert(wantFG) || color.NRGBAModel.Convert(bg) != color.NRGBAModel.Convert(wantBG) {
				t.Fatal("Markdown text did not follow the selected theme")
			}
			renderer := fyneTest.WidgetRenderer(v.body.Objects[0].(*markdownPageSurface)).(*markdownPageRenderer)
			wantWorkspace := appTheme.Color(theme.ColorNameBackground, g.app.Settings().ThemeVariant())
			if color.NRGBAModel.Convert(renderer.workspace.FillColor) != color.NRGBAModel.Convert(wantWorkspace) || color.NRGBAModel.Convert(renderer.paper.FillColor) != color.NRGBAModel.Convert(lightenMarkdownPage(wantBG)) {
				t.Fatal("reading theme did not stay confined to the paper")
			}
			if !editor.markdownThemeItems[choice.index].Checked {
				t.Fatal("theme menu did not update across windows")
			}
		}
		if g.markdownView.scale != scale || g.markdownView.scroll.Offset != offset {
			t.Fatal("theme change lost reading position or zoom")
		}
	}
	newWindow := g.newEditor()
	newWindow.addMarkdown(readingDocument(t), nil)
	defer newWindow.clearMarkdown()
	if !newWindow.markdownThemeItems[2].Checked || newWindow.markdownView.readingTheme().mode != "dark" {
		t.Fatal("new window did not restore the saved preference")
	}
	g.markdownThemeItems[0].Action()
	if g.markdownThemeMode() != "" || !other.markdownThemeItems[0].Checked {
		t.Fatal("system theme was not restored")
	}
}

func TestMarkdownThemeChoiceUpdatesMermaid(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.window.Show()
	g.addMarkdown(readingDocument(t), nil)
	defer g.clearMarkdown()
	for _, mode := range []string{"light", "dark"} {
		g.setMarkdownTheme(mode)
		settleMarkdownDiagrams(t, g, driver)
		if len(g.markdownView.diagrams) == 0 {
			t.Fatal("diagram missing")
		}
		for _, diagram := range g.markdownView.diagrams {
			if diagram.requested.Dark != (mode == "dark") {
				t.Fatal("Mermaid did not follow the explicit reading theme")
			}
		}
	}
}

func TestMarkdownLightCodeContrastOnDarkApplication(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.app.Settings().SetTheme(theme.DarkTheme())
	reading := &markdownTheme{mode: "light", scale: 1}
	// Chroma error tokens assume a colored background; the native renderer
	// renders their text on a normal card instead.
	foreground := reading.Color(fyne.ThemeColorName("markdown-token-"+strconv.Itoa(int(chroma.Error))), theme.VariantDark)
	want := theme.DefaultTheme().Color(theme.ColorNameForeground, theme.VariantLight)
	if color.NRGBAModel.Convert(foreground) != color.NRGBAModel.Convert(want) {
		t.Fatalf("error token has unreadable color: %v", foreground)
	}
	keyword := reading.Color(fyne.ThemeColorName("markdown-token-"+strconv.Itoa(int(chroma.Keyword))), theme.VariantDark)
	if color.NRGBAModel.Convert(keyword) == color.NRGBAModel.Convert(want) {
		t.Fatal("readable syntax highlighting was discarded")
	}
}

func TestMarkdownCodeSegmentsFollowReadingTheme(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.app.Settings().SetTheme(theme.DarkTheme())
	g.app.Preferences().SetString(markdownThemePreferenceKey, "light")
	source := "```go\npackage main\nfunc main() { § }\n```\n\n```\nplain code\n```\n\nInline `code`.\n"
	doc, err := markdown.Load("code.md", "", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(doc, nil)
	defer g.clearMarkdown()
	for _, block := range g.markdownView.textBlocks {
		block.Refresh()
		for _, segment := range block.Segments {
			text, ok := segment.(*widget.TextSegment)
			if !ok || strings.TrimSpace(text.Text) == "" {
				continue
			}
			visual := text.Visual().(*canvas.Text)
			r, green, b, _ := visual.Color.RGBA()
			if r > 60000 && green > 60000 && b > 60000 {
				t.Fatalf("white-on-white code segment %q, color %v", text.Text, visual.Color)
			}
		}
	}
}

func TestMarkdownOffscreenTextKeepsLightTheme(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.app.Settings().SetTheme(theme.DarkTheme())
	g.app.Preferences().SetString(markdownThemePreferenceKey, "light")
	source := "# Mapping\n\n" + strings.Repeat("A paragraph with `inline_code` and **bold text**.\n\n- `volume_id`: the physical volume ID (`disk_uuid`) of the disk.\n\n```bash\nprintf '%s' \"$cluster_id\"\n```\n\n", 50)
	doc, err := markdown.Load("scrolling.md", "", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(doc, nil)
	defer g.clearMarkdown()
	defer g.closeSearch()
	g.window.Resize(fyne.NewSize(1000, 700))
	g.window.Show()
	g.window.Canvas().Capture()
	g.openSearch()
	g.search.entry.SetText("cluster_id")
	for g.search.running {
		driver.runNext(t)
	}
	// Fyne evicts idle renderers together with their widget theme overrides.
	t.Run("expire embedded text renderers", func(t *testing.T) {
		for _, block := range g.markdownView.textBlocks {
			fyneTest.TempWidgetRenderer(t, &block.RichText)
		}
	})
	for _, block := range g.markdownView.textBlocks {
		block.Refresh()
		for _, object := range block.renderer.WidgetRenderer.Objects() {
			if text, ok := object.(*canvas.Text); ok && strings.TrimSpace(text.Text) != "" {
				r, green, b, _ := text.Color.RGBA()
				if r > 60000 && green > 60000 && b > 60000 {
					t.Fatalf("white text after renderer eviction: %q", text.Text)
				}
			}
		}
	}
	for _, index := range []int{25, 49, 0} {
		g.showSearchHit(index)
		g.window.Canvas().Capture()
		// Reflow exposes new lines that were absent from the theme traversal.
		g.window.Resize(fyne.NewSize(850+float32(index), 700))
		g.window.Canvas().Capture()
		for _, block := range g.markdownView.textBlocks {
			if block.renderer == nil {
				continue
			}
			for _, object := range block.renderer.WidgetRenderer.Objects() {
				text, ok := object.(*canvas.Text)
				if !ok || strings.TrimSpace(text.Text) == "" {
					continue
				}
				r, green, b, _ := text.Color.RGBA()
				if r > 60000 && green > 60000 && b > 60000 {
					t.Fatalf("white text after navigation: %q in %q", text.Text, block.text)
				}
			}
		}
	}
}

func TestMarkdownTableRendererRecreationKeepsReadingTheme(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.app.Settings().SetTheme(theme.DarkTheme())
	g.app.Preferences().SetString(markdownThemePreferenceKey, "light")
	doc, err := markdown.Load("table.md", "", strings.NewReader("| Flag | Description |\n| --- | --- |\n| `--gateway-id` | The **dashboard** command uses `GATEWAY_ID`. |\n| `--tenant-id` | Tenant value. |\n"))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(doc, nil)
	defer g.clearMarkdown()
	g.window.Resize(fyne.NewSize(900, 600))
	g.window.Show()
	g.window.Canvas().Capture()
	for cycle := 0; cycle < 2; cycle++ {
		t.Run(fmt.Sprint("expire", cycle), func(t *testing.T) {
			for _, b := range g.markdownView.textBlocks {
				fyneTest.TempWidgetRenderer(t, &b.RichText)
				fyneTest.TempWidgetRenderer(t, b)
			}
		})
		for _, b := range g.markdownView.textBlocks {
			// Painting can recreate a renderer directly, without Resize or Refresh.
			renderer := fyneTest.WidgetRenderer(b)
			for _, object := range renderer.Objects() {
				if text, ok := object.(*canvas.Text); ok && strings.TrimSpace(text.Text) != "" {
					r, green, blue, _ := text.Color.RGBA()
					if r > 60000 && green > 60000 && blue > 60000 {
						t.Fatalf("white table text after recreation: %q", text.Text)
					}
				}
			}
		}
	}
}
