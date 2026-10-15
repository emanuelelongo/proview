package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// PDF button widgets are often smaller than a standard Fyne Check. Draw within
// their actual bounds, with a paper-colored face independent of the app theme.
type formToggle struct {
	widget.DisableableWidget
	radio, Checked   bool
	inList           bool
	focused, hovered bool
	OnChanged        func(bool)
}

func newFormToggle(radio, checked bool, changed func(bool)) *formToggle {
	t := &formToggle{radio: radio, Checked: checked, OnChanged: changed}
	t.ExtendBaseWidget(t)
	return t
}
func (t *formToggle) SetChecked(checked bool) {
	if t.Checked == checked {
		return
	}
	t.Checked = checked
	if t.OnChanged != nil {
		t.OnChanged(checked)
	}
	t.Refresh()
}
func (t *formToggle) Tapped(*fyne.PointEvent) {
	if t.Disabled() || t.radio && t.Checked {
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(t); c != nil {
		c.Focus(t)
	}
	t.SetChecked(!t.Checked)
}
func (t *formToggle) FocusGained() { t.focused = true; t.Refresh() }
func (t *formToggle) FocusLost()   { t.focused = false; t.Refresh() }
func (t *formToggle) TypedRune(r rune) {
	if r == ' ' {
		t.Tapped(nil)
	}
}
func (t *formToggle) TypedKey(*fyne.KeyEvent)        {}
func (t *formToggle) MouseIn(*desktop.MouseEvent)    { t.hovered = true; t.Refresh() }
func (t *formToggle) MouseMoved(*desktop.MouseEvent) {}
func (t *formToggle) MouseOut()                      { t.hovered = false; t.Refresh() }
func (t *formToggle) Cursor() desktop.Cursor         { return desktop.PointerCursor }

var formToggleInk = color.NRGBA{R: 0x17, G: 0x2c, B: 0x45, A: 255}

// Render both circles in one image so pixel rounding at different zoom levels
// cannot move the dot independently of the outer ring.
const formRadioSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100"><circle cx="50" cy="50" r="50" fill="#172c45"/><circle cx="50" cy="50" r="42" fill="white"/>`

var (
	formRadioEmpty    = fyne.NewStaticResource("pdf-radio-empty.svg", []byte(formRadioSVG+`</svg>`))
	formRadioSelected = fyne.NewStaticResource("pdf-radio-selected.svg", []byte(formRadioSVG+`<circle cx="50" cy="50" r="22" fill="#172c45"/></svg>`))
)

func (t *formToggle) CreateRenderer() fyne.WidgetRenderer {
	r := &formToggleRenderer{toggle: t, background: canvas.NewRectangle(color.White)}
	if t.inList {
		r.background.FillColor = color.Transparent
	}
	if t.radio {
		r.radio = canvas.NewImageFromResource(formRadioEmpty)
		r.radio.FillMode = canvas.ImageFillContain
		if t.Checked {
			r.radio.Resource = formRadioSelected
		}
		r.outline = r.radio
		r.objects = []fyne.CanvasObject{r.background, r.radio}
	} else {
		outline := canvas.NewRectangle(color.White)
		outline.StrokeColor = formToggleInk
		outline.StrokeWidth = 2
		r.outline = outline
		r.first = canvas.NewLine(formToggleInk)
		r.second = canvas.NewLine(formToggleInk)
		r.objects = []fyne.CanvasObject{r.background, outline, r.first, r.second}
	}
	if !t.Checked {
		for _, mark := range r.objects[2:] {
			mark.Hide()
		}
	}
	return r
}

type formToggleRenderer struct {
	toggle        *formToggle
	background    *canvas.Rectangle
	outline       fyne.CanvasObject
	radio         *canvas.Image
	first, second *canvas.Line
	objects       []fyne.CanvasObject
}

func (r *formToggleRenderer) Destroy()                     {}
func (r *formToggleRenderer) MinSize() fyne.Size           { return fyne.NewSquareSize(18) }
func (r *formToggleRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *formToggleRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	side := max(float32(1), min(size.Width, size.Height)-4)
	pos := fyne.NewPos((size.Width-side)/2, (size.Height-side)/2)
	r.outline.Move(pos)
	r.outline.Resize(fyne.NewSquareSize(side))
	if r.radio == nil {
		r.first.Position1 = pos.Add(fyne.NewPos(side*.18, side*.52))
		r.first.Position2 = pos.Add(fyne.NewPos(side*.42, side*.76))
		r.second.Position1 = r.first.Position2
		r.second.Position2 = pos.Add(fyne.NewPos(side*.84, side*.23))
		r.first.StrokeWidth = max(float32(1.5), side*.12)
		r.second.StrokeWidth = r.first.StrokeWidth
	}
}
func (r *formToggleRenderer) Refresh() {
	r.background.FillColor = color.White
	if r.toggle.hovered || r.toggle.focused {
		r.background.FillColor = color.NRGBA{R: 0xd6, G: 0xe8, B: 0xfa, A: 255}
	}
	if r.toggle.inList {
		// List rows stretch this widget vertically. Keep their background visible
		// and apply the light face/interaction color only inside the checkbox.
		if outline, ok := r.outline.(*canvas.Rectangle); ok {
			outline.FillColor = r.background.FillColor
		}
		r.background.FillColor = color.Transparent
	}
	if r.radio != nil {
		if r.toggle.Checked {
			r.radio.Resource = formRadioSelected
		} else {
			r.radio.Resource = formRadioEmpty
		}
	} else {
		if r.toggle.Checked {
			r.first.Show()
			r.second.Show()
		} else {
			r.first.Hide()
			r.second.Hide()
		}
	}
	r.Layout(r.toggle.Size())
	for _, obj := range r.objects {
		obj.Refresh()
	}
}
