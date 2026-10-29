package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Tooltips are part of the normal canvas tree. Fyne searches only the top
// overlay for input, so even a nonmodal PopUp would block the underlying tools.
// This surface and its tooltip implement no mouse or focus interfaces.
type tooltipSurface struct {
	widget.BaseWidget
	content    fyne.CanvasObject
	bubble     *fyne.Container
	label      *widget.Label
	background *canvas.Rectangle
	owner      fyne.CanvasObject
	position   fyne.Position
}

func newTooltipSurface(content fyne.CanvasObject) *tooltipSurface {
	s := &tooltipSurface{content: content, label: widget.NewLabel("")}
	s.background = canvas.NewRectangle(theme.OverlayBackgroundColor())
	s.background.StrokeColor = theme.ForegroundColor()
	s.background.StrokeWidth = 1
	s.bubble = container.NewStack(s.background, s.label)
	s.bubble.Hide()
	s.ExtendBaseWidget(s)
	return s
}
func (s *tooltipSurface) show(owner fyne.CanvasObject, text string, pos fyne.Position) {
	s.owner = owner
	s.position = pos
	s.label.SetText(text)
	s.bubble.Resize(s.bubble.MinSize())
	s.positionBubble(s.Size())
	s.bubble.Show()
}
func (s *tooltipSurface) hide(owner fyne.CanvasObject) {
	if s.owner != owner {
		return
	}
	s.owner = nil
	s.bubble.Hide()
}
func (s *tooltipSurface) positionBubble(size fyne.Size) {
	pos := fyne.NewPos(max(0, min(s.position.X, size.Width-s.bubble.Size().Width)), max(0, min(s.position.Y, size.Height-s.bubble.Size().Height)))
	s.bubble.Move(pos)
}
func (s *tooltipSurface) CreateRenderer() fyne.WidgetRenderer { return &tooltipRenderer{surface: s} }

type tooltipRenderer struct{ surface *tooltipSurface }

func (r *tooltipRenderer) Layout(size fyne.Size) {
	r.surface.content.Move(fyne.Position{})
	r.surface.content.Resize(size)
	r.surface.positionBubble(size)
}
func (r *tooltipRenderer) MinSize() fyne.Size { return r.surface.content.MinSize() }
func (r *tooltipRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.surface.content, r.surface.bubble}
}
func (r *tooltipRenderer) Destroy() {}
func (r *tooltipRenderer) Refresh() {
	s := r.surface
	s.content.Refresh()
	s.background.FillColor = theme.OverlayBackgroundColor()
	s.background.StrokeColor = theme.ForegroundColor()
	s.background.Refresh()
	s.label.Refresh()
	r.Layout(s.Size())
	canvas.Refresh(s)
}
