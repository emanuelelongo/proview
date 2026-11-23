package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const toolbarButtonSize float32 = 34

type toggleToolbarAction struct {
	button          *iconButton
	pulseBackground *canvas.Rectangle
	pulseContent    *fyne.Container
}

func newToggleToolbarAction(icon fyne.Resource, action func()) *toggleToolbarAction {
	button := newIconButton(icon, "", action)
	button.Importance = widget.LowImportance
	return &toggleToolbarAction{button: button}
}

func (a *toggleToolbarAction) ToolbarObject() fyne.CanvasObject {
	if a.pulseContent != nil {
		return a.pulseContent
	}
	return a.button
}

func (a *toggleToolbarAction) SetActive(active bool) {
	if active {
		a.button.Importance = widget.HighImportance
	} else {
		a.button.Importance = widget.LowImportance
	}
	a.button.Refresh()
}

func (a *toggleToolbarAction) Disable() { a.button.Disable() }
func (a *toggleToolbarAction) Enable()  { a.button.Enable() }

func (a *toggleToolbarAction) preparePulse() {
	a.pulseBackground = canvas.NewRectangle(color.Transparent)
	a.pulseBackground.CornerRadius = 8
	a.pulseContent = container.NewStack(a.pulseBackground, a.button)
}

// toolbarDivider draws a subtle vertical hairline between toolbar tool groups.
type toolbarDivider struct {
	widget.BaseWidget
}

func newToolbarDivider() *toolbarDivider {
	d := &toolbarDivider{}
	d.ExtendBaseWidget(d)
	return d
}

func (d *toolbarDivider) CreateRenderer() fyne.WidgetRenderer {
	line := canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
	return &toolbarDividerRenderer{divider: d, line: line}
}

type toolbarDividerRenderer struct {
	divider *toolbarDivider
	line    *canvas.Rectangle
}

func (r *toolbarDividerRenderer) Layout(size fyne.Size) {
	const inset float32 = 6
	height := max(1, size.Height-2*inset)
	r.line.Move(fyne.NewPos((size.Width-1)/2, inset))
	r.line.Resize(fyne.NewSize(1, height))
}
func (*toolbarDividerRenderer) MinSize() fyne.Size { return fyne.NewSize(9, toolbarButtonSize) }
func (r *toolbarDividerRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.line}
}
func (r *toolbarDividerRenderer) Refresh() {
	r.line.FillColor = theme.Color(theme.ColorNameSeparator)
	r.line.Refresh()
}
func (*toolbarDividerRenderer) Destroy() {}

func newModernToolbar(items ...widget.ToolbarItem) fyne.CanvasObject {
	row := container.NewHBox()
	for _, item := range items {
		if _, isSep := item.(*widget.ToolbarSeparator); isSep {
			row.Add(newToolbarDivider())
			continue
		}
		row.Add(container.NewGridWrap(fyne.NewSize(toolbarButtonSize, toolbarButtonSize), item.ToolbarObject()))
	}
	padded := container.New(layout.NewCustomPaddedLayout(6, 6, 10, 10), row)
	return newChromePanel(padded, chromeBorderBottom)
}
