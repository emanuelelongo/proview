package gui

import (
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver"
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
func (s *zoomScrollSurface) Resize(size fyne.Size) {
	s.BaseWidget.Resize(size)
	g := s.editor
	if g.openingFitDocument == nil || g.openingFitDocument != g.document || size.Width <= 0 || size.Height <= 0 || s.scroll.Size() != size {
		return
	}
	active := g.imageScroll
	if g.document.Format() == document.FormatPDF {
		active = g.pdfScroll
	}
	if s.scroll != active {
		return
	}
	// A newly created native window has no viewport until Show/layout. Fit
	// once to that real viewport, before subsequent user zooms take ownership.
	finish := func() {
		if g.closed || g.openingFitDocument == nil || g.openingFitDocument != g.document {
			return
		}
		g.openingFitDocument = nil
		g.fitNewDocument()
		g.captureDocument()
	}
	if _, native := g.window.(driver.NativeWindow); native {
		// Native window creation can lay out several intermediate sizes. Fit
		// after that batch settles, using its final viewport rather than the
		// first nonzero one. Read the latest document if openings coalesce.
		if !g.openingFitQueued {
			g.openingFitQueued = true
			fyne.Do(func() { g.openingFitQueued = false; finish() })
		}
	} else {
		finish()
	}
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
		// Keep the document point under the pointer fixed while zooming.
		// Consume the gesture at zoom limits as well.
		if event.Scrolled.DY > 0 {
			s.editor.zoomBy(1, &event.Position)
		} else if event.Scrolled.DY < 0 {
			s.editor.zoomBy(-1, &event.Position)
		}
		return
	}
	s.scroll.Scrolled(event)
}
