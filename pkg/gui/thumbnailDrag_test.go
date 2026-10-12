package gui

import (
	"bytes"
	"emanuelelongo/proview/pkg/document"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestThumbnailFastDragKeepsPressedPageAndUsesReleasePosition(t *testing.T) {
	g := thumbnailDragTestGui(t, 4)
	origin := g.app.Driver().AbsolutePositionForObject(g.sidebar.container)
	g.pressThumbnail(0, g.document, 1, &desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	// The driver starts the drag on another card after a fast initial move.
	g.dragThumbnail(1, g.document, 1, &fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: origin.Add(fyne.NewPos(50, 200))}})
	state := g.thumbnailDrag
	if state == nil || state.from != 0 {
		t.Fatal("fast drag did not retain the page pressed initially")
	}
	if g.thumbnailCursor() != desktop.VResizeCursor {
		t.Fatal("active drag did not change cursor")
	}
	state.timer.Stop()
	state.timer = time.NewTimer(time.Hour)
	g.releaseThumbnail(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary, PointEvent: fyne.PointEvent{AbsolutePosition: origin.Add(fyne.NewPos(50, 20))}})
	if state.insertion != 0 || !state.inside || g.thumbnailPress != nil {
		t.Fatal("drop did not use final mouse release position")
	}
	g.cancelThumbnailDrag()
	if g.thumbnailCursor() != desktop.PointerCursor {
		t.Fatal("cancel did not restore normal cursor")
	}
}

func thumbnailDragTestGui(t *testing.T, count int) *Gui {
	t.Helper()
	app := fyneTest.NewApp()
	t.Cleanup(app.Quit)
	source, err := document.Load("pages.pdf", bytes.NewReader(scrollTestPDF(count)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.Close)
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	g := &Gui{app: app, window: app.NewWindow("Drag test"), document: doc, pdfGeneration: 1, thumbList: container.NewVBox()}
	for range count {
		artwork := image.NewNRGBA(image.Rect(0, 0, 110, 180))
		draw.Draw(artwork, artwork.Bounds(), &image.Uniform{C: color.NRGBA{R: 30, G: 90, B: 150, A: 255}}, image.Point{}, draw.Src)
		preview := canvas.NewImageFromImage(artwork)
		g.pdfThumbImages = append(g.pdfThumbImages, preview)
		preview.SetMinSize(fyne.NewSize(110, 180))
		marker := newThumbnailDropIndicator()
		g.pdfThumbDropIndicators = append(g.pdfThumbDropIndicators, marker)
		g.thumbList.Add(container.NewStack(preview, marker))
	}
	g.sidebar = NewSidebar(g)
	g.sidebar.container.Show()
	g.window.SetContent(g.sidebar.container)
	g.window.Resize(fyne.NewSize(160, 300))
	g.window.Show()
	t.Cleanup(func() { g.cancelThumbnailDrag() })
	return g
}

func TestThumbnailMovePreservesObjectsAndIsOneUndoStep(t *testing.T) {
	g := thumbnailDragTestGui(t, 4)
	overlay, err := g.document.AddSignature(1, document.NormalizedRect{X: .1, Y: .1, Width: .2, Height: .1}, testSignature(t, 2, 1))
	if err != nil {
		t.Fatal(err)
	}
	ids := g.document.PageIDs()
	state := &thumbnailDragState{document: g.document, generation: 1, from: 1, insertion: 4, inside: true}
	to, moved, err := g.applyThumbnailMove(state)
	if err != nil || !moved || to != 3 {
		t.Fatalf("drop result=%d,%v,%v", to, moved, err)
	}
	if !slices.Equal(g.document.PageIDs(), []uint64{ids[0], ids[2], ids[3], ids[1]}) {
		t.Fatal("downward drop produced wrong page order")
	}
	if objects := g.document.Overlays(); len(objects) != 1 || objects[0].ID != overlay.ID || objects[0].Page != 3 {
		t.Fatal("drop lost or misplaced page objects")
	}
	if !g.document.Undo() || !slices.Equal(g.document.PageIDs(), ids) || g.document.Overlays()[0].Page != 1 {
		t.Fatal("drop was not restored by one Undo")
	}
	state.from = 3
	state.insertion = 0
	to, moved, err = g.applyThumbnailMove(state)
	if err != nil || !moved || to != 0 || !slices.Equal(g.document.PageIDs(), []uint64{ids[3], ids[0], ids[1], ids[2]}) {
		t.Fatal("upward drop produced wrong page order")
	}
}

func TestThumbnailNoOpAndStaleOrOutsideDropsDoNotEditDocument(t *testing.T) {
	g := thumbnailDragTestGui(t, 4)
	ids := g.document.PageIDs()
	revision := g.document.Revision()
	for _, state := range []*thumbnailDragState{
		{document: g.document, generation: 1, from: 1, insertion: 1, inside: true},
		{document: g.document, generation: 1, from: 1, insertion: 2, inside: true},
		{document: g.document, generation: 0, from: 1, insertion: 4, inside: true},
		{document: g.document, generation: 1, from: 1, insertion: 4, inside: false},
	} {
		if _, moved, err := g.applyThumbnailMove(state); moved || err != nil {
			t.Fatal("invalid/no-op drop changed document")
		}
	}
	if !slices.Equal(g.document.PageIDs(), ids) || g.document.Revision() != revision {
		t.Fatal("cancelled drops created history")
	}
}

func TestThumbnailDragShowsInsertionAutoScrollsAndEscapeCancels(t *testing.T) {
	g := thumbnailDragTestGui(t, 8)
	state := &thumbnailDragState{document: g.document, generation: 1, from: 0, timer: time.NewTimer(time.Hour)}
	g.thumbnailDrag = state
	origin := g.app.Driver().AbsolutePositionForObject(g.sidebar.container)
	tap := newThumbnailTap(nil)
	tap.onDrag = func(event *fyne.DragEvent) { g.dragThumbnail(0, g.document, 1, event) }
	tap.onDragEnd = g.finishThumbnailDrag
	tap.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: origin.Add(fyne.NewPos(30, 200))}})
	if !state.inside || state.insertion != 1 || !g.pdfThumbDropIndicators[1].Visible() {
		t.Fatal("drag did not show expected insertion gap")
	}
	if g.window.Canvas().Overlays().Top() != nil {
		t.Fatal("drag indicator blocks input")
	}
	tap.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: origin.Add(fyne.NewPos(30, g.sidebar.container.Size().Height-2))}})
	if !g.scrollThumbnailDrag(state) || g.sidebar.container.Offset.Y <= 0 {
		t.Fatal("dragging at lower edge did not scroll")
	}
	// Keep the pointer fixed while scrolling: the insertion gap must advance.
	first := state.insertion
	for range 20 {
		g.scrollThumbnailDrag(state)
	}
	if state.insertion <= first {
		t.Fatal("insertion did not follow sidebar auto-scroll")
	}
	if !g.cancelCurrentOperation() || g.thumbnailDrag != nil {
		t.Fatal("Escape did not cancel drag")
	}
	tap.DragEnd()
	if g.document.CanUndo() {
		t.Fatal("Escape followed by release committed a reorder")
	}
	for _, marker := range g.pdfThumbDropIndicators {
		if marker.Visible() {
			t.Fatal("Escape left insertion marker visible")
		}
	}
}

