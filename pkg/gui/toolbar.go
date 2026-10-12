package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

type toggleToolbarAction struct {
	button *iconButton
}

func newToggleToolbarAction(icon fyne.Resource, action func()) *toggleToolbarAction {
	button := newIconButton(icon, "", action)
	button.Importance = widget.LowImportance
	return &toggleToolbarAction{button: button}
}

func (a *toggleToolbarAction) ToolbarObject() fyne.CanvasObject {
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
