package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

type panOverlay struct {
	widget.BaseWidget
	scroll *container.Scroll
}

func newPanOverlay(scroll *container.Scroll) *panOverlay {
	overlay := &panOverlay{scroll: scroll}
	overlay.ExtendBaseWidget(overlay)
	overlay.Hide()
	return overlay
}

func (p *panOverlay) SetScroll(scroll *container.Scroll) {
	p.scroll = scroll
}

func (p *panOverlay) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(color.Transparent)
	return &panOverlayRenderer{background: background}
}

func (p *panOverlay) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (p *panOverlay) Dragged(event *fyne.DragEvent) {
	if p.scroll == nil {
		return
	}
	p.scroll.ScrollToOffset(fyne.NewPos(
		p.scroll.Offset.X-float32(event.Dragged.DX),
		p.scroll.Offset.Y-float32(event.Dragged.DY),
	))
}

func (p *panOverlay) DragEnd() {}

type panOverlayRenderer struct {
	background *canvas.Rectangle
}

func (r *panOverlayRenderer) Destroy() {}

func (r *panOverlayRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
}

func (r *panOverlayRenderer) MinSize() fyne.Size {
	return fyne.NewSize(0, 0)
}

func (r *panOverlayRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.background}
}

func (r *panOverlayRenderer) Refresh() {
	r.background.Refresh()
}

func (g *Gui) setPanActive(active bool) {
	active = active && g.document != nil && g.zoomLevel > 0 && !g.closed && !g.working
	if active && g.window != nil {
		// Focused controls and dialogs own Space; keep text entry and buttons usable.
		active = g.window.Canvas().Focused() == nil && g.window.Canvas().Overlays().Top() == nil
	}
	for _, overlay := range g.panOverlays {
		if active {
			overlay.Show()
		} else {
			overlay.Hide()
		}
	}
}

func (g *Gui) handlePanKey(key fyne.KeyName, down bool) {
	if key != fyne.KeySpace {
		return
	}
	g.panModifierDown = down
	g.setPanActive(down)
}
