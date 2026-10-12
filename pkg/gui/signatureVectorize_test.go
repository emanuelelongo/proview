package gui

import (
	"image"
	"image/color"
	"math"
	"testing"

	"emanuelelongo/proview/pkg/signatures"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSignatureScanLiveCropPreservesAspectAndUpdatesWithoutConfirmation(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	original := image.NewNRGBA(image.Rect(0, 0, 600, 400))
	original.SetNRGBA(350, 220, color.NRGBA{R: 240, A: 255})
	view := newSignatureScanView(original)
	var selection image.Rectangle
	changes := 0
	view.crop.ActivateLive(original.Bounds(), func(rect image.Rectangle) { selection = rect; changes++ })
	view.Resize(fyne.NewSize(640, 240))
	renderer := fyneTest.WidgetRenderer(view)
	renderer.Layout(view.Size())
	if math.Abs(float64(view.picture.Size().Width-360))+math.Abs(float64(view.picture.Size().Height-240))+math.Abs(float64(view.picture.Position().X-140))+math.Abs(float64(view.picture.Position().Y)) > 0.001 {
		t.Fatalf("scan was stretched: %v at %v", view.picture.Size(), view.picture.Position())
	}
	if !view.crop.rect.Visible() || !view.crop.hasSelection || view.crop.confirm.Visible() {
		t.Fatal("initial live crop is not visible without a confirmation button")
	}
	// Drag inside the initial full-image selection to choose a new region.
	view.crop.beginDrag(fyne.NewPos(120, 60))
	view.crop.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(240, 180)}, Dragged: fyne.NewDelta(120, 120)})
	if selection != image.Rect(200, 100, 400, 300) || changes != 1 {
		t.Fatalf("drag did not update the crop immediately: %v, %d", selection, changes)
	}
	view.crop.DragEnd()
	if !view.crop.active || view.crop.confirm.Visible() {
		t.Fatal("live crop required confirmation or disappeared")
	}
	if view.picture.Image.Bounds() != original.Bounds() {
		t.Fatal("selecting crop replaced the displayed original")
	}
	// Moving the selected rectangle updates the source region, not a nested crop.
	view.crop.beginDrag(fyne.NewPos(170, 100))
	view.crop.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(200, 100)}, Dragged: fyne.NewDelta(30, 0)})
	view.crop.DragEnd()
	if selection != image.Rect(250, 100, 450, 300) {
		t.Fatalf("moving crop maps to %v", selection)
	}
	previous := selection
	previousChanges := changes
	view.Resize(fyne.NewSize(800, 400))
	renderer.Layout(view.Size())
	if selection != previous || view.crop.liveArea != previous || changes != previousChanges {
		t.Fatal("resizing the editor changed source coordinates")
	}
	// The handles also publish changes without pressing Crop.
	handle := view.crop.handles[3]
	handle.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(40, 0)})
	if selection.Dx() <= previous.Dx() {
		t.Fatal("resizing through a handle did not update selection")
	}
	if view.crop.confirm.Visible() {
		t.Fatal("live selection displayed Crop button")
	}
}

func TestOrdinaryCropStillRequiresExplicitConfirmation(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	overlay := newCropOverlay()
	overlay.Resize(fyne.NewSize(300, 200))
	calls := 0
	overlay.Activate(image.Rect(0, 0, 600, 400), func(image.Rectangle) { calls++ })
	overlay.beginDrag(fyne.NewPos(30, 30))
	overlay.finishDrag(fyne.NewPos(200, 150))
	if calls != 0 || !overlay.confirm.Visible() {
		t.Fatal("ordinary document crop lost its confirmation")
	}
	overlay.applySelection()
	if calls != 1 || overlay.active {
		t.Fatal("ordinary crop did not commit exactly once")
	}
}

func TestSignatureScanEditorCanCloseWhilePreviewIsPending(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.editSignatureScan(signatures.Scan{Image: image.NewNRGBA(image.Rect(0, 0, 100, 50))}, nil)
	editor := g.signatureDialog
	if editor == nil {
		t.Fatal("signature editor was not tracked")
	}
	editor.Hide()
	editor.Hide() // Save/window-close callbacks can hide an already closed dialog.
	if g.signatureDialog != nil {
		t.Fatal("closed editor remains active")
	}
}

func TestSignatureImportPanelsAndFooterHaveEqualSizes(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.window.Resize(fyne.NewSize(1000, 700))
	g.editSignatureScan(signatures.Scan{Image: image.NewNRGBA(image.Rect(0, 0, 600, 400))}, nil)
	defer g.signatureDialog.Hide()
	popup := g.window.Canvas().Overlays().Top().(*widget.PopUp)
	var source *signatureScanView
	var preview *canvas.Image
	buttons := map[string]*widget.Button{}
	var visit func(fyne.CanvasObject)
	visit = func(object fyne.CanvasObject) {
		switch object := object.(type) {
		case *fyne.Container:
			for _, child := range object.Objects {
				visit(child)
			}
		case *signatureScanView:
			source = object
		case *canvas.Image:
			preview = object
		case *widget.Button:
			buttons[object.Text] = object
		}
	}
	visit(popup.Content)
	if source == nil || preview == nil {
		t.Fatal("missing source or preview panel")
	}
	if source.Size() != preview.Size() {
		t.Fatalf("unequal panels: %v, %v", source.Size(), preview.Size())
	}
	cancel, save := buttons["Cancel"], buttons["Save"]
	if cancel == nil || save == nil {
		t.Fatal("missing footer buttons")
	}
	if cancel.Size() != save.Size() || cancel.Position().Y != save.Position().Y {
		t.Fatalf("unequal footer buttons: %v at %v, %v at %v", cancel.Size(), cancel.Position(), save.Size(), save.Position())
	}
}
