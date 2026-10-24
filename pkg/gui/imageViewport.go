package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// Keep the image and editing layers at their requested zoom even when the scroll
// content fills a larger viewport. Contain mode alone would stretch them back to
// fit. Space-drag panning still covers the viewport's empty margins.
type imageViewportLayout struct {
	image *canvas.Image
	pan   fyne.CanvasObject
}

func (l *imageViewportLayout) MinSize([]fyne.CanvasObject) fyne.Size { return l.image.MinSize() }

func (l *imageViewportLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	fitted := l.image.MinSize()
	position := fyne.NewPos((size.Width-fitted.Width)/2, (size.Height-fitted.Height)/2)
	for _, object := range objects {
		if object == l.pan {
			object.Move(fyne.Position{})
			object.Resize(size)
		} else {
			object.Move(position)
			object.Resize(fitted)
		}
	}
}
