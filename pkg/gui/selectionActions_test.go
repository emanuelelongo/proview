package gui

import (
	"image/color"
	"net/url"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestSelectedURLAcceptsWebAddressesOnly(t *testing.T) {
	for _, example := range []struct{ text, want string }{
		{"https://example.com/path?q=one&other=two", "https://example.com/path?q=one&other=two"},
		{"http://example.com", "http://example.com"},
		{"www.example.com", "https://www.example.com"},
		{"example.com/path;", "https://example.com/path"},
		{"example.com:8080/path", "https://example.com:8080/path"},
		{"", ""}, {"ordinary text", ""}, {"250.00", ""},
		{"javascript:alert(1)", ""}, {"file:///tmp/file", ""}, {"ftp://example.com", ""},
		{"user@example.com", ""}, {"https://example.com with text", ""},
	} {
		target := selectedURL(example.text)
		got := ""
		if target != nil {
			got = target.String()
		}
		if got != example.want {
			t.Fatalf("%q: got %q, want %q", example.text, got, example.want)
		}
	}
}

type selectionURLApp struct {
	fyne.App
	opened []*url.URL
}

func (a *selectionURLApp) OpenURL(target *url.URL) error {
	a.opened = append(a.opened, target)
	return nil
}
func TestStatusLinkOpensCurrentSelectionAndClearsOnDeselection(t *testing.T) {
	g, l := annotationTestGui(t)
	installTestText(g)
	entry := g.ocr.cache[g.document.TextSelectionKey(0)]
	entry.layout.Words[0].Text = "www.example.com"
	app := &selectionURLApp{App: g.app}
	g.app = app
	g.tipText = canvas.NewText("", color.Black)
	surface := newStatusTextSurface(g)
	l.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 225)})
	if surface.Size() != g.tipText.MinSize() {
		t.Fatal("link hit area did not follow selected text")
	}
	surface.Tapped(&fyne.PointEvent{})
	if g.tipText.Text != "www.example.com" || len(app.opened) != 1 || app.opened[0].String() != "https://www.example.com" {
		t.Fatal("status link did not open the selected address")
	}
	g.clearTextSelection()
	surface.Tapped(&fyne.PointEvent{})
	if len(app.opened) != 1 {
		t.Fatal("deselected URL remained clickable")
	}
	entry.layout.Words[0].Text = "ordinary"
	l.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 225)})
	surface.Tapped(&fyne.PointEvent{})
	if len(app.opened) != 1 {
		t.Fatal("ordinary selection opened a URL")
	}
}
func TestRightClickSelectionCopiesTextAndRejectsObsoleteMenu(t *testing.T) {
	g, l := annotationTestGui(t)
	installTestText(g)
	g.tipText = canvas.NewText("", color.Black)
	l.MouseDown(mouseAt(150, 225))
	l.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(180, 325)}})
	l.MouseUp(mouseAt(180, 325))
	l.TappedSecondary(&fyne.PointEvent{Position: fyne.NewPos(150, 225), AbsolutePosition: fyne.NewPos(150, 225)})
	if g.selectionCopyMenu == nil || len(g.selectionCopyMenu.Items) != 1 {
		t.Fatal("selected words did not offer Copy")
	}
	fyneTest.Tap(g.selectionCopyMenu.Items[0].(fyne.Tappable))
	if g.window.Clipboard().Content() != "Caffè pronto\nSecond" || g.selectionCopyMenu != nil {
		t.Fatal("context copy failed")
	}
	l.TappedSecondary(&fyne.PointEvent{Position: fyne.NewPos(800, 700)})
	if g.selectionCopyMenu != nil {
		t.Fatal("unselected background showed text menu")
	}
	surface := newStatusTextSurface(g)
	surface.TappedSecondary(&fyne.PointEvent{})
	if g.selectionCopyMenu == nil {
		t.Fatal("status text did not offer Copy")
	}
	oldItem := g.selectionCopyMenu.Items[0]
	g.clearTextSelection()
	g.window.Clipboard().SetContent("sentinel")
	fyneTest.Tap(oldItem.(fyne.Tappable))
	if g.window.Clipboard().Content() != "sentinel" {
		t.Fatal("obsolete menu copied previous selection")
	}
	// A valid selection from a different source revision must also be rejected.
	l.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 225)})
	surface.TappedSecondary(&fyne.PointEvent{})
	oldItem = g.selectionCopyMenu.Items[0]
	if err := g.document.TransformImage("right"); err != nil {
		t.Fatal(err)
	}
	fyneTest.Tap(oldItem.(fyne.Tappable))
	if g.window.Clipboard().Content() != "sentinel" {
		t.Fatal("menu copied stale source text")
	}
}
