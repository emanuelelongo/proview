package gui

import (
	"image/color"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func displayMeasure(v float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 2, 64), "0"), ".")
}

func numberSelect(icon, label string, values []float64, current float64, suffix string, changed func(float64)) *compactSelect {
	values = append(append([]float64(nil), values...), current)
	sort.Float64s(values)
	var labels []string
	lookup := map[string]float64{}
	for _, v := range values {
		label := displayMeasure(v) + suffix
		if _, exists := lookup[label]; !exists {
			labels = append(labels, label)
			lookup[label] = v
		}
	}
	selectBox := newCompactSelect(labels, icon, label, nil)
	selectBox.SetSelected(displayMeasure(current) + suffix)
	selectBox.OnChanged = func(value string) { changed(lookup[value]) }
	return selectBox
}

// compactSelect keeps the selected value in its tooltip and popup, so long
// option names never affect the width of the property rail.
type compactSelect struct {
	widget.Select
	button      *iconButton
	content     *fyne.Container
	label       string
	optionLabel func(string) string
}

func newCompactSelect(options []string, icon, label string, changed func(string)) *compactSelect {
	s := &compactSelect{Select: widget.Select{Options: options, OnChanged: changed}, label: label}
	s.ExtendBaseWidget(s)
	s.button = newIconButton(tablerIcon(icon), label, s.showOptions)
	s.content = compactDropdownContent(s.button)
	return s
}

func (s *compactSelect) displayOption(value string) string {
	if s.optionLabel != nil {
		return s.optionLabel(value)
	}
	return value
}

func compactDropdownContent(button fyne.CanvasObject) *fyne.Container {
	arrow := canvas.NewImageFromResource(tablerIcon("chevron-down"))
	arrow.FillMode = canvas.ImageFillContain
	return container.New(shapeSelectorLayout{}, button, arrow)
}

func (s *compactSelect) showOptions() {
	if s.Disabled() {
		return
	}
	items := make([]*fyne.MenuItem, 0, len(s.Options))
	for _, value := range s.Options {
		item := fyne.NewMenuItem(s.displayOption(value), func() { s.SetSelected(value) })
		item.Checked = value == s.Selected
		items = append(items, item)
	}
	c := fyne.CurrentApp().Driver().CanvasForObject(s)
	if c == nil {
		return
	}
	p := fyne.CurrentApp().Driver().AbsolutePositionForObject(s)
	p.Y += s.Size().Height
	widget.NewPopUpMenu(fyne.NewMenu(s.label, items...), c).ShowAtPosition(p)
}

func (s *compactSelect) Tapped(*fyne.PointEvent) { s.showOptions() }
func (s *compactSelect) TypedKey(event *fyne.KeyEvent) {
	if s.Disabled() {
		return
	}
	switch event.Name {
	case fyne.KeySpace, fyne.KeyUp, fyne.KeyDown:
		s.showOptions()
	default:
		s.Select.TypedKey(event)
	}
}
func (s *compactSelect) CreateRenderer() fyne.WidgetRenderer {
	return &compactSelectRenderer{WidgetRenderer: widget.NewSimpleRenderer(s.content), selectBox: s}
}

type compactSelectRenderer struct {
	fyne.WidgetRenderer
	selectBox *compactSelect
}

func (r *compactSelectRenderer) Refresh() {
	s := r.selectBox
	s.button.tooltip = s.label + ": " + s.displayOption(s.Selected)
	if s.Disabled() {
		s.button.Disable()
	} else {
		s.button.Enable()
	}
	r.WidgetRenderer.Refresh()
}

type colorSwatch struct {
	widget.BaseWidget
	rect     *canvas.Rectangle
	tapped   func()
	disabled bool
}

func newColorSwatch(c color.Color, tapped func()) *colorSwatch {
	s := &colorSwatch{rect: canvas.NewRectangle(c), tapped: tapped}
	s.rect.StrokeColor = theme.ForegroundColor()
	s.rect.StrokeWidth = 1
	s.ExtendBaseWidget(s)
	return s
}
func (s *colorSwatch) Tapped(*fyne.PointEvent) {
	if !s.disabled && s.tapped != nil {
		s.tapped()
	}
}
func (s *colorSwatch) Disable() {
	s.disabled = true
	s.rect.StrokeColor = theme.DisabledColor()
	s.Refresh()
}
func (s *colorSwatch) Enable() {
	s.disabled = false
	s.rect.StrokeColor = theme.ForegroundColor()
	s.Refresh()
}
func (s *colorSwatch) Disabled() bool                      { return s.disabled }
func (s *colorSwatch) setColor(c color.Color)              { s.rect.FillColor = c; s.Refresh() }
func (s *colorSwatch) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(s.rect) }
