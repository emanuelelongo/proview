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

// emptyState presents a clean welcome screen when no document is open.
type emptyState struct {
	widget.BaseWidget
	title    *canvas.Text
	subtitle *canvas.Text
	hint     *canvas.Text
	content  fyne.CanvasObject
}

func newEmptyState(g *Gui) *emptyState {
	icon := canvas.NewImageFromResource(fyne.NewStaticResource("app-icon.png", assets.AppIcon))
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(72, 72))

	title := canvas.NewText("Proview", theme.Color(theme.ColorNameForeground))
	title.TextSize = 22
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	subtitle := canvas.NewText("Drop a PDF, image, Markdown or STL file to get started", theme.Color(theme.ColorNamePlaceHolder))
	subtitle.TextSize = 13
	subtitle.Alignment = fyne.TextAlignCenter

	openBtn := widget.NewButtonWithIcon("Open file", tablerIcon("folder"), g.requestOpen)
	openBtn.Importance = widget.HighImportance
	newBtn := widget.NewButtonWithIcon("New page", tablerIcon("file-spark"), g.newPage)
	newBtn.Importance = widget.MediumImportance

	buttons := container.NewCenter(container.NewHBox(
		container.NewGridWrap(fyne.NewSize(128, 36), openBtn),
		layout.NewSpacer(),
		container.NewGridWrap(fyne.NewSize(128, 36), newBtn),
	))

	hintText := "Ctrl+O Open  ·  Ctrl+N New page"
	if runtime.GOOS == "darwin" {
		hintText = "⌘O Open  ·  ⌘N New page"
	}
	hint := canvas.NewText(hintText, theme.Color(theme.ColorNameDisabled))
	hint.TextSize = 11
	hint.Alignment = fyne.TextAlignCenter

	stack := container.NewVBox(
		container.NewCenter(icon),
		container.New(layout.NewCustomPaddedLayout(6, 0, 0, 0), title),
		container.New(layout.NewCustomPaddedLayout(0, 14, 0, 0), subtitle),
		buttons,
		container.New(layout.NewCustomPaddedLayout(12, 0, 0, 0), hint),
	)

	e := &emptyState{
		title:    title,
		subtitle: subtitle,
		hint:     hint,
		content:  container.NewCenter(container.NewPadded(stack)),
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
	r.state.title.Color = theme.Color(theme.ColorNameForeground)
	r.state.subtitle.Color = theme.Color(theme.ColorNamePlaceHolder)
	r.state.hint.Color = theme.Color(theme.ColorNameDisabled)
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
