package gui

import (
	"emanuelelongo/proview/pkg/annotations"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

func (g *Gui) shapeSelector(selected annotations.ShapeKind, changed func(annotations.ShapeKind)) fyne.CanvasObject {
	var button *widget.Button
	var selector *fyne.Container
	button = widget.NewButtonWithIcon("", tablerIcon(shapeIconName(selected)), func() {
		var items []*fyne.MenuItem
		for _, name := range annotations.Shapes {
			kind := annotations.ShapeKind(name)
			item := fyne.NewMenuItem("", func() { button.SetIcon(tablerIcon(shapeIconName(kind))); changed(kind) })
			item.Icon = tablerIcon(shapeIconName(kind))
			items = append(items, item)
		}
		p := fyne.CurrentApp().Driver().AbsolutePositionForObject(selector)
		p.Y += selector.Size().Height
		widget.NewPopUpMenu(fyne.NewMenu("Shapes", items...), g.window.Canvas()).ShowAtPosition(p)
	})
	selector = compactDropdownContent(button)
	return selector
}

func shapeIconName(kind annotations.ShapeKind) string {
	switch kind {
	case annotations.Oval:
		return "oval"
	case annotations.Callout:
		return "message-circle"
	case annotations.Line:
		return "stroke-straight"
	case annotations.Arrow:
		return "arrow-narrow-right"
	case annotations.Star:
		return "star"
	case annotations.Magnifier:
		return "device-tablet-search"
	default:
		return "rectangle"
	}
}

type shapeSelectorLayout struct{}

func (shapeSelectorLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
	objects[1].Move(fyne.NewPos(size.Width-12, size.Height-12))
	objects[1].Resize(fyne.NewSize(10, 10))
}

func (shapeSelectorLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(36, 36) }
