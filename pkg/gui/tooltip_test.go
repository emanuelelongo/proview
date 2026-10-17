package gui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestTooltipKeepsHoverStableAndFirstCanvasClickReachesButton(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	calls := 0
	button := newIconButton(tablerIcon("folder"), "Open file", func() { calls++ })
	surface := newTooltipSurface(container.NewHBox(button))
	window := app.NewWindow("Tooltip test")
	window.SetContent(surface)
	window.Resize(fyne.NewSize(240, 120))
	window.Show()
	initialMin := surface.MinSize()
	event := &desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(10, 10)}}
	button.MouseIn(event)
	if !surface.bubble.Visible() || surface.owner != button {
		t.Fatal("hover did not show tooltip")
	}
	if window.Canvas().Overlays().Top() != nil {
		t.Fatal("tooltip blocks canvas input with an overlay")
	}
	bubble := surface.bubble
	for range 5 {
		button.MouseMoved(event)
	}
	if surface.bubble != bubble || !bubble.Visible() || surface.MinSize() != initialMin {
		t.Fatal("moving within icon changed tooltip or layout")
	}
	position := app.Driver().AbsolutePositionForObject(button).Add(fyne.NewPos(button.Size().Width/2, button.Size().Height/2))
	fyneTest.TapCanvas(window.Canvas(), position)
	if calls != 1 {
		t.Fatalf("first canvas click activated action %d times", calls)
	}
	if surface.bubble.Visible() {
		t.Fatal("click did not dismiss tooltip")
	}
	button.MouseIn(event)
	button.MouseOut()
	if surface.bubble.Visible() {
		t.Fatal("leaving icon did not dismiss tooltip")
	}
}

func TestLeavingPreviousIconDoesNotHideNextTooltip(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	first := newIconButton(tablerIcon("folder"), "Open", nil)
	second := newIconButton(tablerIcon("crop"), "Crop", nil)
	surface := newTooltipSurface(container.NewHBox(first, second))
	window := app.NewWindow("Tooltip test")
	window.SetContent(surface)
	window.Resize(fyne.NewSize(200, 100))
	window.Show()
	event := &desktop.MouseEvent{}
	first.MouseIn(event)
	second.MouseIn(event)
	first.MouseOut()
	if surface.owner != second || surface.label.Text != "Crop" || !surface.bubble.Visible() {
		t.Fatal("old hover dismissed the current tooltip")
	}
	second.MouseOut()
	if surface.bubble.Visible() {
		t.Fatal("tooltip survived leaving its owner")
	}
}

func TestPDFTextDialogTooltipsStayInsideModalAndKeepButtonsClickable(t *testing.T) {
	g, _ := textMoveTestGui(t)
	overlays := len(g.window.Canvas().Overlays().List())
	var buttons []*iconButton
	for _, object := range fyneTest.LaidOutObjects(g.window.Canvas().Overlays().Top()) {
		if button, ok := object.(*iconButton); ok && (button.tooltip == "Move" || button.tooltip == "Duplicate") {
			buttons = append(buttons, button)
		}
	}
	if len(buttons) != 2 {
		t.Fatalf("dialog buttons=%d", len(buttons))
	}
	for _, button := range buttons {
		button.MouseIn(&desktop.MouseEvent{})
		surface := button.tooltipHost
		if surface == nil || !surface.bubble.Visible() || surface.label.Text != button.tooltip {
			t.Fatal("dialog hover did not show tooltip")
		}
		if len(g.window.Canvas().Overlays().List()) != overlays {
			t.Fatal("tooltip added an input-blocking overlay")
		}
		button.MouseOut()
		if surface.bubble.Visible() {
			t.Fatal("tooltip survived mouse exit")
		}
	}
	button := buttons[0]
	button.MouseIn(&desktop.MouseEvent{})
	fyneTest.Tap(button)
	if g.correctionDialog != nil || g.textMove == nil || button.tooltipHost.bubble.Visible() {
		t.Fatal("tooltip blocked the action or survived dialog close")
	}
}
