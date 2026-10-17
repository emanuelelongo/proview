package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// thumbnailTap receives clicks without painting over the page preview on hover.
type thumbnailTap struct {
	widget.BaseWidget
	onTap       func()
	onSelect    func(fyne.KeyModifier)
	modifiers   fyne.KeyModifier
	onDrag      func(*fyne.DragEvent)
	onDragEnd   func()
	onSecondary func(*fyne.PointEvent)
	onMouseDown func(*desktop.MouseEvent)
	onMouseUp   func(*desktop.MouseEvent)
	onCursor    func() desktop.Cursor
}

func newThumbnailTap(onTap func()) *thumbnailTap {
	tap := &thumbnailTap{onTap: onTap}
	tap.ExtendBaseWidget(tap)
	return tap
}

func (t *thumbnailTap) Tapped(*fyne.PointEvent) {
	if t.onSelect != nil {
		t.onSelect(t.modifiers)
	} else if t.onTap != nil {
		t.onTap()
	}
	t.modifiers = 0
}

func (t *thumbnailTap) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

func (t *thumbnailTap) Dragged(event *fyne.DragEvent) {
	if t.onDrag != nil {
		t.onDrag(event)
	}
}
func (t *thumbnailTap) DragEnd() {
	t.modifiers = 0
	if t.onDragEnd != nil {
		t.onDragEnd()
	}
}
func (t *thumbnailTap) TappedSecondary(event *fyne.PointEvent) {
	if t.onSecondary != nil {
		t.onSecondary(event)
	}
}
func (t *thumbnailTap) MouseDown(event *desktop.MouseEvent) {
	if event.Button == desktop.MouseButtonPrimary {
		t.modifiers = event.Modifier
	}
	if t.onMouseDown != nil {
		t.onMouseDown(event)
	}
}
func (t *thumbnailTap) MouseUp(event *desktop.MouseEvent) {
	if t.onMouseUp != nil {
		t.onMouseUp(event)
	}
}
func (t *thumbnailTap) Cursor() desktop.Cursor {
	if t.onCursor != nil {
		return t.onCursor()
	}
	return desktop.PointerCursor
}
