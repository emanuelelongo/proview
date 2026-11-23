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

// proviewTheme provides a refined 3-level modern surface palette (canvas, chrome,
// elevated capsules) and rounded geometry for both light and dark modes.
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
			return color.NRGBA{R: 0xE7, G: 0xEA, B: 0xF1, A: 0xFF}
		case theme.ColorNameHeaderBackground:
			return color.NRGBA{R: 0xF6, G: 0xF7, B: 0xFB, A: 0xFF}
		case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
			return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
		case theme.ColorNameInputBackground:
			return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
		case theme.ColorNameInputBorder:
			return color.NRGBA{R: 0xD2, G: 0xD8, B: 0xE4, A: 0xFF}
		case theme.ColorNameButton:
			return color.NRGBA{R: 0xE6, G: 0xEA, B: 0xF2, A: 0xFF}
		case theme.ColorNameDisabledButton:
			return color.NRGBA{R: 0xEB, G: 0xEE, B: 0xF5, A: 0xFF}
		case theme.ColorNameHover:
			return color.NRGBA{R: 0x0F, G: 0x17, B: 0x2A, A: 0x10}
		case theme.ColorNamePressed:
			return color.NRGBA{R: 0x0F, G: 0x17, B: 0x2A, A: 0x1C}
		case theme.ColorNamePrimary:
			return color.NRGBA{R: 0x3A, G: 0x66, B: 0xF5, A: 0xFF}
		case theme.ColorNameFocus:
			return color.NRGBA{R: 0x5B, G: 0x82, B: 0xFF, A: 0xFF}
		case theme.ColorNameSelection:
			return color.NRGBA{R: 0x3A, G: 0x66, B: 0xF5, A: 0x22}
		case theme.ColorNameForeground:
			return color.NRGBA{R: 0x15, G: 0x18, B: 0x21, A: 0xFF}
		case theme.ColorNameForegroundOnPrimary:
			return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
		case theme.ColorNameDisabled:
			return color.NRGBA{R: 0x9A, G: 0xA2, B: 0xB4, A: 0xFF}
		case theme.ColorNamePlaceHolder:
			return color.NRGBA{R: 0x6B, G: 0x74, B: 0x89, A: 0xFF}
		case theme.ColorNameSeparator:
			return color.NRGBA{R: 0xD8, G: 0xDD, B: 0xE8, A: 0xFF}
		case theme.ColorNameScrollBar:
			return color.NRGBA{R: 0x64, G: 0x6E, B: 0x84, A: 0x6C}
		case theme.ColorNameScrollBarBackground:
			return color.Transparent
		case theme.ColorNameShadow:
			return color.NRGBA{R: 0x0F, G: 0x17, B: 0x2A, A: 0x20}
		case theme.ColorNameHyperlink:
			return color.NRGBA{R: 0x2F, G: 0x5C, B: 0xE6, A: 0xFF}
		}
		return t.Theme.Color(name, variant)
	}

	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 0x0E, G: 0x10, B: 0x15, A: 0xFF}
	case theme.ColorNameHeaderBackground:
		return color.NRGBA{R: 0x15, G: 0x18, B: 0x21, A: 0xFF}
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return color.NRGBA{R: 0x1C, G: 0x20, B: 0x2B, A: 0xFF}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 0x1D, G: 0x21, B: 0x2D, A: 0xFF}
	case theme.ColorNameInputBorder:
		return color.NRGBA{R: 0x2F, G: 0x35, B: 0x46, A: 0xFF}
	case theme.ColorNameButton:
		return color.NRGBA{R: 0x23, G: 0x28, B: 0x36, A: 0xFF}
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 0x1A, G: 0x1D, B: 0x27, A: 0xFF}
	case theme.ColorNameHover:
		return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x14}
	case theme.ColorNamePressed:
		return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x24}
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 0x5B, G: 0x82, B: 0xFF, A: 0xFF}
	case theme.ColorNameFocus:
		return color.NRGBA{R: 0x76, G: 0x98, B: 0xFF, A: 0xFF}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 0x5B, G: 0x82, B: 0xFF, A: 0x34}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 0xF0, G: 0xF2, B: 0xF7, A: 0xFF}
	case theme.ColorNameForegroundOnPrimary:
		return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 0x62, G: 0x6A, B: 0x7E, A: 0xFF}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 0x8B, G: 0x94, B: 0xAA, A: 0xFF}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 0x25, G: 0x2A, B: 0x37, A: 0xFF}
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 0x8A, G: 0x95, B: 0xAE, A: 0x68}
	case theme.ColorNameScrollBarBackground:
		return color.Transparent
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x66}
	case theme.ColorNameHyperlink:
		return color.NRGBA{R: 0x80, G: 0xA8, B: 0xFF, A: 0xFF}
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

