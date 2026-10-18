package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
	"math"
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
	point := fyne.NewPos(30, 40)
	origin, size := g.zoomPageRect(g.imageScroll, 0, false)
	relative := fyne.NewPos((offset.X+point.X-origin.X)/size.Width, (offset.Y+point.Y-origin.Y)/size.Height)
	surface.scrollWithModifiers(&fyne.ScrollEvent{PointEvent: fyne.PointEvent{Position: point}, Scrolled: fyne.Delta{DY: 10}}, fyne.KeyModifierShortcutDefault)
	origin, size = g.zoomPageRect(g.imageScroll, 0, false)
	actual := fyne.NewPos(origin.X+relative.X*size.Width-g.imageScroll.Offset.X, origin.Y+relative.Y*size.Height-g.imageScroll.Offset.Y)
	if g.zoomLevel != level+1 || math.Abs(float64(actual.X-point.X)) > .01 || math.Abs(float64(actual.Y-point.Y)) > .01 {
		t.Fatal("modified wheel did not preserve the pointer's document point", actual, point)
	}
	surface.scrollWithModifiers(&fyne.ScrollEvent{PointEvent: fyne.PointEvent{Position: point}, Scrolled: fyne.Delta{DY: -10}}, fyne.KeyModifierShortcutDefault)
	if g.zoomLevel != level || math.Abs(float64(g.imageScroll.Offset.X-offset.X)) > .01 || math.Abs(float64(g.imageScroll.Offset.Y-offset.Y)) > .01 {
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

func TestPDFZoomKeepsLaterPageAndLocalPoint(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.window.Resize(fyne.NewSize(900, 650))
	g.showDocument(workspacePDF(t, 8))
	g.zoomIn()
	g.zoomIn()
	scroll := g.pdfScroll
	page := 5
	origin, size := g.zoomPageRect(scroll, page, true)
	point := fyne.NewPos(scroll.Size().Width*.45, scroll.Size().Height*.4)
	scroll.ScrollToOffset(fyne.NewPos(max(0, origin.X+size.Width*.5-point.X), origin.Y+size.Height*.5-point.Y))
	origin, size = g.zoomPageRect(scroll, page, true)
	relative := fyne.NewPos((scroll.Offset.X+point.X-origin.X)/size.Width, (scroll.Offset.Y+point.Y-origin.Y)/size.Height)
	for range 3 {
		g.zoomBy(1, &point)
		origin, size = g.zoomPageRect(scroll, page, true)
		actual := fyne.NewPos(origin.X+relative.X*size.Width-scroll.Offset.X, origin.Y+relative.Y*size.Height-scroll.Offset.Y)
		if math.Abs(float64(actual.X-point.X)) > .05 || math.Abs(float64(actual.Y-point.Y)) > .05 {
			t.Fatal("zoom drifted away from later page", actual, point)
		}
	}
	center := fyne.NewPos(scroll.Size().Width/2, scroll.Size().Height/2)
	origin, size = g.zoomPageRect(scroll, page, true)
	relative = fyne.NewPos((scroll.Offset.X+center.X-origin.X)/size.Width, (scroll.Offset.Y+center.Y-origin.Y)/size.Height)
	g.zoomIn()
	origin, size = g.zoomPageRect(scroll, page, true)
	actual := fyne.NewPos(origin.X+relative.X*size.Width-scroll.Offset.X, origin.Y+relative.Y*size.Height-scroll.Offset.Y)
	if math.Abs(float64(actual.X-center.X)) > .05 || math.Abs(float64(actual.Y-center.Y)) > .05 {
		t.Fatal("toolbar zoom lost viewport center", actual, center)
	}
}
