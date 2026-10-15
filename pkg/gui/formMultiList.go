package gui

import (
	"slices"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Rows use independent checkmarks: clicking or pressing Space on a row toggles
// one value without requiring Ctrl/Command and without clearing other choices.
type formMultiList struct {
	widget.List
	cursor                   int
	navigating               bool
	options, exports, values []string
	changed                  func([]string)
}

func newFormMultiList(field document.FormField, changed func([]string)) *formMultiList {
	m := &formMultiList{cursor: -1, options: field.Options, exports: field.ExportOptions, values: slices.Clone(field.Values), changed: changed}
	m.Length = func() int { return len(m.options) }
	m.CreateItem = func() fyne.CanvasObject {
		label := widget.NewLabel("")
		check := newFormToggle(false, false, nil)
		check.inList = true
		return container.NewBorder(nil, nil, check, nil, label)
	}
	m.UpdateItem = func(id widget.ListItemID, obj fyne.CanvasObject) {
		row := obj.(*fyne.Container)
		for _, child := range row.Objects {
			switch control := child.(type) {
			case *widget.Label:
				control.SetText(m.options[id])
			case *formToggle:
				control.OnChanged = nil
				control.SetChecked(slices.Contains(m.values, m.exports[id]))
				control.OnChanged = func(on bool) { m.toggle(id, on) }
			}
		}
	}
	m.HideSeparators = true
	m.OnSelected = func(id widget.ListItemID) {
		m.cursor = id
		if m.navigating {
			return
		}
		m.toggle(id, !slices.Contains(m.values, m.exports[id]))
		m.Unselect(id)
	}
	m.ExtendBaseWidget(m)
	return m
}
func (m *formMultiList) toggle(id int, on bool) {
	if id < 0 || id >= len(m.exports) {
		return
	}
	values := slices.Clone(m.values)
	if on {
		if !slices.Contains(values, m.exports[id]) {
			values = append(values, m.exports[id])
		}
	} else {
		values = slices.DeleteFunc(values, func(value string) bool { return value == m.exports[id] })
	}
	m.SetValues(values)
	if m.changed != nil {
		m.changed(slices.Clone(m.values))
	}
}
func (m *formMultiList) SetValues(values []string) {
	if slices.Equal(values, m.values) {
		return
	}
	m.values = slices.Clone(values)
	m.Refresh()
}

func (m *formMultiList) TypedKey(key *fyne.KeyEvent) {
	if len(m.options) == 0 {
		return
	}
	switch key.Name {
	case fyne.KeyDown:
		m.cursor = min(len(m.options)-1, m.cursor+1)
	case fyne.KeyUp:
		m.cursor = max(0, m.cursor-1)
	case fyne.KeyHome:
		m.cursor = 0
	case fyne.KeyEnd:
		m.cursor = len(m.options) - 1
	case fyne.KeyReturn, fyne.KeyEnter:
		m.TypedRune(' ')
		return
	default:
		return
	}
	m.navigating = true
	m.List.Select(m.cursor)
	m.navigating = false
}
func (m *formMultiList) TypedRune(r rune) {
	if r != ' ' || len(m.options) == 0 {
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.toggle(m.cursor, !slices.Contains(m.values, m.exports[m.cursor]))
}
