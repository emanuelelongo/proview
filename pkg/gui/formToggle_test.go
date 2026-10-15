package gui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestFormButtonsDistinguishableAndReadableAcrossThemes(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	window := app.NewWindow("PDF buttons")
	defer window.Close()
	for _, th := range []fyne.Theme{theme.LightTheme(), theme.DarkTheme()} {
		app.Settings().SetTheme(th)
		check := newFormToggle(false, true, nil)
		radio := newFormToggle(true, true, nil)
		content := container.NewWithoutLayout(check, radio)
		window.SetContent(content)
		window.Resize(fyne.NewSize(100, 60))
		check.Move(fyne.NewPos(5, 5))
		check.Resize(fyne.NewSquareSize(40))
		radio.Move(fyne.NewPos(55, 5))
		radio.Resize(fyne.NewSquareSize(40))
		cr := fyneTest.WidgetRenderer(check).(*formToggleRenderer)
		rr := fyneTest.WidgetRenderer(radio).(*formToggleRenderer)
		if _, ok := cr.outline.(*canvas.Rectangle); !ok {
			t.Fatal("checkbox is not square")
		}
		if rr.radio == nil {
			t.Fatal("radio is missing its concentric icon")
		}
		capture := window.Canvas().Capture()
		cp := app.Driver().AbsolutePositionForObject(check)
		rp := app.Driver().AbsolutePositionForObject(radio)
		paper := color.NRGBAModel.Convert(capture.At(int(cp.X+15), int(cp.Y+12))).(color.NRGBA)
		mark := color.NRGBAModel.Convert(capture.At(int(rp.X+20), int(rp.Y+20))).(color.NRGBA)
		if paper.R < 240 || paper.G < 240 || paper.B < 240 {
			t.Fatalf("form face follows dark theme: %v", paper)
		}
		if mark.R > 70 || mark.G > 90 || mark.B > 110 {
			t.Fatalf("selected radio has low contrast: %v", mark)
		}
		initial := newFormToggle(false, false, nil)
		initialRenderer := fyneTest.WidgetRenderer(initial).(*formToggleRenderer)
		if initialRenderer.first.Visible() || initialRenderer.second.Visible() {
			t.Fatal("unchecked checkbox initially shows a check mark")
		}
		check.SetChecked(false)
		fyneTest.Tap(check)
		if !check.Checked {
			t.Fatal("checkbox cannot be checked")
		}
		fyneTest.Tap(check)
		if check.Checked {
			t.Fatal("checkbox cannot be cleared")
		}
		changes := 0
		radio.OnChanged = func(bool) { changes++ }
		fyneTest.Tap(radio)
		radio.TypedRune(' ')
		if !radio.Checked || changes != 0 {
			t.Fatal("selected radio can be deselected")
		}
		radio.SetChecked(false)
		radio.TypedRune(' ')
		if !radio.Checked || changes != 2 {
			t.Fatal("radio cannot be selected by keyboard")
		}
	}
}
