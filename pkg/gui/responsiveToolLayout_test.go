package gui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"image/color"
)

func TestResponsiveToolLayoutMovesWholePropertyBlock(t *testing.T) {
	toolbar := canvas.NewRectangle(color.Transparent)
	toolbar.SetMinSize(fyne.NewSize(600, 40))
	control := canvas.NewRectangle(color.Transparent)
	control.SetMinSize(fyne.NewSize(200, 36))
	controls := container.NewHBox(control)
	properties := container.NewHScroll(controls)
	properties.SetMinSize(fyne.NewSize(0, 50))
	separator := canvas.NewRectangle(color.Transparent)
	separator.SetMinSize(fyne.NewSize(1, 1))
	layout := newResponsiveToolLayout(toolbar, properties, controls, separator)
	objects := []fyne.CanvasObject{toolbar, separator, properties}

	layout.Layout(objects, fyne.NewSize(900, 100))
	if !layout.inline || properties.Position().Y != 0 || separator.Size().Width != 1 {
		t.Fatalf("wide layout did not place properties inline: inline=%v position=%v separator=%v", layout.inline, properties.Position(), separator.Size())
	}
	if separator.Position().X <= toolbar.Size().Width || properties.Position().X <= separator.Position().X+separator.Size().Width {
		t.Fatalf("inline separator has no surrounding space: toolbar=%v separator=%v properties=%v", toolbar.Size(), separator.Position(), properties.Position())
	}
	if separator.Position().Y != 0 || separator.Size().Height != toolbar.Size().Height {
		t.Fatalf("inline separator does not match toolbar height: toolbar=%v separator position=%v size=%v", toolbar.Size(), separator.Position(), separator.Size())
	}
	if got := layout.MinSize(objects).Height; got != 40 {
		t.Fatalf("inline panel height = %v, want toolbar height 40", got)
	}

	layout.Layout(objects, fyne.NewSize(750, 100))
	if layout.inline || properties.Position().Y <= 40 || separator.Size() != (fyne.Size{}) {
		t.Fatalf("narrow layout did not move the complete block down: inline=%v position=%v separator=%v", layout.inline, properties.Position(), separator.Size())
	}
	if got := layout.MinSize(objects).Height; got <= 90 {
		t.Fatalf("stacked panel height = %v", got)
	}
}
