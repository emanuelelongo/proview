package gui

import (
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"emanuelelongo/proview/pkg/signatures"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func waitSignatureUI(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ok := false
		fyne.DoAndWait(func() { ok = ready() })
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("signature UI did not finish loading")
}

func TestSignatureDropdownInsertsWithOneClickAndHasNoButtons(t *testing.T) {
	g, _ := annotationTestGui(t)
	g.image = canvas.NewImageFromImage(image.NewNRGBA(image.Rect(0, 0, 1000, 700)))
	g.window.Resize(fyne.NewSize(1000, 700))
	g.window.Show()
	store := &signatures.Store{Root: t.TempDir()}
	asset, err := signatures.Import("sample.svg", []byte(`<svg viewBox="0 0 300 80"><path d="M10 60Q40 5 60 55T110 30Q120 70 160 25T250 60" fill="none" stroke="black" stroke-width="3"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(signatures.Entry{Asset: asset}); err != nil {
		t.Fatal(err)
	}
	g.signatureStore = store
	g.promptSignature()
	popup := g.signaturePopup
	if popup == nil || g.signatureDialog != nil {
		t.Fatal("toolbar opened management dialog instead of dropdown")
	}
	content := popup.Content.(*container.Scroll).Content.(*fyne.Container)
	waitSignatureUI(t, func() bool { return len(content.Objects) == 3 })
	first, ok := content.Objects[0].(*signatureChoice)
	if !ok {
		t.Fatal("signature preview is not clickable")
	}
	if popup.Content.Size().Height < content.MinSize().Height {
		t.Fatal("dropdown clips the management row")
	}
	manage := content.Objects[2].(*signatureChoice)
	manage.MouseIn(&desktop.MouseEvent{})
	for _, object := range content.Objects {
		if _, ok := object.(*widget.Button); ok {
			t.Fatal("dropdown contains an action button")
		}
	}
	if path := os.Getenv("PROVIEW_SIGNATURE_PICKER_PREVIEW"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, g.window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}
	fyneTest.Tap(first)
	waitSignatureUI(t, func() bool { return !g.working && len(g.document.Overlays()) == 1 })
	if popup.Visible() || g.signaturePopup != nil || !g.hasSelectedSignature {
		t.Fatal("one-click insertion did not close picker and select signature")
	}
	g.promptSignature()
	if !g.closeSignatureDialogOnEscape() || g.signaturePopup != nil {
		t.Fatal("Escape did not dismiss dropdown")
	}
}

func TestSignatureManagementRowsAndCloudReturn(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.window.Resize(fyne.NewSize(1000, 700))
	g.window.Show()
	store := &signatures.Store{Root: t.TempDir()}
	asset, err := signatures.Import("sample.svg", []byte(`<svg viewBox="0 0 100 30"><path d="M5 20L95 10" stroke="black"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(signatures.Entry{Asset: asset}); err != nil {
		t.Fatal(err)
	}
	service := testS3Service(app, store, &testS3Credentials{})
	service.restored = true
	g.signatureStore = store
	g.signatureCloud = service
	g.promptSignature()
	popup := g.signaturePopup
	content := popup.Content.(*container.Scroll).Content.(*fyne.Container)
	waitSignatureUI(t, func() bool { return len(content.Objects) == 3 })
	fyneTest.Tap(content.Objects[2].(*signatureChoice))
	library := g.signatureDialog
	if library == nil {
		t.Fatal("management entry did not open library")
	}
	var row *fyne.Container
	var cloudButton *widget.Button
	var visit func(fyne.CanvasObject)
	visit = func(object fyne.CanvasObject) {
		switch object := object.(type) {
		case *fyne.Container:
			if _, ok := object.Layout.(signatureLibraryRowLayout); ok {
				row = object
			}
			for _, child := range object.Objects {
				visit(child)
			}
		case *container.Scroll:
			visit(object.Content)
		case *widget.Button:
			if object.Text == "Cloud sync" {
				cloudButton = object
			}
			if object.Text == "Insert" || object.Text == "Rename" || object.Text == "Edit" || object.Text == "Delete" {
				t.Fatal("obsolete row action remains")
			}
		}
	}
	waitSignatureUI(t, func() bool { visit(g.window.Canvas().Overlays().Top().(*widget.PopUp).Content); return row != nil })
	remove, ok := row.Objects[1].(*widget.Button)
	if !ok || remove.Importance != widget.DangerImportance || remove.Icon == nil || row.Objects[0].Size().Height != remove.Size().Height || remove.Position().X != row.Objects[0].Size().Width {
		t.Fatal("trash action is not red, attached and full height")
	}
	if cloudButton == nil {
		t.Fatal("missing cloud settings action")
	}
	fyneTest.Tap(cloudButton)
	settings := g.signatureDialog
	if settings == nil || settings == library {
		t.Fatal("cloud settings did not open")
	}
	if !g.closeSignatureDialogOnEscape() || g.signatureDialog == nil || g.signatureDialog == settings {
		t.Fatal("closing cloud settings did not return to library")
	}
	g.signatureDialog.Hide()
}
