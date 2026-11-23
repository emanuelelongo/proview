package gui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestProviewThemeProvidesModernPaletteAndGeometry(t *testing.T) {
	th := newProviewTheme(theme.DefaultTheme())
	for _, variant := range []fyne.ThemeVariant{theme.VariantDark, theme.VariantLight} {
		bg := th.Color(theme.ColorNameBackground, variant)
		chrome := th.Color(theme.ColorNameHeaderBackground, variant)
		if bg == chrome {
			t.Fatalf("variant %v: chrome surface matches canvas background", variant)
		}
		if th.Color(theme.ColorNamePrimary, variant) == nil {
			t.Fatalf("variant %v: missing primary color", variant)
		}
	}
	if th.Size(theme.SizeNameInputRadius) < 6 {
		t.Fatalf("input radius = %v, want rounded corners", th.Size(theme.SizeNameInputRadius))
	}
	if th.Size(theme.SizeNameSeparatorThickness) != 1 {
		t.Fatalf("separator thickness = %v, want 1", th.Size(theme.SizeNameSeparatorThickness))
	}
}

func TestChromePanelAddsBorderThicknessToMinSize(t *testing.T) {
	inner := canvas.NewRectangle(theme.Color(theme.ColorNameBackground))
	inner.SetMinSize(fyne.NewSize(100, 40))
	panel := newChromePanel(inner, chromeBorderTop|chromeBorderLeft)
	got := panel.MinSize()
	if got.Width != 101 || got.Height != 41 {
		t.Fatalf("chromePanel MinSize = %v, want 101x41", got)
	}
}

func TestEmptyStateTracksOpenContent(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	if !g.emptyState.Visible() {
		t.Fatal("empty state should be visible with no open document")
	}
	doc := testDocument(t, 120, 80)
	defer doc.Close()
	g.document = doc
	g.updateHistoryActions()
	if g.emptyState.Visible() {
		t.Fatal("empty state should hide when a document is open")
	}
	g.document = nil
	g.updateHistoryActions()
	if !g.emptyState.Visible() {
		t.Fatal("empty state should reappear after closing the document")
	}
}