// pageShadow draws a subtle, layered drop shadow behind floating pages and
// cards without changing their layout MinSize or coordinate geometry.
type pageShadow struct {
	widget.BaseWidget
	radius float32
	spread float32
}

func newPageShadow(radius, spread float32) *pageShadow {
	s := &pageShadow{radius: radius, spread: spread}
	s.ExtendBaseWidget(s)
	return s
}

func (s *pageShadow) CreateRenderer() fyne.WidgetRenderer {
	outer := canvas.NewRectangle(color.Transparent)
	inner := canvas.NewRectangle(color.Transparent)
	outer.CornerRadius = s.radius + 2
	inner.CornerRadius = s.radius + 1
	r := &pageShadowRenderer{shadow: s, outer: outer, inner: inner}
	r.applyColors()
	return r
}

type pageShadowRenderer struct {
	shadow *pageShadow
	outer  *canvas.Rectangle
	inner  *canvas.Rectangle
}

func (r *pageShadowRenderer) applyColors() {
	base := color.NRGBAModel.Convert(theme.Color(theme.ColorNameShadow)).(color.NRGBA)
	outerColor := base
	outerColor.A = uint8(min(255, int(base.A)*45/100))
	innerColor := base
	innerColor.A = uint8(min(255, int(base.A)*85/100))
	r.outer.FillColor = outerColor
	r.inner.FillColor = innerColor
}

func (r *pageShadowRenderer) Layout(size fyne.Size) {
	spread := r.shadow.spread
	r.outer.Move(fyne.NewPos(-spread, -spread*0.4+spread*0.6))
	r.outer.Resize(fyne.NewSize(size.Width+spread*2, size.Height+spread*2))
	half := spread * 0.45
	r.inner.Move(fyne.NewPos(-half, half*0.4))
	r.inner.Resize(fyne.NewSize(size.Width+half*2, size.Height+half*2))
}

func (*pageShadowRenderer) MinSize() fyne.Size { return fyne.Size{} }
func (r *pageShadowRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.outer, r.inner}
}
func (r *pageShadowRenderer) Refresh() {
	r.applyColors()
	r.outer.Refresh()
	r.inner.Refresh()
}
func (*pageShadowRenderer) Destroy() {}

// railDivider draws a centered hairline divider inside the vertical property rail.
type railDivider struct {
	widget.BaseWidget
}

func newRailDivider() *railDivider {
	d := &railDivider{}
	d.ExtendBaseWidget(d)
	return d
}

func (d *railDivider) CreateRenderer() fyne.WidgetRenderer {
	line := canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
	line.CornerRadius = 1
	return &railDividerRenderer{line: line}
}

type railDividerRenderer struct {
	line *canvas.Rectangle
}

func (r *railDividerRenderer) Layout(size fyne.Size) {
	width := min(float32(20), size.Width)
	r.line.Move(fyne.NewPos((size.Width-width)/2, (size.Height-1)/2))
	r.line.Resize(fyne.NewSize(width, 1))
}
func (*railDividerRenderer) MinSize() fyne.Size           { return fyne.NewSize(20, 9) }
func (r *railDividerRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.line} }
func (r *railDividerRenderer) Refresh() {
	r.line.FillColor = theme.Color(theme.ColorNameSeparator)
	r.line.Refresh()
}
func (*railDividerRenderer) Destroy() {}
