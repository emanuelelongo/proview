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

const toolbarButtonSize float32 = 32

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

// toolbarCapsule groups related toolbar actions inside an elevated rounded pill.
type toolbarCapsule struct {
	widget.BaseWidget
	content fyne.CanvasObject
}

func newToolbarCapsule(objects ...fyne.CanvasObject) *toolbarCapsule {
	row := container.New(layout.NewHBoxLayout(), objects...)
	padded := container.New(layout.NewCustomPaddedLayout(3, 3, 4, 4), row)
	c := &toolbarCapsule{content: padded}
	c.ExtendBaseWidget(c)
	return c
}

func (c *toolbarCapsule) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	bg.CornerRadius = 10
	bg.StrokeColor = theme.Color(theme.ColorNameSeparator)
	bg.StrokeWidth = 1
	return &toolbarCapsuleRenderer{
		WidgetRenderer: widget.NewSimpleRenderer(container.NewStack(bg, c.content)),
		bg:             bg,
	}
}

type toolbarCapsuleRenderer struct {
	fyne.WidgetRenderer
	bg *canvas.Rectangle
}

func (r *toolbarCapsuleRenderer) Refresh() {
	r.bg.FillColor = theme.Color(theme.ColorNameInputBackground)
	r.bg.StrokeColor = theme.Color(theme.ColorNameSeparator)
	r.bg.Refresh()
	r.WidgetRenderer.Refresh()
}

func newModernToolbar(items ...widget.ToolbarItem) fyne.CanvasObject {
	var groups [][]fyne.CanvasObject
	var current []fyne.CanvasObject
	for _, item := range items {
		if _, isSep := item.(*widget.ToolbarSeparator); isSep {
			if len(current) > 0 {
				groups = append(groups, current)
				current = nil
			}
			continue
		}
		current = append(current, container.NewGridWrap(fyne.NewSize(toolbarButtonSize, toolbarButtonSize), item.ToolbarObject()))
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}

	row := container.NewHBox()
	for i, group := range groups {
		if i > 0 {
			if i == len(groups)-1 && len(groups) > 1 {
				// Right-align the zoom capsule for a balanced studio layout.
				row.Add(layout.NewSpacer())
			} else {
				gap := canvas.NewRectangle(color.Transparent)
				gap.SetMinSize(fyne.NewSize(4, 1))
				row.Add(gap)
			}
		}
		row.Add(newToolbarCapsule(group...))
	}
	padded := container.New(layout.NewCustomPaddedLayout(7, 7, 10, 10), row)
	return newChromePanel(padded, chromeBorderBottom)
}
