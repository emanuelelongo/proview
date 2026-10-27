package gui

import (
	"image/color"
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
