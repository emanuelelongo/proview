//go:build darwin && !no_native_menus

package gui

import (
	"emanuelelongo/proview/pkg/document"
	"testing"

	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
)

type menuCountingWindow struct {
	fyne.Window
	menuBuilds int
}

func (w *menuCountingWindow) SetMainMenu(menu *fyne.MainMenu) {
	w.menuBuilds++
	w.Window.SetMainMenu(menu)
}

type menuCountingDriver struct {
	fyne.Driver
	window fyne.Window
}

func (d menuCountingDriver) AllWindows() []fyne.Window { return []fyne.Window{d.window} }

type menuCountingApp struct {
	fyne.App
	driver fyne.Driver
}

func (a menuCountingApp) Driver() fyne.Driver { return a.driver }

func TestNativeMenuStateChangesDoNotRebuildMenu(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	window := &menuCountingWindow{Window: app.NewWindow("Proview")}
	fyne.SetCurrentApp(menuCountingApp{App: app, driver: menuCountingDriver{Driver: app.Driver(), window: window}})
	defer fyne.SetCurrentApp(app)
	g := &Gui{app: app, window: window}
	BuildMenu(g)
	g.document = testDocument(t, 100, 50)
	g.updateHistoryActions()
	if _, err := g.document.AddSignature(0, document.NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, testSignature(t, 2, 1)); err != nil {
		t.Fatal(err)
	}
	g.updateHistoryActions()
	if g.undoItem.Disabled || !g.redoItem.Disabled || g.saveItem.Disabled {
		t.Fatal("menu items did not reflect the document edit")
	}
	g.document.Undo()
	g.updateHistoryActions()
	if !g.undoItem.Disabled || g.redoItem.Disabled {
		t.Fatal("menu items did not reflect undo")
	}
	if window.menuBuilds != 1 {
		t.Fatalf("native menu rebuilt %d times; only initial construction is safe", window.menuBuilds)
	}
}

func TestNativeModelMenuVisibilityDoesNotRebuildMenu(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	window := &menuCountingWindow{Window: app.NewWindow("Proview")}
	g := &Gui{app: app, window: window}
	BuildMenu(g)
	first := g.modelMenu.Items[4]
	if !first.Disabled {
		t.Fatal("STL section enabled without model")
	}
	g.modelViewport = &modelViewport{mesh: testSTLMesh(t)}
	g.updateHistoryActions()
	if first.Disabled {
		t.Fatal("STL section disabled with model")
	}
	g.modelViewport.mesh = nil
	g.updateHistoryActions()
	if !first.Disabled || first != g.modelMenu.Items[4] {
		t.Fatal("native menu callback items changed")
	}
	if window.menuBuilds != 1 {
		t.Fatal("model transition rebuilt native menu")
	}
}
