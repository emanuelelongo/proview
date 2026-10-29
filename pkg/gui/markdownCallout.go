package gui

import (
	"image/color"
	"strings"

	"emanuelelongo/proview/internal/markdown"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (v *markdownView) callout(n *markdown.Admonition) fyne.CanvasObject {
	icon := "info-circle"
	switch n.Category {
	case "tip":
		icon = "bulb"
	case "warning", "caution", "danger":
		icon = "alert-triangle"
	}
	title := v.selectable(&widget.TextSegment{Text: strings.ToUpper(n.Label), Style: widget.RichTextStyleStrong})
	header := container.NewBorder(nil, nil, container.NewCenter(widget.NewIcon(tablerIcon(icon))), nil, v.styleText(&title.RichText, title))
	children := append([]fyne.CanvasObject{header}, v.blocks(n)...)
	body := container.New(layout.NewCustomPaddedLayout(12, 12, 20, 16), container.NewVBox(children...))
	background := &markdownCalloutBackground{view: v, kind: n.Category}
	background.ExtendBaseWidget(background)
	return container.NewStack(background, body)
}

// A passive background keeps the content in the ordinary container tree, so
// text selection, search and offscreen culling retain their existing geometry.
type markdownCalloutBackground struct {
	widget.BaseWidget
	view *markdownView
	kind string
}

func (b *markdownCalloutBackground) CreateRenderer() fyne.WidgetRenderer {
	r := &markdownCalloutRenderer{background: b, fill: canvas.NewRectangle(color.Transparent), stripe: canvas.NewRectangle(color.Transparent)}
	r.fill.CornerRadius = 6
	r.stripe.TopLeftCornerRadius, r.stripe.BottomLeftCornerRadius = 6, 6
	r.Refresh()
	return r
}

type markdownCalloutRenderer struct {
	background   *markdownCalloutBackground
	fill, stripe *canvas.Rectangle
}

func (*markdownCalloutRenderer) MinSize() fyne.Size { return fyne.Size{} }
func (*markdownCalloutRenderer) Destroy()           {}
func (r *markdownCalloutRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.fill, r.stripe}
}
func (r *markdownCalloutRenderer) Layout(size fyne.Size) {
	r.fill.Resize(size)
	r.stripe.Resize(fyne.NewSize(5, size.Height))
}
func (r *markdownCalloutRenderer) Refresh() {
	base := r.background.view.readingTheme().Color(theme.ColorNameBackground, fyne.CurrentApp().Settings().ThemeVariant())
	red, green, blue, _ := base.RGBA()
	dark := red+green+blue < 3*32768
	lightFill, darkFill, accent := calloutColors(r.background.kind)
	r.fill.FillColor = lightFill
	if dark {
		r.fill.FillColor = darkFill
	}
	if r.background.kind == "details" {
		r.fill.StrokeColor = accent
		r.fill.StrokeWidth = 1
		r.stripe.Hide()
	} else if r.background.kind == "code" {
		r.stripe.Hide()
	} else {
		r.stripe.FillColor = accent
	}
	r.fill.Refresh()
	r.stripe.Refresh()
}
func calloutColors(kind string) (color.Color, color.Color, color.Color) {
	switch kind {
	case "code":
		return color.NRGBA{R: 240, G: 242, B: 247, A: 255}, color.NRGBA{R: 40, G: 44, B: 60, A: 255}, color.Transparent
	case "details":
		return color.NRGBA{R: 231, G: 246, B: 249, A: 255}, color.NRGBA{R: 25, G: 60, B: 71, A: 255}, color.NRGBA{R: 65, G: 174, B: 199, A: 255}
	case "tip":
		return color.NRGBA{R: 230, G: 246, B: 230, A: 255}, color.NRGBA{R: 0, G: 48, B: 0, A: 255}, color.NRGBA{R: 0, G: 155, B: 0, A: 255}
	case "important":
		return color.NRGBA{R: 244, G: 235, B: 255, A: 255}, color.NRGBA{R: 49, G: 28, B: 70, A: 255}, color.NRGBA{R: 145, G: 75, B: 205, A: 255}
	case "warning", "caution":
		return color.NRGBA{R: 255, G: 245, B: 204, A: 255}, color.NRGBA{R: 77, G: 57, B: 0, A: 255}, color.NRGBA{R: 230, G: 172, B: 0, A: 255}
	case "danger":
		return color.NRGBA{R: 255, G: 232, B: 232, A: 255}, color.NRGBA{R: 70, G: 15, B: 20, A: 255}, color.NRGBA{R: 220, G: 55, B: 65, A: 255}
	default:
		return color.NRGBA{R: 230, G: 240, B: 255, A: 255}, color.NRGBA{R: 15, G: 40, B: 65, A: 255}, color.NRGBA{R: 60, G: 140, B: 225, A: 255}
	}
}
