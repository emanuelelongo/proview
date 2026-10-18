package gui

import (
	"emanuelelongo/proview/pkg/annotations"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func (g *Gui) shapeSelector(selected annotations.ShapeKind, changed func(annotations.ShapeKind)) fyne.CanvasObject {
	icons := map[annotations.ShapeKind]string{annotations.Rectangle: "rectangle", annotations.Oval: "oval", annotations.Callout: "message-circle", annotations.Line: "stroke-straight", annotations.Arrow: "arrow-narrow-right", annotations.Star: "star", annotations.Magnifier: "zoom-in"}
	if _, ok := icons[selected]; !ok {
		icons[selected] = "rectangle"
	}
	var button *widget.Button
	var selector *fyne.Container
	button = widget.NewButtonWithIcon("", tablerIcon(icons[selected]), func() {
		var items []*fyne.MenuItem
		for _, name := range annotations.Shapes {
			kind := annotations.ShapeKind(name)
			item := fyne.NewMenuItem("", func() { button.SetIcon(tablerIcon(icons[kind])); changed(kind) })
			item.Icon = tablerIcon(icons[kind])
			items = append(items, item)
		}
		p := fyne.CurrentApp().Driver().AbsolutePositionForObject(selector)
		p.Y += selector.Size().Height
		widget.NewPopUpMenu(fyne.NewMenu("Shapes", items...), g.window.Canvas()).ShowAtPosition(p)
	})
	arrow := canvas.NewImageFromResource(tablerIcon("chevron-down"))
	arrow.FillMode = canvas.ImageFillContain
	selector = container.New(shapeSelectorLayout{}, button, arrow)
	return selector
}

type shapeSelectorLayout struct{}

func (shapeSelectorLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
	objects[1].Move(fyne.NewPos(size.Width-12, size.Height-12))
	objects[1].Resize(fyne.NewSize(10, 10))
}

func (shapeSelectorLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(44, 36) }
