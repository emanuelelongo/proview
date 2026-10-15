package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"image/color"
)

type toggleToolbarAction struct {
	button          *iconButton
	pulseBackground *canvas.Rectangle
	pulseContent    *fyne.Container
}

func newToggleToolbarAction(icon fyne.Resource, action func()) *toggleToolbarAction {
	button := newIconButton(icon, "", action)
	button.Importance = widget.LowImportance
	return &toggleToolbarAction{button: button}
}

func (a *toggleToolbarAction) ToolbarObject() fyne.CanvasObject {
	if a.pulseContent != nil {
		return a.pulseContent
	}
	return a.button
}

func (a *toggleToolbarAction) SetActive(active bool) {
	if active {
		a.button.Importance = widget.HighImportance
	} else {
		a.button.Importance = widget.LowImportance
	}
	a.button.Refresh()
}

func (a *toggleToolbarAction) Disable() { a.button.Disable() }
func (a *toggleToolbarAction) Enable()  { a.button.Enable() }

func (a *toggleToolbarAction) preparePulse() {
	a.pulseBackground = canvas.NewRectangle(color.Transparent)
	a.pulseBackground.CornerRadius = 4
	a.pulseContent = container.NewStack(a.pulseBackground, a.button)
}
