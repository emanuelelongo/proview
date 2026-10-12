package gui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestPanOverlayMovesScrollOffset(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()

	content := canvas.NewRectangle(nil)
	content.SetMinSize(fyne.NewSize(500, 400))
	scroll := container.NewScroll(content)
	scroll.Resize(fyne.NewSize(100, 100))
	scroll.ScrollToOffset(fyne.NewPos(100, 100))
	overlay := newPanOverlay(scroll)

	overlay.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DX: 20, DY: -10}})
	if scroll.Offset != (fyne.Position{X: 80, Y: 110}) {
		t.Fatalf("pan offset = %v, want (80, 110)", scroll.Offset)
	}
}

func TestPanOnlyActivatesAboveFitZoom(t *testing.T) {
	overlay := newPanOverlay(nil)
	g := &Gui{document: testDocument(t, 100, 50), panOverlays: []*panOverlay{overlay}}

	g.setPanActive(true)
	if overlay.Visible() {
		t.Fatal("pan is active at fit zoom")
	}
	g.zoomLevel = 1
	g.setPanActive(true)
	if !overlay.Visible() {
		t.Fatal("pan is inactive above fit zoom")
	}
	g.setPanActive(false)
	if overlay.Visible() {
		t.Fatal("pan stayed active after Space was released")
	}
}

func TestZoomInShortcutsCoverMainAndKeypadPlus(t *testing.T) {
	want := map[fyne.KeyName]bool{
		fyne.KeyEqual:        false,
		fyne.KeyPlus:         false,
		fyne.KeyRightBracket: false,
	}
	for _, shortcut := range zoomInShortcuts() {
		if shortcut.Modifier&fyne.KeyModifierShortcutDefault != 0 {
			want[shortcut.KeyName] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Fatalf("missing zoom-in shortcut for key %q", key)
		}
	}
}

func TestSpacePanPreservesTextAndDialogInput(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.showDocument(testDocument(t, 100, 50))
	g.zoomIn()
	g.handlePanKey(desktop.KeyControlLeft, true)
	if g.imagePanOverlay.Visible() {
		t.Fatal("Control activated pan")
	}
	g.handlePanKey(fyne.KeySpace, true)
	if !g.imagePanOverlay.Visible() {
		t.Fatal("Space did not activate pan")
	}
	g.handlePanKey(fyne.KeySpace, false)
	if g.imagePanOverlay.Visible() {
		t.Fatal("Space release did not stop pan")
	}
	entry := widget.NewEntry()
	content := g.window.Content()
	g.window.SetContent(entry)
	g.window.Canvas().Focus(entry)
	g.handlePanKey(fyne.KeySpace, true)
	if g.imagePanOverlay.Visible() {
		t.Fatal("pan intercepted text input")
	}
	g.handlePanKey(fyne.KeySpace, false)
	g.window.Canvas().Focus(nil)
	g.window.SetContent(content)
	prompt := dialog.NewInformation("Test", "Dialog", g.window)
	prompt.Show()
	g.handlePanKey(fyne.KeySpace, true)
	if g.imagePanOverlay.Visible() {
		t.Fatal("pan intercepted dialog input")
	}
	g.handlePanKey(fyne.KeySpace, false)
	prompt.Hide()
	g.requestCloseDocument()
}
