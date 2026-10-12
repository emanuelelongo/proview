package gui

import (
	"emanuelelongo/proview/pkg/signatures"
	"image"
	"math"
	"reflect"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestSignaturePadCapturesIndependentStrokesAndLogicalCoordinates(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	// Suppress asynchronous preview work: this test exercises mouse acquisition.
	pad := &signaturePad{drawing: signatures.Drawing{Version: 1, Width: 4, Smooth: 25}, editable: true, picture: canvas.NewImageFromImage(nil), timer: time.NewTimer(time.Hour)}
	defer pad.timer.Stop()
	pad.ExtendBaseWidget(pad)
	pad.Resize(fyne.NewSize(600, 270))
	down := func(x, y float32) {
		pad.MouseDown(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(x, y)}, Button: desktop.MouseButtonPrimary})
	}
	down(60, 60)
	pad.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(120, 90)}})
	pad.MouseOut()
	pad.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(180, 120)}})
	if len(pad.drawing.Strokes) != 1 || len(pad.drawing.Strokes[0]) != 2 {
		t.Fatal("leaving the pad connected an unintended stroke")
	}
	pad.Resize(fyne.NewSize(1000, 450))
	down(300, 200)
	pad.MouseUp(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(300, 200)}, Button: desktop.MouseButtonPrimary})
	if len(pad.drawing.Strokes) != 2 || len(pad.drawing.Strokes[1]) != 1 {
		t.Fatal("click did not create a separate dot")
	}
	if p := pad.drawing.Strokes[0][0]; p.X < 99.99 || p.X > 100.01 {
		t.Fatalf("logical coordinate = %v; expected x 100", p)
	}
	if p := pad.drawing.Strokes[1][0]; math.Abs(p.X-300) > 0.0001 || math.Abs(p.Y-200) > 0.0001 {
		t.Fatalf("resized pad coordinate = %v", p)
	}
	before := pad.drawing.Clone()
	pad.editable = false
	down(100, 100)
	if !reflect.DeepEqual(before, pad.drawing) {
		t.Fatal("settings-only editor accepted new strokes")
	}
}

func TestSignatureLibraryEnabledAndDocumentToolsDisabledWithoutOpenDocument(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	if g.openAction.button.Disabled() || g.blankAction.button.Disabled() || g.signatureAction.button.Disabled() {
		t.Fatal("Open, Blank Page and Signature should remain enabled without a document")
	}
	if !g.cropAction.button.Disabled() ||
		!g.zoomInAction.button.Disabled() || !g.zoomOutAction.button.Disabled() {
		t.Fatal("document tools should be disabled without a document")
	}
	for _, action := range g.annotationActions {
		if !action.button.Disabled() {
			t.Fatal("annotation tool should be disabled without a document")
		}
	}
	var signaturesMenuFound bool
	for _, menu := range g.window.MainMenu().Items {
		if menu.Label == "Signatures" {
			signaturesMenuFound = true
		}
		if menu.Label == "Tools" {
			for _, item := range menu.Items {
				if item.Label == "Signature" {
					if item.Disabled {
						t.Fatal("signature library menu should remain enabled without a document")
					}
					continue
				}
				if !item.Disabled {
					t.Fatalf("tool %q should be disabled without a document", item.Label)
				}
			}
		}
	}
	if signaturesMenuFound {
		t.Fatal("Signatures should not be a top-level menu")
	}
}

func TestSignaturePreviewUpdatesWhileMouseKeepsMoving(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	pad := &signaturePad{active: true, generation: 10, picture: canvas.NewImageFromImage(nil)}
	frame := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	// The mouse has already added more points than this rendered snapshot.
	pad.applyPreview(drawingRequest{generation: 5}, frame, nil)
	if pad.picture.Image != frame {
		t.Fatal("live preview waited for the mouse to stop moving")
	}
	older := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	pad.applyPreview(drawingRequest{generation: 4}, older, nil)
	if pad.picture.Image != frame {
		t.Fatal("older frame replaced a newer preview")
	}
	// Clearing or changing settings invalidates frames still being rendered.
	pad.revision++
	pad.applyPreview(drawingRequest{generation: 9}, older, nil)
	if pad.picture.Image != frame {
		t.Fatal("frame from before an edit replaced the preview")
	}
	pad.applyPreview(drawingRequest{generation: 10, revision: pad.revision}, older, nil)
	if pad.picture.Image != older {
		t.Fatal("preview after an edit was discarded")
	}
}
