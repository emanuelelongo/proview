//go:build darwin && !no_native_menus

package gui

/*
#cgo darwin CFLAGS: -x objective-c
#cgo darwin LDFLAGS: -framework Foundation -framework AppKit
void proviewObserveModelMenus(void);
*/
import "C"

import "fyne.io/fyne/v2"

// Fyne's native macOS menu validates Disabled and Checked directly from each
// MenuItem when opened. Refresh rebuilds the entire native menu and enters
// resetDarwinMenu, which can crash while removing AppKit menu items. State-only
// updates need no rebuild; retain the items registered by BuildMenu.
func refreshMenuState(_ *fyne.Menu) {}

// Register once and keep all callback IDs stable. AppKit hides the STL section
// when opening View, using the active window's current MenuItem.Disabled state.
func prepareModelMenu(g *Gui) {
	C.proviewObserveModelMenus()
	g.modelMenu.Items = append(g.modelMenu.Items, g.modelMenuItems...)
	g.modelMenu.Items = append(g.modelMenu.Items, g.markdownThemeItem, g.markdownSourceItem)
}
func updateModelMenu(g *Gui) {
	for _, item := range g.modelMenuItems {
		item.Disabled = !g.showingModel()
	}
}
