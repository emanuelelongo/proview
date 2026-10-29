//go:build !darwin || no_native_menus

package gui

import "fyne.io/fyne/v2"

func refreshMenuState(menu *fyne.Menu) {
	menu.Refresh()
}

func prepareModelMenu(g *Gui) {
	g.modelMenu.Items = append(g.modelMenu.Items, g.markdownThemeItem, g.markdownSourceItem)
}
func updateModelMenu(g *Gui) {
	if g.modelMenu == nil {
		return
	}
	g.modelMenu.Items = g.modelMenu.Items[:3]
	if g.showingModel() {
		g.modelMenu.Items = append(g.modelMenu.Items, g.modelMenuItems...)
	}
	g.modelMenu.Items = append(g.modelMenu.Items, g.markdownThemeItem, g.markdownSourceItem)
}
