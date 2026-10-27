package gui

import "fyne.io/fyne/v2"

const markdownThemePreferenceKey = "markdown.theme"

func (g *Gui) markdownThemeMode() string {
	mode := g.app.Preferences().String(markdownThemePreferenceKey)
	if mode == "light" || mode == "dark" {
		return mode
	}
	return ""
}

func (v *markdownView) readingTheme() *markdownTheme {
	t := &markdownTheme{scale: v.scale}
	if v.editor != nil && v.editor.app != nil {
		t.mode = v.editor.markdownThemeMode()
	}
	return t
}

func (g *Gui) newMarkdownThemeMenu() *fyne.MenuItem {
	menu := fyne.NewMenu("")
	for _, choice := range []struct{ label, mode string }{{"System", ""}, {"Light", "light"}, {"Dark", "dark"}} {
		item := fyne.NewMenuItem(choice.label, func() { g.setMarkdownTheme(choice.mode) })
		item.Checked = g.markdownThemeMode() == choice.mode
		g.markdownThemeItems = append(g.markdownThemeItems, item)
		menu.Items = append(menu.Items, item)
	}
	item := fyne.NewMenuItem("Markdown Theme", nil)
	item.ChildMenu = menu
	return item
}

func (g *Gui) setMarkdownTheme(mode string) {
	if mode != "light" && mode != "dark" {
		mode = ""
	}
	g.app.Preferences().SetString(markdownThemePreferenceKey, mode)
	windows := []*Gui{g}
	if g.editors != nil {
		windows = g.editors.windows
	}
	for _, editor := range windows {
		if editor.closed {
			continue
		}
		for i, value := range []string{"", "light", "dark"} {
			if i < len(editor.markdownThemeItems) {
				editor.markdownThemeItems[i].Checked = mode == value
			}
		}
		if editor.markdownThemeItem != nil {
			refreshMenuState(editor.markdownThemeItem.ChildMenu)
		}
		if v := editor.markdownView; v != nil {
			offset := v.scroll.Offset
			if v.pendingReadingOffset != nil {
				offset = *v.pendingReadingOffset
			}
			v.rebuild()
			v.restoreReadingOffset(offset)
		}
	}
}
