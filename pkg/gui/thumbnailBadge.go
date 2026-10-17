package gui

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"image/color"
)

// A passive overlay leaves the thumbnail's image size and hit testing intact.
type thumbnailBadge struct {
	widget.BaseWidget
	number int
}

func newThumbnailBadge(number int) *thumbnailBadge {
	b := &thumbnailBadge{number: number}
	b.ExtendBaseWidget(b)
	return b
}
func (b *thumbnailBadge) CreateRenderer() fyne.WidgetRenderer {
	text := canvas.NewText(fmt.Sprint(b.number), color.White)
	text.TextSize = 12
	text.TextStyle = fyne.TextStyle{Bold: true}
	return &thumbnailBadgeRenderer{badge: b, circle: canvas.NewCircle(theme.PrimaryColor()), text: text}
}

type thumbnailBadgeRenderer struct {
	badge  *thumbnailBadge
	circle *canvas.Circle
	text   *canvas.Text
}

func (r *thumbnailBadgeRenderer) Layout(size fyne.Size) {
	diameter := max(float32(24), r.text.MinSize().Width+10)
	x, y := max(0, size.Width-diameter-6), max(0, size.Height-diameter-6)
	r.circle.Move(fyne.NewPos(x, y))
	r.circle.Resize(fyne.NewSize(diameter, diameter))
	textSize := r.text.MinSize()
	r.text.Move(fyne.NewPos(x+(diameter-textSize.Width)/2, y+(diameter-textSize.Height)/2))
	r.text.Resize(textSize)
}
func (*thumbnailBadgeRenderer) MinSize() fyne.Size { return fyne.Size{} }
func (*thumbnailBadgeRenderer) Destroy()           {}
func (r *thumbnailBadgeRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.circle, r.text}
}
func (r *thumbnailBadgeRenderer) Refresh() {
	r.circle.FillColor = theme.PrimaryColor()
	r.circle.Refresh()
	r.Layout(r.badge.Size())
	r.text.Refresh()
	canvas.Refresh(r.badge)
}
