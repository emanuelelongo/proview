package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
	"testing"
)

func TestModifiedWheelZoomAndNormalScroll(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.showDocument(testDocument(t, 1000, 1000))
	g.zoomIn()
	g.imageScroll.Resize(fyne.NewSize(100, 100))
	g.imageScroll.ScrollToOffset(fyne.NewPos(20, 20))
	surface := newZoomScrollSurface(g, g.imageScroll)
	offset, level := g.imageScroll.Offset, g.zoomLevel
	surface.scrollWithModifiers(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 10}}, fyne.KeyModifierShortcutDefault)
	if g.zoomLevel != level+1 || g.imageScroll.Offset != offset {
		t.Fatal("modified wheel scrolled or failed to zoom")
	}
	surface.scrollWithModifiers(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: -10}}, fyne.KeyModifierShortcutDefault)
	if g.zoomLevel != level || g.imageScroll.Offset != offset {
		t.Fatal("wheel zoom out failed")
	}
	surface.scrollWithModifiers(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: -10}}, 0)
	if g.zoomLevel != level || g.imageScroll.Offset.Y <= offset.Y {
		t.Fatal("normal wheel no longer scrolls")
	}
	g.zoomLevel = maxZoomLevel
	offset = g.imageScroll.Offset
	surface.scrollWithModifiers(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 10}}, fyne.KeyModifierShortcutDefault)
	if g.zoomLevel != maxZoomLevel || g.imageScroll.Offset != offset {
		t.Fatal("zoom limit leaked scrolling")
	}
	// The surface must stay passive for all other pointer events.
	if _, ok := any(surface).(desktop.Hoverable); ok {
		t.Fatal("scroll surface captures hover")
	}
	if _, ok := any(surface).(fyne.Tappable); ok {
		t.Fatal("scroll surface captures clicks")
	}
}
func TestScrollSurfaceRoutingAndVisibility(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := &Gui{app: app}
	content := canvas.NewRectangle(nil)
	content.SetMinSize(fyne.NewSize(300, 500))
	scroll := container.NewScroll(content)
	surface := newZoomScrollSurface(g, scroll)
	window := app.NewWindow("Scroll")
	window.SetContent(container.NewStack(scroll, surface))
	window.Resize(fyne.NewSize(150, 150))
	fyneTest.Scroll(window.Canvas(), fyne.NewPos(30, 30), 0, -20)
	if scroll.Offset.Y == 0 {
		t.Fatal("surface did not forward normal scroll events")
	}
	scroll.Hide()
	if surface.Visible() {
		t.Fatal("hidden viewer intercepts scrolling")
	}
}
func TestZoomKeyboardShortcutsUsePlatformModifier(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.showDocument(testDocument(t, 400, 200))
	for _, shortcut := range zoomInShortcuts() {
		before := g.zoomLevel
		g.window.Canvas().(fyne.Shortcutable).TypedShortcut(shortcut)
		if g.zoomLevel != before+1 {
			t.Fatalf("zoom-in shortcut %v did not work", shortcut)
		}
	}
	before := g.zoomLevel
	g.window.Canvas().(fyne.Shortcutable).TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyMinus, Modifier: fyne.KeyModifierShortcutDefault})
	if g.zoomLevel != before-1 {
		t.Fatal("platform minus shortcut did not zoom out")
	}
	g.window.Canvas().(fyne.Shortcutable).TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.Key0, Modifier: fyne.KeyModifierShortcutDefault})
	if g.zoomLevel != 0 {
		t.Fatal("platform zero shortcut did not reset zoom")
	}
}
