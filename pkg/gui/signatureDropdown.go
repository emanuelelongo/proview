package gui

import (
	"emanuelelongo/proview/pkg/signatures"
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (g *Gui) showSignatureDropdown() {
	if g.signaturePopup != nil {
		visible := g.signaturePopup.Visible()
		g.signaturePopup.Hide()
		g.signaturePopup = nil
		if visible {
			return
		}
	}
	store, ok := g.signatureArchive()
	if !ok {
		return
	}
	content := container.NewVBox(widget.NewLabel("Loading signatures"))
	scroll := container.NewVScroll(content)
	popup := widget.NewPopUp(scroll, g.window.Canvas())
	g.signaturePopup = popup
	position := fyne.NewPos(8, 48)
	if g.signatureAction != nil {
		button := g.signatureAction.ToolbarObject()
		position = g.app.Driver().AbsolutePositionForObject(button)
		position.Y += button.Size().Height
	}
	viewport := g.window.Canvas().Size()
	width := min(float32(300), max(180, viewport.Width-24))
	position.X = max(8, min(position.X, viewport.Width-width-12))
	height := max(80, min(float32(420), viewport.Height-position.Y-16))
	scroll.SetMinSize(fyne.NewSize(width, 80))
	popup.Resize(fyne.NewSize(width, 80))
	popup.ShowAtPosition(position)
	doc := g.document
	if g.signatureCloud != nil {
		g.signatureCloud.open()
	}
	go func() {
		entries, err := store.List()
		previews := renderSignaturePreviews(entries, 300, maxSignaturePreviewPixels)
		fyne.Do(func() {
			if g.closed || !popup.Visible() || g.signaturePopup != popup || g.document != doc {
				return
			}
			content.Objects = nil
			if err != nil {
				content.Add(widget.NewLabel("Some signatures could not be read."))
			}
			if len(entries) == 0 {
				content.Add(widget.NewLabel("No saved signatures"))
			}
			for i, entry := range entries {
				entry := entry
				preview := canvas.NewImageFromImage(previews[i])
				preview.FillMode = canvas.ImageFillContain
				preview.SetMinSize(fyne.NewSize(width-16, 60))
				choice := newSignatureChoice(container.NewStack(canvas.NewRectangle(color.White), preview), func() {
					popup.Hide()
					g.signaturePopup = nil
					if g.document == doc {
						g.insertSignatureAsset(entry.Asset)
					}
				})
				if doc == nil {
					choice.tapped = nil
				}
				content.Add(choice)
			}
			content.Add(widget.NewSeparator())
			content.Add(newSignatureChoice(widget.NewLabel("Manage signatures…"), func() {
				popup.Hide()
				g.signaturePopup = nil
				g.showSignatureLibrary()
			}))
			content.Refresh()
			// PopUp dimensions include its frame padding; the scroll viewport
			// needs the full content height to display the last row completely.
			padding := popup.Theme().Size(theme.SizeNameInnerPadding)
			popup.Resize(fyne.NewSize(width+padding, min(height, content.MinSize().Height+padding)))
		})
	}()
}

const maxSignaturePreviewPixels = 2_000_000

// Keep preview rasters retained by a dialog bounded even when the archive has
// many entries. Entries beyond the preview budget remain selectable.
func renderSignaturePreviews(entries []signatures.Entry, maxSide, pixelBudget int) []image.Image {
	previews := make([]image.Image, len(entries))
	if maxSide < 1 || pixelBudget < 1 {
		return previews
	}
	maxPerPreview := maxSide * maxSide
	count := min(len(entries), pixelBudget/maxPerPreview)
	for i := 0; i < count; i++ {
		previews[i], _ = entries[i].Asset.Render(maxSide)
	}
	return previews
}

// A whole preview is clickable; dropdown choices contain no action buttons.
type signatureChoice struct {
	widget.BaseWidget
	content fyne.CanvasObject
	border  *canvas.Rectangle
	tapped  func()
}

func newSignatureChoice(content fyne.CanvasObject, tapped func()) *signatureChoice {
	w := &signatureChoice{content: content, border: canvas.NewRectangle(color.Transparent), tapped: tapped}
	w.ExtendBaseWidget(w)
	return w
}
func (w *signatureChoice) Tapped(*fyne.PointEvent) {
	if w.tapped != nil {
		w.tapped()
	}
}
func (w *signatureChoice) MouseIn(*desktop.MouseEvent) {
	if w.tapped != nil {
		w.border.StrokeColor = theme.PrimaryColor()
		w.border.StrokeWidth = 2
		w.Refresh()
	}
}
func (w *signatureChoice) MouseMoved(*desktop.MouseEvent) {}
func (w *signatureChoice) MouseOut()                      { w.border.StrokeWidth = 0; w.Refresh() }
func (w *signatureChoice) Cursor() desktop.Cursor {
	if w.tapped != nil {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}
func (w *signatureChoice) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.New(signatureChoiceLayout{}, w.content, w.border))
}

// Keep the hover stroke inside the scroll viewport rather than clipping its
// outer half at the left and bottom edges of the final row.
type signatureChoiceLayout struct{}

func (signatureChoiceLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return objects[0].MinSize()
}
func (signatureChoiceLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
	objects[1].Move(fyne.NewPos(2, 2))
	objects[1].Resize(fyne.NewSize(max(0, size.Width-4), max(0, size.Height-4)))
}

// The trash action touches the preview and stretches to its complete height.
type signatureLibraryRowLayout struct{}

func (signatureLibraryRowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(objects[0].MinSize().Width+44, max(objects[0].MinSize().Height, objects[1].MinSize().Height))
}
func (signatureLibraryRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(max(0, size.Width-44), size.Height))
	objects[1].Move(fyne.NewPos(max(0, size.Width-44), 0))
	objects[1].Resize(fyne.NewSize(44, size.Height))
}
