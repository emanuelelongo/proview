package gui

import (
	"emanuelelongo/proview/pkg/document"
	"image/color"
	"math"
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// A page-sized layer with zero minimum size lets the existing page control zoom.
type formLayer struct {
	widget.BaseWidget
	editor   *Gui
	page     int
	fields   []document.FormField
	controls []fyne.CanvasObject
	updating bool
}

func newFormLayer(g *Gui, page int) *formLayer {
	l := &formLayer{editor: g, page: page}
	l.ExtendBaseWidget(l)
	l.reload()
	return l
}

func (l *formLayer) reload() {
	l.updating = true
	defer func() { l.updating = false }()
	l.fields = nil
	l.controls = nil
	if l.editor.document == nil {
		return
	}
	fields, err := l.editor.document.FormFields(l.page)
	if err != nil {
		l.Hide()
		return
	}
	l.fields = fields
	for _, f := range fields {
		changedValues := func(values []string) {
			if l.updating || l.editor.document == nil {
				return
			}
			if l.editor.working {
				l.reload()
				return
			}
			if err := l.editor.document.SetFormValues(l.page, f.ID, values); err != nil {
				dialog.ShowError(err, l.editor.window)
				return
			}
			l.editor.activatePDFPage(l.page)
			l.editor.syncFormValues()
			l.editor.updateHistoryActions()
		}
		changed := func(value string) { changedValues([]string{value}) }
		switch f.Kind {
		case document.FormText:
			entry := widget.NewEntry()
			if f.Flags&document.FormPassword != 0 {
				entry = widget.NewPasswordEntry()
			}
			entry.Validator = func(value string) error { return document.ValidateFormField(f, []string{value}, true) }
			if f.Flags&document.FormMultiline != 0 {
				entry.MultiLine = true
				entry.Wrapping = fyne.TextWrapWord
			}
			entry.SetText(f.Value)
			entry.OnChanged = func(value string) {
				if f.MaxLen > 0 && len([]rune(value)) > f.MaxLen {
					entry.SetText(string([]rune(value)[:f.MaxLen]))
					return
				}
				changed(value)
			}
			l.controls = append(l.controls, entry)
		case document.FormCheckbox, document.FormRadio:
			check := newFormToggle(f.Kind == document.FormRadio, f.Value != "Off" && f.Value != "" && f.Value == f.OnValue, nil)
			check.OnChanged = func(on bool) {
				value := "Off"
				if on {
					value = f.OnValue
				}
				changed(value)
			}
			l.controls = append(l.controls, check)
		case document.FormReset:
			caption := f.Caption
			if caption == "" {
				caption = "Reset form"
			}
			button := widget.NewButton(caption, func() {
				if l.editor.working {
					return
				}
				if err := l.editor.document.ResetForm(l.page, f.ID); err != nil {
					dialog.ShowError(err, l.editor.window)
					return
				}
				l.editor.syncFormValues()
				l.editor.refreshFormPreviews()
				l.editor.updateHistoryActions()
			})
			l.controls = append(l.controls, button)
		case document.FormList:
			if f.Flags&document.FormMultiSelect != 0 {
				l.controls = append(l.controls, newFormMultiList(f, changedValues))
				break
			}
			list := widget.NewList(func() int { return len(f.Options) }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(id widget.ListItemID, obj fyne.CanvasObject) { obj.(*widget.Label).SetText(f.Options[id]) })
			list.HideSeparators = true
			if i := slices.Index(f.ExportOptions, f.Value); i >= 0 {
				list.Select(i)
			}
			list.OnSelected = func(id widget.ListItemID) { changed(f.ExportOptions[id]) }
			l.controls = append(l.controls, list)
		case document.FormCombo:
			if f.Flags&(1<<18) != 0 {
				entry := widget.NewSelectEntry(f.Options)
				value := f.Value
				if i := slices.Index(f.ExportOptions, value); i >= 0 {
					value = f.Options[i]
				}
				entry.SetText(value)
				entry.OnChanged = func(label string) {
					value := label
					if i := slices.Index(f.Options, label); i >= 0 {
						value = f.ExportOptions[i]
					}
					changed(value)
				}
				l.controls = append(l.controls, entry)
			} else {
				choice := widget.NewSelect(f.Options, func(label string) {
					i := slices.Index(f.Options, label)
					if i >= 0 {
						changed(f.ExportOptions[i])
					}
				})
				if i := slices.Index(f.ExportOptions, f.Value); i >= 0 {
					choice.SetSelected(f.Options[i])
				}
				l.controls = append(l.controls, choice)
			}
		}
	}
	l.Refresh()
	if l.editor.formsActive {
		l.Show()
	} else {
		l.Hide()
	}
}

func (l *formLayer) CreateRenderer() fyne.WidgetRenderer {
	content := container.NewWithoutLayout()
	scaled := &formZoomTheme{scale: 1}
	return &formLayerRenderer{layer: l, content: content, scaled: scaled, themed: container.NewThemeOverride(content, scaled)}
}

// Scale widget typography and spacing with the page, without changing the
// application theme or recreating focused entries during zoom.
type formZoomTheme struct{ scale float32 }

func formBaseTheme() fyne.Theme {
	if app := fyne.CurrentApp(); app != nil && app.Settings().Theme() != nil {
		return app.Settings().Theme()
	}
	return theme.DefaultTheme()
}

func (t *formZoomTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	return formBaseTheme().Color(n, v)
}
func (t *formZoomTheme) Font(s fyne.TextStyle) fyne.Resource     { return formBaseTheme().Font(s) }
func (t *formZoomTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return formBaseTheme().Icon(n) }
func (t *formZoomTheme) Size(n fyne.ThemeSizeName) float32       { return formBaseTheme().Size(n) * t.scale }

type formLayerRenderer struct {
	layer             *formLayer
	content           *fyne.Container
	backgrounds       []*canvas.Rectangle
	validationBorders []*canvas.Rectangle
	scaled            *formZoomTheme
	themed            *container.ThemeOverride
}

func (r *formLayerRenderer) Destroy()                     {}
func (r *formLayerRenderer) MinSize() fyne.Size           { return fyne.NewSize(0, 0) }
func (r *formLayerRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.themed} }
func (r *formLayerRenderer) Layout(size fyne.Size) {
	if r.layer.editor.document != nil {
		if pageSize, err := r.layer.editor.document.PageSize(r.layer.page); err == nil && pageSize.X > 0 && size.Width > 0 {
			scale := size.Width / float32(pageSize.X)
			if r.scaled.scale != scale {
				r.scaled.scale = scale
				r.themed.Refresh()
			}
		}
	}
	r.themed.Resize(size)
	r.content.Resize(size)
	for i, control := range r.layer.controls {
		rect := r.layer.fields[i].Rect
		position := fyne.NewPos(float32(rect.X)*size.Width, float32(rect.Y)*size.Height)
		fieldSize := fyne.NewSize(float32(rect.Width)*size.Width, float32(rect.Height)*size.Height)
		control.Move(position)
		control.Resize(fieldSize)
		if i < len(r.backgrounds) {
			r.backgrounds[i].Move(position)
			r.backgrounds[i].Resize(fieldSize)
			r.validationBorders[i].Move(position)
			r.validationBorders[i].Resize(fieldSize)
		}
	}
}
func (r *formLayerRenderer) Refresh() {
	// Hover and focus colors in Fyne can be translucent. Cover the PDF widget
	// appearance before painting any interactive control, including checkboxes.
	background := theme.ColorForWidget(theme.ColorNameBackground, r.layer)
	if len(r.backgrounds) != len(r.layer.controls) {
		r.backgrounds = make([]*canvas.Rectangle, len(r.layer.controls))
		r.validationBorders = make([]*canvas.Rectangle, len(r.layer.controls))
		for i := range r.backgrounds {
			r.backgrounds[i] = canvas.NewRectangle(background)
			r.validationBorders[i] = canvas.NewRectangle(color.Transparent)
		}
	}
	r.content.Objects = nil
	var fields []document.FormField
	var err error
	if r.layer.editor.document != nil {
		fields, err = r.layer.editor.document.FormFields(r.layer.page)
	}
	for i, control := range r.layer.controls {
		r.backgrounds[i].FillColor = background
		border := r.validationBorders[i]
		border.StrokeWidth = 0
		if err == nil && i < len(fields) && document.ValidateFormField(fields[i], fields[i].Values, true) != nil {
			border.StrokeWidth = 2
			border.StrokeColor = theme.ColorForWidget(theme.ColorNameError, r.layer)
		}
		r.content.Objects = append(r.content.Objects, r.backgrounds[i], control, border)
	}
	r.Layout(r.layer.Size())
	r.content.Refresh()
	r.themed.Refresh()
}

