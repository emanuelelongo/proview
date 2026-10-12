package gui

import (
	"fyne.io/fyne/v2/dialog"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"testing"
)

func TestSignatureEscapeClosesOnlyTopDialogAndRunsCleanup(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	d := dialog.NewCustom("Signature", "Close", widget.NewLabel("Signature"), g.window)
	closed := false
	g.showSignatureDialog(d, func() { closed = true })
	prompt := dialog.NewCustom("Prompt", "Cancel", widget.NewLabel("Prompt"), g.window)
	prompt.Show()
	if g.closeSignatureDialogOnEscape() || closed {
		t.Fatal("Escape closed signature dialog behind another prompt")
	}
	prompt.Hide()
	if !g.closeSignatureDialogOnEscape() || !closed {
		t.Fatal("Escape did not close signature dialog and run cleanup")
	}
	if g.signatureDialog != nil || g.closeSignatureDialogOnEscape() {
		t.Fatal("closed signature dialog is still tracked")
	}
}
