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