func (g *Gui) reloadFormLayers() {
	for _, l := range g.formLayers {
		l.reload()
	}
}
func (g *Gui) toggleForms() {
	if g.working || g.document == nil || !g.document.HasFormFields() {
		return
	}
	active := !g.formsActive
	g.setAnnotationTool(toolSelect)
	g.setFormsActive(active)
}
func (g *Gui) setFormsActive(active bool) {
	changed := g.formsActive != active
	g.formsActive = active
	if active {
		g.stopFormPulse()
	}
	if g.formAction != nil {
		g.formAction.SetActive(active)
	}
	if changed && g.window != nil {
		g.window.Canvas().Focus(nil)
	}
	g.reloadFormLayers()
	if changed && !active && g.document != nil {
		g.refreshFormPreviews()
	}
}

// Render saved field appearances also when the form controls are hidden.
func (g *Gui) refreshFormPreviews() {
	if g.document == nil || g.document.Format() != document.FormatPDF || !g.document.HasFormFields() {
		return
	}
	doc, revision, generation := g.document, g.document.Revision(), g.pdfGeneration
	snapshot := doc.RenderingSnapshot()
	for page := range g.pdfPageImages {
		if !g.pdfPageLoaded[page] {
			continue
		}
		go func() {
			preview, err := snapshot.RenderBasePage(page)
			thumb, thumbErr := snapshot.RenderPage(page)
			fyne.Do(func() {
				if g.document != doc || doc.Revision() != revision || g.pdfGeneration != generation {
					return
				}
				if err != nil {
					dialog.ShowError(err, g.window)
					return
				}
				g.pdfPageImages[page].Image = preview
				g.pdfPageImages[page].Refresh()
				if thumbErr == nil {
					g.pdfThumbImages[page].Image = thumb
					g.pdfThumbImages[page].Refresh()
				}
			})
		}()
	}
}

