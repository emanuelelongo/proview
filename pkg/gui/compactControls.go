package gui

import (
	"image/color"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func displayMeasure(v float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 2, 64), "0"), ".")
}

func numberSelect(values []float64, current float64, suffix string, changed func(float64)) *widget.Select {
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
	selectBox := widget.NewSelect(labels, nil)
	selectBox.SetSelected(displayMeasure(current) + suffix)
	selectBox.OnChanged = func(value string) { changed(lookup[value]) }
	return selectBox
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
