package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const iconSizeIncrease = float32(3)

type largerIconTheme struct {
	fyne.Theme
}

func (t largerIconTheme) Size(name fyne.ThemeSizeName) float32 {
	size := t.Theme.Size(name)
	if name == theme.SizeNameInlineIcon {
		return size + iconSizeIncrease
	}
	return size
}

// proviewTheme provides a refined, modern light/dark palette and geometry
// while preserving Fyne's fonts and icon lookups.
type proviewTheme struct {
	fyne.Theme
}

func newProviewTheme(base fyne.Theme) fyne.Theme {
	if base == nil {
		base = theme.DefaultTheme()
	}
	return proviewTheme{Theme: base}
}

func (t proviewTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if variant == theme.VariantLight {
		switch name {
		case theme.ColorNameBackground:
			return color.NRGBA{R: 0xEC, G: 0xEE, B: 0xF3, A: 0xFF}
		case theme.ColorNameHeaderBackground:
			return color.NRGBA{R: 0xF7, G: 0xF8, B: 0xFB, A: 0xFF}
		case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
			return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
		case theme.ColorNameInputBackground:
			return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
		case theme.ColorNameInputBorder:
			return color.NRGBA{R: 0xD2, G: 0xD7, B: 0xE2, A: 0xFF}
		case theme.ColorNameButton:
			return color.NRGBA{R: 0xE4, G: 0xE8, B: 0xF0, A: 0xFF}
		case theme.ColorNameDisabledButton:
			return color.NRGBA{R: 0xEA, G: 0xED, B: 0xF3, A: 0xFF}
		case theme.ColorNameHover:
			return color.NRGBA{R: 0x0F, G: 0x17, B: 0x2A, A: 0x10}
		case theme.ColorNamePressed:
			return color.NRGBA{R: 0x0F, G: 0x17, B: 0x2A, A: 0x1C}
		case theme.ColorNamePrimary:
			return color.NRGBA{R: 0x3B, G: 0x68, B: 0xF5, A: 0xFF}
		case theme.ColorNameFocus:
			return color.NRGBA{R: 0x52, G: 0x7B, B: 0xFF, A: 0xFF}
		case theme.ColorNameSelection:
			return color.NRGBA{R: 0x3B, G: 0x68, B: 0xF5, A: 0x24}
		case theme.ColorNameForeground:
			return color.NRGBA{R: 0x18, G: 0x1B, B: 0x22, A: 0xFF}
		case theme.ColorNameForegroundOnPrimary:
			return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
		case theme.ColorNameDisabled:
			return color.NRGBA{R: 0x9A, G: 0xA1, B: 0xB2, A: 0xFF}
		case theme.ColorNamePlaceHolder:
			return color.NRGBA{R: 0x6F, G: 0x77, B: 0x8A, A: 0xFF}
		case theme.ColorNameSeparator:
			return color.NRGBA{R: 0xD8, G: 0xDC, B: 0xE6, A: 0xFF}
		case theme.ColorNameScrollBar:
			return color.NRGBA{R: 0x64, G: 0x6E, B: 0x82, A: 0x70}
		case theme.ColorNameScrollBarBackground:
			return color.Transparent
		case theme.ColorNameShadow:
			return color.NRGBA{R: 0x0F, G: 0x17, B: 0x2A, A: 0x22}
		case theme.ColorNameHyperlink:
			return color.NRGBA{R: 0x2F, G: 0x5C, B: 0xE6, A: 0xFF}
		}
		return t.Theme.Color(name, variant)
	}

	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 0x13, G: 0x15, B: 0x1A, A: 0xFF}
	case theme.ColorNameHeaderBackground:
		return color.NRGBA{R: 0x1B, G: 0x1E, B: 0x25, A: 0xFF}
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return color.NRGBA{R: 0x20, G: 0x24, B: 0x2D, A: 0xFF}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 0x24, G: 0x28, B: 0x33, A: 0xFF}
	case theme.ColorNameInputBorder:
		return color.NRGBA{R: 0x35, G: 0x3B, B: 0x4A, A: 0xFF}
	case theme.ColorNameButton:
		return color.NRGBA{R: 0x26, G: 0x2B, B: 0x36, A: 0xFF}
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 0x1F, G: 0x22, B: 0x2B, A: 0xFF}
	case theme.ColorNameHover:
		return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x14}
	case theme.ColorNamePressed:
		return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x24}
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 0x52, G: 0x7B, B: 0xFF, A: 0xFF}
	case theme.ColorNameFocus:
		return color.NRGBA{R: 0x6E, G: 0x91, B: 0xFF, A: 0xFF}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 0x52, G: 0x7B, B: 0xFF, A: 0x38}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 0xEE, G: 0xF0, B: 0xF5, A: 0xFF}
	case theme.ColorNameForegroundOnPrimary:
		return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 0x67, G: 0x6F, B: 0x80, A: 0xFF}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 0x8B, G: 0x93, B: 0xA6, A: 0xFF}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 0x2B, G: 0x30, B: 0x3B, A: 0xFF}
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 0x8A, G: 0x94, B: 0xA8, A: 0x6E}
	case theme.ColorNameScrollBarBackground:
		return color.Transparent
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x5A}
	case theme.ColorNameHyperlink:
		return color.NRGBA{R: 0x7B, G: 0xA3, B: 0xFF, A: 0xFF}
	}
	return t.Theme.Color(name, variant)
}

