package gui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestCompactSelectPopupDoesNotWidenPropertyRail(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	window := app.NewWindow("Compact properties")
	selected := ""
	options := []string{"Go Sans", "A very long font name"}
	selector := newCompactSelect(options, "text-outline", "Font", func(value string) { selected = value })
	selector.SetSelected(options[0])
	rail := container.NewVBox(selector)
	window.SetContent(container.NewBorder(nil, nil, nil, rail, widget.NewLabel("Document")))
	window.Resize(fyne.NewSize(800, 600))
	window.Show()
	initialSize := rail.Size()
	if initialSize.Width != 36 {
		t.Fatalf("rail width = %v, want 36", initialSize.Width)
	}
	fyneTest.Tap(selector.button)
	var popup *widget.PopUpMenu
	for _, object := range fyneTest.LaidOutObjects(window.Canvas().Overlays().Top()) {
		if menu, ok := object.(*widget.PopUpMenu); ok {
			popup = menu
			break
		}
	}
	if popup == nil {
		t.Fatal("compact selector did not open an overlay menu")
	}
	if popup.MinSize().Width <= rail.Size().Width || rail.Size() != initialSize {
		t.Fatal("menu should overflow without changing the rail size")
	}
	fyneTest.Tap(popup.Items[1].(fyne.Tappable))
	if selected != options[1] || selector.Selected != options[1] || rail.Size() != initialSize {
		t.Fatal("choosing a long value lost the selection or widened the rail")
	}
	if selector.button.tooltip != "Font: "+options[1] {
		t.Fatal("tooltip does not show the selected value")
	}
	selector.Disable()
	fyneTest.Tap(selector.button)
	if window.Canvas().Overlays().Top() != nil {
		t.Fatal("disabled selector opened its menu")
	}
	selector.Enable()
	selector.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if window.Canvas().Overlays().Top() == nil {
		t.Fatal("keyboard could not open the compact selector")
	}
}
