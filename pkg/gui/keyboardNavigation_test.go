package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"slices"
	"testing"
)

type navigationTestDriver struct {
	fyne.Driver
	modifiers fyne.KeyModifier
}

func (d *navigationTestDriver) CurrentKeyModifiers() fyne.KeyModifier { return d.modifiers }
func (d *navigationTestDriver) CreateSplashWindow() fyne.Window       { return d.CreateWindow("Splash") }

type navigationTestApp struct {
	fyne.App
	driver *navigationTestDriver
}

func (a *navigationTestApp) Driver() fyne.Driver { return a.driver }

func TestArrowKeysPanViewerAndClampWithoutEditing(t *testing.T) {
	base := fyneTest.NewApp()
	defer base.Quit()
	app := &navigationTestApp{App: base, driver: &navigationTestDriver{Driver: base.Driver()}}
	content := canvas.NewRectangle(nil)
	content.SetMinSize(fyne.NewSize(500, 400))
	scroll := container.NewScroll(content)
	scroll.Resize(fyne.NewSize(100, 100))
	g := &Gui{app: app, window: app.NewWindow("Navigation"), document: testDocument(t, 500, 400), imageScroll: scroll}
	g.installKeyHandler()
	scroll.ScrollToOffset(fyne.NewPos(100, 100))
	for _, key := range []fyne.KeyName{fyne.KeyRight, fyne.KeyDown} {
		g.window.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: key})
	}
	if scroll.Offset != (fyne.Position{X: 140, Y: 140}) {
		t.Fatal("arrow keys did not pan the main viewport", scroll.Offset)
	}
	g.handleDocumentNavigationKey(fyne.KeyLeft)
	g.handleDocumentNavigationKey(fyne.KeyUp)
	if scroll.Offset != (fyne.Position{X: 100, Y: 100}) {
		t.Fatal("reverse pan failed")
	}
	for range 20 {
		g.handleDocumentNavigationKey(fyne.KeyLeft)
		g.handleDocumentNavigationKey(fyne.KeyUp)
	}
	if scroll.Offset != (fyne.Position{}) {
		t.Fatal("pan went beyond the top/left bounds")
	}
	for _, modifier := range []fyne.KeyModifier{fyne.KeyModifierShift, fyne.KeyModifierControl, fyne.KeyModifierSuper, fyne.KeyModifierAlt} {
		app.driver.modifiers = modifier
		if g.handleDocumentNavigationKey(fyne.KeyDown) || scroll.Offset != (fyne.Position{}) {
			t.Fatal("navigation intercepted a modifier combination")
		}
	}
	app.driver.modifiers = 0
	if g.handleDocumentNavigationKey(fyne.KeyPageDown) || g.handleDocumentNavigationKey(fyne.KeySpace) || g.document.Dirty() || g.document.CanUndo() {
		t.Fatal("navigation changed image history or intercepted another key")
	}
}

func TestNavigationYieldsToControlsDialogsAndEditingGestures(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.showDocument(testDocument(t, 100, 50))
	for _, control := range []fyne.Focusable{widget.NewEntry(), widget.NewButton("Button", nil), widget.NewSelect([]string{"One", "Two"}, nil)} {
		g.window.SetContent(control.(fyne.CanvasObject))
		g.window.Canvas().Focus(control)
		if g.handleDocumentNavigationKey(fyne.KeyDown) || g.handleDocumentNavigationKey(fyne.KeyPageDown) {
			t.Fatal("navigation stole focused control keys")
		}
		g.window.Canvas().Focus(nil)
	}
	prompt := dialog.NewInformation("Test", "Dialog", g.window)
	prompt.Show()
	if g.handleDocumentNavigationKey(fyne.KeyDown) {
		t.Fatal("navigation stole dialog keys")
	}
	prompt.Hide()
	layer := g.imageSignatureLayer
	blockages := []struct{ begin, end func() }{
		{func() { g.working = true }, func() { g.working = false }},
		{func() { g.textPlacement = &textPlacement{} }, func() { g.textPlacement = nil }},
		{func() { g.thumbnailDrag = &thumbnailDragState{} }, func() { g.thumbnailDrag = nil }},
		{func() { layer.input.active = true }, func() { layer.input.active = false }},
		{func() { layer.textGesture = true }, func() { layer.textGesture = false }},
	}
	for _, block := range blockages {
		block.begin()
		if g.handleDocumentNavigationKey(fyne.KeyRight) {
			t.Fatal("navigation interrupted editing")
		}
		block.end()
	}
	g.requestCloseDocument()
}

func TestPageKeysNavigatePDFAndKeepMultipleSelection(t *testing.T) {
	g, _ := newDropTestGui(t)
	doc := workspacePDF(t, 4)
	g.addDocument(doc, nil, nil)
	t.Cleanup(func() {
		if !g.closed {
			g.closeWindow()
		}
	})
	ids := doc.PageIDs()
	g.setThumbnailSelection([]int{0, 2})
	g.window.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyPageDown})
	if g.activePDFPage != 1 || !slices.Equal(g.selectedPDFPages(), []int{0, 2}) {
		t.Fatal("Page Down did not navigate or changed multiple selection")
	}
	for range 8 {
		g.handleDocumentNavigationKey(fyne.KeyPageDown)
	}
	if g.activePDFPage != 3 {
		t.Fatal("Page Down wrapped or did not reach the last page", g.activePDFPage)
	}
	g.handleDocumentNavigationKey(fyne.KeyPageUp)
	if g.activePDFPage != 2 {
		t.Fatal("Page Up did not select the preceding page")
	}
	for range 8 {
		g.handleDocumentNavigationKey(fyne.KeyPageUp)
	}
	if g.activePDFPage != 0 || doc.Dirty() || doc.CanUndo() || !slices.Equal(doc.PageIDs(), ids) {
		t.Fatal("page navigation changed document history or crossed the first page")
	}
}
