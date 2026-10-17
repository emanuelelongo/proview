package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// Icon buttons retain a readable name on hover without widening the toolbar.
type iconButton struct {
	widget.Button
	tooltip        string
	tooltipSurface *tooltipSurface
	tooltipHost    *tooltipSurface
}

func newIconButton(icon fyne.Resource, label string, tapped func()) *iconButton {
	button := &iconButton{Button: widget.Button{Icon: icon, OnTapped: tapped}, tooltip: label}
	button.ExtendBaseWidget(button)
	return button
}
func (b *iconButton) MouseIn(event *desktop.MouseEvent) {
	b.Button.MouseIn(event)
	if b.tooltip == "" || b.Disabled() {
		return
	}
	c := fyne.CurrentApp().Driver().CanvasForObject(b)
	if c == nil {
		return
	}
	surface := b.tooltipHost
	if surface == nil {
		if c.Overlays().Top() != nil {
			return
		}
		var ok bool
		surface, ok = c.Content().(*tooltipSurface)
		if !ok {
			return
		}
	}
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(b)
	rootPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(surface)
	b.tooltipSurface = surface
	surface.show(b, b.tooltip, fyne.NewPos(pos.X-rootPos.X, pos.Y-rootPos.Y+b.Size().Height+4))
}
func (b *iconButton) MouseOut()                     { b.Button.MouseOut(); b.hideTooltip() }
func (b *iconButton) Tapped(event *fyne.PointEvent) { b.hideTooltip(); b.Button.Tapped(event) }
func (b *iconButton) hideTooltip() {
	if b.tooltipSurface != nil {
		b.tooltipSurface.hide(b)
		b.tooltipSurface = nil
	}
}

type iconToolbarAction struct{ button *iconButton }

func newIconToolbarAction(icon fyne.Resource, label string, tapped func()) *iconToolbarAction {
	button := newIconButton(icon, label, tapped)
	button.Importance = widget.LowImportance
	return &iconToolbarAction{button}
}
func (a *iconToolbarAction) ToolbarObject() fyne.CanvasObject { return a.button }
func (a *iconToolbarAction) Disable()                         { a.button.Disable() }
func (a *iconToolbarAction) Enable()                          { a.button.Enable() }
