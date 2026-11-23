package gui

import (
	"emanuelelongo/proview/assets"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// formatPill displays a supported file type badge on the welcome card.
type formatPill struct {
	widget.BaseWidget
	bg    *canvas.Rectangle
	label *canvas.Text
	body  fyne.CanvasObject
}

func newFormatPill(text string) *formatPill {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	bg.CornerRadius = 6
	bg.StrokeColor = theme.Color(theme.ColorNameSeparator)
	bg.StrokeWidth = 1
	label := canvas.NewText(text, theme.Color(theme.ColorNamePlaceHolder))
	label.TextSize = 11
	label.Alignment = fyne.TextAlignCenter
	p := &formatPill{
		bg:    bg,
		label: label,
		body:  container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(4, 4, 10, 10), label)),
	}
	p.ExtendBaseWidget(p)
	return p
}

func (p *formatPill) CreateRenderer() fyne.WidgetRenderer {
	return &formatPillRenderer{WidgetRenderer: widget.NewSimpleRenderer(p.body), pill: p}
}

type formatPillRenderer struct {
	fyne.WidgetRenderer
	pill *formatPill
}

func (r *formatPillRenderer) Refresh() {
	r.pill.bg.FillColor = theme.Color(theme.ColorNameInputBackground)
	r.pill.bg.StrokeColor = theme.Color(theme.ColorNameSeparator)
	r.pill.label.Color = theme.Color(theme.ColorNamePlaceHolder)
	r.pill.bg.Refresh()
	r.pill.label.Refresh()
	r.WidgetRenderer.Refresh()
}

// emptyState presents a modern elevated welcome card when no document is open.
type emptyState struct {
	widget.BaseWidget
	cardBG   *canvas.Rectangle
	iconHalo *canvas.Circle
	title    *canvas.Text
	subtitle *canvas.Text
	hint     *canvas.Text
	content  fyne.CanvasObject
}

func newEmptyState(g *Gui) *emptyState {
	icon := canvas.NewImageFromResource(fyne.NewStaticResource("app-icon.png", assets.AppIcon))
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(62, 62))

	halo := canvas.NewCircle(theme.Color(theme.ColorNameSelection))
	halo.StrokeColor = theme.Color(theme.ColorNameSeparator)
	halo.StrokeWidth = 1
	iconBadge := container.NewGridWrap(fyne.NewSize(90, 90), container.NewStack(halo, container.NewCenter(icon)))

	title := canvas.NewText("Proview", theme.Color(theme.ColorNameForeground))
	title.TextSize = 22
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	subtitle := canvas.NewText("Drop a file anywhere to view, annotate or edit", theme.Color(theme.ColorNamePlaceHolder))
	subtitle.TextSize = 13
	subtitle.Alignment = fyne.TextAlignCenter

	pills := container.NewCenter(container.NewHBox(
		newFormatPill("PDF"),
		newFormatPill("Images"),
		newFormatPill("Markdown"),
		newFormatPill("3D STL"),
	))

	openBtn := widget.NewButtonWithIcon("Open file", tablerIcon("folder"), g.requestOpen)
	openBtn.Importance = widget.HighImportance
	newBtn := widget.NewButtonWithIcon("New page", tablerIcon("file-spark"), g.newPage)
	newBtn.Importance = widget.MediumImportance

	buttons := container.NewCenter(container.NewHBox(
		container.NewGridWrap(fyne.NewSize(142, 36), openBtn),
		container.NewGridWrap(fyne.NewSize(142, 36), newBtn),
	))

	hintText := "Ctrl+O Open  ·  Ctrl+N New page"
	if runtime.GOOS == "darwin" {
		hintText = "⌘O Open  ·  ⌘N New page"
	}
	hint := canvas.NewText(hintText, theme.Color(theme.ColorNameDisabled))
	hint.TextSize = 11
	hint.Alignment = fyne.TextAlignCenter

	cardBody := container.NewVBox(
		container.NewCenter(iconBadge),
		container.New(layout.NewCustomPaddedLayout(8, 0, 0, 0), title),
		container.New(layout.NewCustomPaddedLayout(0, 10, 0, 0), subtitle),
		container.New(layout.NewCustomPaddedLayout(0, 16, 0, 0), pills),
		buttons,
		container.New(layout.NewCustomPaddedLayout(14, 0, 0, 0), hint),
	)

	cardBG := canvas.NewRectangle(theme.Color(theme.ColorNameHeaderBackground))
	cardBG.CornerRadius = 18
	cardBG.StrokeColor = theme.Color(theme.ColorNameSeparator)
	cardBG.StrokeWidth = 1

	card := container.NewStack(
		newPageShadow(18, 10),
		cardBG,
		container.New(layout.NewCustomPaddedLayout(28, 24, 36, 36), cardBody),
	)

	e := &emptyState{
		cardBG:   cardBG,
		iconHalo: halo,
		title:    title,
		subtitle: subtitle,
		hint:     hint,
		content:  container.NewCenter(container.NewPadded(card)),
	}
	e.ExtendBaseWidget(e)
	return e
}

func (e *emptyState) CreateRenderer() fyne.WidgetRenderer {
	return &emptyStateRenderer{
		WidgetRenderer: widget.NewSimpleRenderer(e.content),
		state:          e,
	}
}

type emptyStateRenderer struct {
	fyne.WidgetRenderer
	state *emptyState
}

func (r *emptyStateRenderer) Refresh() {
	r.state.cardBG.FillColor = theme.Color(theme.ColorNameHeaderBackground)
	r.state.cardBG.StrokeColor = theme.Color(theme.ColorNameSeparator)
	r.state.iconHalo.FillColor = theme.Color(theme.ColorNameSelection)
	r.state.iconHalo.StrokeColor = theme.Color(theme.ColorNameSeparator)
	r.state.title.Color = theme.Color(theme.ColorNameForeground)
	r.state.subtitle.Color = theme.Color(theme.ColorNamePlaceHolder)
	r.state.hint.Color = theme.Color(theme.ColorNameDisabled)
	r.state.cardBG.Refresh()
	r.state.iconHalo.Refresh()
	r.state.title.Refresh()
	r.state.subtitle.Refresh()
	r.state.hint.Refresh()
	r.WidgetRenderer.Refresh()
}

func (g *Gui) updateEmptyState() {
	if g.emptyState == nil {
		return
	}
	if g.hasOpenContent() {
		g.emptyState.Hide()
	} else {
		g.emptyState.Show()
	}
}