func TestThumbnailReleaseOutsideCancelsAndFinalGapIsReachable(t *testing.T) {
	g := thumbnailDragTestGui(t, 3)
	state := &thumbnailDragState{document: g.document, generation: 1, from: 0, pointer: fyne.NewPos(30, 100), timer: time.NewTimer(time.Hour)}
	g.thumbnailDrag = state
	g.sidebar.container.ScrollToBottom()
	state.pointer.Y = g.sidebar.container.Size().Height - 2
	g.updateThumbnailDrop(state)
	if state.insertion != 3 || !g.pdfThumbDropIndicators[2].Visible() || !g.pdfThumbDropIndicators[2].bottom {
		t.Fatal("last insertion gap is not reachable")
	}
	state.pointer.X = g.sidebar.container.Size().Width + 20
	g.updateThumbnailDrop(state)
	g.finishThumbnailDrag()
	if g.thumbnailDrag != nil || g.document.CanUndo() {
		t.Fatal("release outside sidebar committed reorder")
	}
}

func TestThumbnailDragFeedbackActuallyRendersAndRestoresPreview(t *testing.T) {
	g := thumbnailDragTestGui(t, 3)
	before := g.window.Canvas().Capture()
	origin := g.app.Driver().AbsolutePositionForObject(g.sidebar.container)
	g.dragThumbnail(0, g.document, 1, &fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: origin.Add(fyne.NewPos(30, 200))}})
	state := g.thumbnailDrag
	state.timer.Stop()
	state.timer = time.NewTimer(time.Hour)
	marker := g.pdfThumbDropIndicators[1]
	if marker.Size().Width <= 0 {
		t.Fatal("visible insertion marker has zero width")
	}
	during := g.window.Canvas().Capture()
	if path := os.Getenv("PROVIEW_DRAG_FEEDBACK_PREVIEW"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, during); err != nil {
			t.Fatal(err)
		}
	}
	inset, _ := g.window.Canvas().InteractiveArea()
	markerOrigin := g.app.Driver().AbsolutePositionForObject(marker).Add(inset)
	x, y := int(markerOrigin.X+marker.Size().Width/2), int(markerOrigin.Y+2)
	want := color.NRGBAModel.Convert(theme.PrimaryColor()).(color.NRGBA)
	got := color.NRGBAModel.Convert(during.At(x, y)).(color.NRGBA)
	if got != want {
		t.Fatalf("insertion line is not painted: pixel=%v, want=%v", got, want)
	}
	previewOrigin := g.app.Driver().AbsolutePositionForObject(g.pdfThumbImages[0]).Add(inset)
	px, py := int(previewOrigin.X+40), int(previewOrigin.Y+60)
	if before.At(px, py) == during.At(px, py) {
		t.Fatal("dragged preview is not visibly attenuated")
	}

	g.cancelThumbnailDrag()
	after := g.window.Canvas().Capture()
	if after.At(px, py) != before.At(px, py) || marker.Visible() {
		t.Fatal("ending drag did not restore preview and clear marker")
	}
}