// A single PDF field can have widgets on several pages. Keep their controls
// synchronized without replacing the entry that currently has keyboard focus.
func (g *Gui) syncFormValues() {
	for _, l := range g.formLayers {
		fields, err := g.document.FormFields(l.page)
		if err != nil || len(fields) != len(l.controls) {
			continue
		}
		l.updating = true
		for i, f := range fields {
			value := f.Value
			if f.Kind == document.FormCombo || f.Kind == document.FormList {
				if j := slices.Index(f.ExportOptions, value); j >= 0 {
					value = f.Options[j]
				}
			}
			switch control := l.controls[i].(type) {
			case *widget.Entry:
				if control.Text != value {
					control.SetText(value)
				}
			case *widget.SelectEntry:
				if control.Text != value {
					control.SetText(value)
				}
			case *widget.Select:
				if control.Selected != value {
					if slices.Contains(control.Options, value) {
						control.SetSelected(value)
					} else {
						control.ClearSelected()
					}
				}
			case *formMultiList:
				control.SetValues(f.Values)
			case *widget.List:
				if index := slices.Index(f.ExportOptions, f.Value); index >= 0 {
					control.Select(index)
				} else {
					control.UnselectAll()
				}
			case *formToggle:
				control.SetChecked(value != "" && value != "Off" && value == f.OnValue)
			}
		}
		l.updating = false
		l.Refresh()
	}
}

func (g *Gui) stopFormPulse() {
	g.formPulseSerial++
	if g.formPulse != nil {
		g.formPulse.Stop()
		g.formPulse = nil
	}
	if g.formAction != nil && g.formAction.pulseBackground != nil {
		g.formAction.pulseBackground.FillColor = color.Transparent
		g.formAction.pulseBackground.Refresh()
	}
}
func (g *Gui) startFormPulse() {
	g.stopFormPulse()
	if g.formAction == nil || g.formAction.pulseBackground == nil || g.document == nil || !g.document.HasFormFields() {
		return
	}
	doc, serial := g.document, g.formPulseSerial
	g.formPulse = fyne.NewAnimation(2400*time.Millisecond, func(progress float32) {
		if serial != g.formPulseSerial || g.document != doc || g.formsActive || g.closed {
			return
		}
		tint := color.NRGBAModel.Convert(theme.Color(theme.ColorNamePrimary)).(color.NRGBA)
		amplitude := math.Sin(2 * math.Pi * float64(progress))
		tint.A = uint8(90 * amplitude * amplitude)
		g.formAction.pulseBackground.FillColor = tint
		g.formAction.pulseBackground.Refresh()
	})
	g.formPulse.Start()
}