func (t proviewTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameInputRadius:
		return 8
	case theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameScrollBarRadius:
		return 4
	case theme.SizeNameScrollBar:
		return 8
	case theme.SizeNameScrollBarSmall:
		return 4
	case theme.SizeNameSeparatorThickness:
		return 1
	case theme.SizeNamePadding:
		return 5
	case theme.SizeNameInnerPadding:
		return 7
	}
	return t.Theme.Size(name)
}

type chromeBorderEdge uint8

const (
	chromeBorderTop chromeBorderEdge = 1 << iota
	chromeBorderBottom
	chromeBorderLeft
	chromeBorderRight
)

// chromePanel paints an elevated application chrome surface with optional
// 1px hairline dividers that automatically track light/dark theme changes.
type chromePanel struct {
	widget.BaseWidget
	content fyne.CanvasObject
	edges   chromeBorderEdge
}

func newChromePanel(content fyne.CanvasObject, edges chromeBorderEdge) *chromePanel {
	p := &chromePanel{content: content, edges: edges}
	p.ExtendBaseWidget(p)
	return p
}

func (p *chromePanel) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameHeaderBackground))
	var borders [4]*canvas.Rectangle
	for i := range borders {
		borders[i] = canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
		borders[i].Hide()
	}
	r := &chromePanelRenderer{panel: p, bg: bg, borders: borders}
	r.syncBorders()
	return r
}

type chromePanelRenderer struct {
	panel   *chromePanel
	bg      *canvas.Rectangle
	borders [4]*canvas.Rectangle // top, bottom, left, right
}

func (r *chromePanelRenderer) syncBorders() {
	edges := r.panel.edges
	flags := [4]chromeBorderEdge{chromeBorderTop, chromeBorderBottom, chromeBorderLeft, chromeBorderRight}
	for i, flag := range flags {
		if edges&flag != 0 {
			r.borders[i].Show()
		} else {
			r.borders[i].Hide()
		}
	}
}

func (r *chromePanelRenderer) Layout(size fyne.Size) {
	r.bg.Move(fyne.Position{})
	r.bg.Resize(size)

	const thickness float32 = 1
	var top, bottom, left, right float32
	if r.panel.edges&chromeBorderTop != 0 {
		top = thickness
		r.borders[0].Move(fyne.NewPos(0, 0))
		r.borders[0].Resize(fyne.NewSize(size.Width, thickness))
	}
	if r.panel.edges&chromeBorderBottom != 0 {
		bottom = thickness
		r.borders[1].Move(fyne.NewPos(0, max(0, size.Height-thickness)))
		r.borders[1].Resize(fyne.NewSize(size.Width, thickness))
	}
	if r.panel.edges&chromeBorderLeft != 0 {
		left = thickness
		r.borders[2].Move(fyne.NewPos(0, 0))
		r.borders[2].Resize(fyne.NewSize(thickness, size.Height))
	}
	if r.panel.edges&chromeBorderRight != 0 {
		right = thickness
		r.borders[3].Move(fyne.NewPos(max(0, size.Width-thickness), 0))
		r.borders[3].Resize(fyne.NewSize(thickness, size.Height))
	}
	r.panel.content.Move(fyne.NewPos(left, top))
	r.panel.content.Resize(fyne.NewSize(max(0, size.Width-left-right), max(0, size.Height-top-bottom)))
}

func (r *chromePanelRenderer) MinSize() fyne.Size {
	minSize := r.panel.content.MinSize()
	if r.panel.edges&chromeBorderTop != 0 {
		minSize.Height += 1
	}
	if r.panel.edges&chromeBorderBottom != 0 {
		minSize.Height += 1
	}
	if r.panel.edges&chromeBorderLeft != 0 {
		minSize.Width += 1
	}
	if r.panel.edges&chromeBorderRight != 0 {
		minSize.Width += 1
	}
	return minSize
}

func (r *chromePanelRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.panel.content, r.borders[0], r.borders[1], r.borders[2], r.borders[3]}
}

func (r *chromePanelRenderer) Refresh() {
	r.bg.FillColor = theme.Color(theme.ColorNameHeaderBackground)
	r.bg.Refresh()
	sep := theme.Color(theme.ColorNameSeparator)
	for _, b := range r.borders {
		b.FillColor = sep
		b.Refresh()
	}
	r.syncBorders()
	r.panel.content.Refresh()
	canvas.Refresh(r.panel)
}

func (r *chromePanelRenderer) Destroy() {}
