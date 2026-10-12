package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"image/color"
)

// This passive surface handles only scrolling. It does not capture hover,
// clicks or drags, and covers the viewport's empty margins as well as pages.
type zoomScrollSurface struct {
	widget.BaseWidget
	editor *Gui
	scroll *container.Scroll
}

func newZoomScrollSurface(g *Gui, scroll *container.Scroll) *zoomScrollSurface {
	s := &zoomScrollSurface{editor: g, scroll: scroll}
	s.ExtendBaseWidget(s)
	return s
}
func (s *zoomScrollSurface) Visible() bool { return s.BaseWidget.Visible() && s.scroll.Visible() }
func (s *zoomScrollSurface) CreateRenderer() fyne.WidgetRenderer {
	return &panOverlayRenderer{background: canvas.NewRectangle(color.Transparent)}
}
func (s *zoomScrollSurface) Scrolled(event *fyne.ScrollEvent) {
	var modifiers fyne.KeyModifier
	if driver, ok := s.editor.app.Driver().(desktop.Driver); ok {
		modifiers = driver.CurrentKeyModifiers()
	}
	s.scrollWithModifiers(event, modifiers)
}
func (s *zoomScrollSurface) scrollWithModifiers(event *fyne.ScrollEvent, modifiers fyne.KeyModifier) {
	if modifiers&fyne.KeyModifierShortcutDefault != 0 {
		// Consume modified scrolling even at the zoom limits. Never scroll the page
		// sideways or vertically as a side effect of a zoom gesture.
		if event.Scrolled.DY > 0 {
			s.editor.zoomIn()
		} else if event.Scrolled.DY < 0 {
			s.editor.zoomOut()
		}
		return
	}
	s.scroll.Scrolled(event)
}
