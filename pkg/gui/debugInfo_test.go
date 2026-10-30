package gui

import (
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/storage"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestDebugInfoToggleAndSelection(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	defer g.stopDebugInfo()
	defer g.stopTipSequence()
	g.startTipSequence()
	oldGeneration := g.tipGeneration
	g.debugItem.Action()
	if !g.debugEnabled || !g.debugItem.Checked || g.tipsEnabled || g.tipsItem.Checked || g.tipTimer != nil || g.debugTimer == nil {
		t.Fatal("debug mode did not replace tips")
	}
	for _, label := range []string{"CPU Go:", "RAM Go:", "Goroutines:"} {
		if strings.Contains(g.tipText.Text, label) {
			t.Fatalf("unexpected label %s in %q", label, g.tipText.Text)
		}
	}
	text := g.tipText.Text
	g.showNextTip(oldGeneration, false)
	if g.tipText.Text != text {
		t.Fatal("stale tip replaced debug info")
	}
	next := newGui(app)
	defer next.stopDebugInfo()
	if !next.debugEnabled || next.tipsEnabled || next.debugTimer == nil {
		t.Fatal("debug preference not restored")
	}
	g.selectionStatusActive = true
	g.tipText.Text = "selected text"
	g.updateDebugInfo()
	if g.tipText.Text != "selected text" {
		t.Fatal("debug replaced selection")
	}
	g.clearTextSelectionStatus()
	if !strings.Contains(g.tipText.Text, "MiB") {
		t.Fatal("debug did not resume after selection")
	}
	g.tipsItem.Action()
	if g.debugEnabled || g.debugItem.Checked || g.debugTimer != nil || !g.tipsEnabled || g.tipTimer == nil || app.Preferences().Bool(debugPreferenceKey) {
		t.Fatal("tips did not disable debug mode")
	}
}

func TestFileInfoCopyActions(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.window = textClipboardWindow{g.window, fyneTest.NewClipboard()}
	if g.fileInfoMenu() != nil {
		t.Fatal("empty editor has a file menu")
	}
	g.showDocument(testDocument(t, 100, 50))
	menu := g.fileInfoMenu()
	if !menu.Items[1].Disabled {
		t.Fatal("unsaved content has a full path")
	}
	path := filepath.Join(t.TempDir(), "file with spaces.png")
	g.saveTarget = storage.NewFileURI(path)
	menu = g.fileInfoMenu()
	menu.Items[0].Action()
	if got := g.window.Clipboard().Content(); got != filepath.Base(path) {
		t.Fatalf("filename = %q", got)
	}
	menu.Items[1].Action()
	if got := g.window.Clipboard().Content(); got != path {
		t.Fatalf("full path = %q", got)
	}
}

func TestModelFileInfoRetainsPath(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	defer g.clearModel()
	g.window = textClipboardWindow{g.window, fyneTest.NewClipboard()}
	path := filepath.Join(t.TempDir(), "model.stl")
	g.addDocument(nil, testSTLMesh(t), storage.NewFileURI(path))
	g.captureDocument()
	menu := g.fileInfoMenu()
	if menu.Items[1].Disabled {
		t.Fatal("model lost its source path")
	}
	menu.Items[1].Action()
	if got := g.window.Clipboard().Content(); got != path {
		t.Fatalf("model path = %q", got)
	}
}

func TestDebugCPUStaysVisibleBetweenCounterUpdates(t *testing.T) {
	var sample debugCPUSample
	for _, counters := range [][3]float64{{100, 75, 25}, {100, 75, 25}, {200, 125, 50}, {200, 125, 50}} {
		if got := sample.update(counters[0], counters[1]); got != counters[2] {
			t.Fatalf("CPU = %v, want %v", got, counters[2])
		}
	}
}

func TestDebugTooltip(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	defer g.stopDebugInfo()
	g.setDebugEnabled(true)
	g.window.Show()
	surface := g.window.Content().(*tooltipSurface)
	g.tipSurface.MouseIn(&desktop.MouseEvent{})
	if !surface.bubble.Visible() || surface.label.Text != "CPU - RAM - Routines" {
		t.Fatal("missing debug labels on hover")
	}
	if g.window.Canvas().Overlays().Top() != nil {
		t.Fatal("tooltip blocks input")
	}
	g.tipSurface.MouseOut()
	if surface.bubble.Visible() {
		t.Fatal("tooltip survived mouse exit")
	}
	g.tipSurface.MouseIn(&desktop.MouseEvent{})
	g.setDebugEnabled(false)
	if surface.bubble.Visible() {
		t.Fatal("tooltip survived disabling debug")
	}
}
