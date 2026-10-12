package gui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
)

func TestThumbnailTapDoesNotCoverPreviewOnHover(t *testing.T) {
	clicked := false
	tap := newThumbnailTap(func() { clicked = true })
	if _, paintsHover := any(tap).(desktop.Hoverable); paintsHover {
		t.Fatal("thumbnail tap widget paints a hover state")
	}
	objects := tap.CreateRenderer().Objects()
	if len(objects) != 1 {
		t.Fatalf("thumbnail overlay has %d canvas objects, want one transparent object", len(objects))
	}
	background, ok := objects[0].(*canvas.Rectangle)
	if !ok {
		t.Fatalf("thumbnail overlay object is %T, want a rectangle", objects[0])
	}
	if _, _, _, alpha := background.FillColor.RGBA(); alpha != 0 {
		t.Fatalf("thumbnail overlay opacity = %d, want 0", alpha)
	}
	tap.Tapped(&fyne.PointEvent{})
	if !clicked {
		t.Fatal("thumbnail click did not run its action")
	}
}
