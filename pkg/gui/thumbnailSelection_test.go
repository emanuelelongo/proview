package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"image/color"
	"image/png"
	"os"
	"slices"
	"testing"
)

func TestThumbnailSelectionSupportsToggleRangeAndStablePageIDs(t *testing.T) {
	g := thumbnailDragTestGui(t, 6)
	g.pdfThumbBorders = make([]*canvas.Rectangle, 6)
	for index := range g.pdfThumbBorders {
		g.pdfThumbBorders[index] = canvas.NewRectangle(color.Transparent)
	}
	g.prepareThumbnailSelection(g.document)
	g.selectThumbnail(2, fyne.KeyModifierShift)
	if !slices.Equal(g.selectedPDFPages(), []int{0, 1, 2}) {
		t.Fatal("initial Shift range lost its anchor", g.selectedPDFPages())
	}
	g.selectThumbnail(1, 0)
	g.selectThumbnail(4, fyne.KeyModifierShortcutDefault)
	g.selectThumbnail(5, fyne.KeyModifierShift|fyne.KeyModifierShortcutDefault)
	if !slices.Equal(g.selectedPDFPages(), []int{1, 4, 5}) {
		t.Fatal("additive range did not preserve selection", g.selectedPDFPages())
	}
	g.selectThumbnail(4, fyne.KeyModifierShortcutDefault)
	if !slices.Equal(g.selectedPDFPages(), []int{1, 5}) {
		t.Fatal("modifier click did not deselect page")
	}
	g.activePDFPage = 0
	g.activatePDFPage(3)
	if !slices.Equal(g.selectedPDFPages(), []int{1, 5}) {
		t.Fatal("viewport activation collapsed multiple selection")
	}
	ids := g.document.PageIDs()
	if err := g.document.SetPageOrder([]int{5, 4, 3, 2, 1, 0}); err != nil {
		t.Fatal(err)
	}
	g.prepareThumbnailSelection(g.document)
	g.refreshThumbnailSelection()
	if !slices.Equal(g.selectedPDFPages(), []int{0, 4}) || !g.thumbnailSelection[ids[1]] || !g.thumbnailSelection[ids[5]] {
		t.Fatal("reordering changed selected identities")
	}
	if !g.pdfThumbBorders[0].Visible() || !g.pdfThumbBorders[4].Visible() || g.pdfThumbBorders[1].Visible() {
		t.Fatal("selection borders disagree with selected pages")
	}
}

func TestThumbnailSelectionUpdatesOnlyChangedBorders(t *testing.T) {
	g := thumbnailDragTestGui(t, 3)
	g.pdfThumbBorders = make([]*canvas.Rectangle, 3)
	for index := range g.pdfThumbBorders {
		g.pdfThumbBorders[index] = canvas.NewRectangle(color.Transparent)
	}
	g.pdfPageIDs = g.document.PageIDs()
	g.setThumbnailSelection([]int{0})
	g.pdfThumbBorders[2].Show()
	g.setThumbnailSelection([]int{1})
	if g.pdfThumbBorders[0].Visible() || !g.pdfThumbBorders[1].Visible() || !g.pdfThumbBorders[2].Visible() {
		t.Fatal("selection refresh changed a thumbnail outside the old/new selection")
	}
}

func TestThumbnailGroupDragPreservesOrderSelectionAndOneUndo(t *testing.T) {
	g := thumbnailDragTestGui(t, 6)
	ids := g.document.PageIDs()
	g.setThumbnailSelection([]int{0, 2, 4})
	origin := g.app.Driver().AbsolutePositionForObject(g.sidebar.container)
	g.pressThumbnail(0, g.document, 1, &desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	g.dragThumbnail(2, g.document, 1, &fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: origin.Add(fyne.NewPos(30, 100))}})
	state := g.thumbnailDrag
	if state == nil || !slices.Equal(state.pages, []int{0, 2, 4}) {
		t.Fatal("drag collapsed selected group")
	}
	for _, index := range state.pages {
		if g.pdfThumbImages[index].Translucency != .5 {
			t.Fatal("group member has no drag feedback")
		}
	}
	g.cancelThumbnailDrag()
	for _, preview := range g.pdfThumbImages {
		if preview.Translucency != 0 {
			t.Fatal("cancel left group faded")
		}
	}
	state.inside, state.insertion = true, 6
	to, moved, err := g.applyThumbnailMove(state)
	if err != nil || !moved || to != 3 || !slices.Equal(g.document.PageIDs(), []uint64{ids[1], ids[3], ids[5], ids[0], ids[2], ids[4]}) {
		t.Fatal("group drag reordered pages incorrectly", to, moved, err)
	}
	if !slices.Equal(g.selectedPDFPages(), []int{3, 4, 5}) {
		t.Fatal("group lost its selection after moving")
	}
	if !g.document.Undo() || !slices.Equal(g.document.PageIDs(), ids) || g.document.CanUndo() {
		t.Fatal("group move was not one Undo")
	}
	state.pages = []int{1, 2}
	state.from = 1
	state.insertion = 2
	revision := g.document.Revision()
	if _, moved, err := g.applyThumbnailMove(state); err != nil || moved || g.document.Revision() != revision {
		t.Fatal("dropping inside an unchanged contiguous group created history")
	}
}

func TestThumbnailBadgeUsesAccentAndFitsInsidePreview(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	badge := newThumbnailBadge(123)
	image := canvas.NewRectangle(color.White)
	image.SetMinSize(fyne.NewSize(130, 180))
	card := container.NewStack(image, badge)
	card.Resize(fyne.NewSize(130, 180))
	renderer := fyneTest.WidgetRenderer(badge).(*thumbnailBadgeRenderer)
	if renderer.circle.FillColor != theme.PrimaryColor() || renderer.text.Text != "123" {
		t.Fatal("badge lost number or accent color")
	}
	circle := renderer.circle
	if circle.Position().X+circle.Size().Width > 130 || circle.Position().Y+circle.Size().Height > 180 || circle.Position().X < 0 || circle.Position().Y < 0 {
		t.Fatal("badge is outside thumbnail")
	}
}

func TestThumbnailGroupDuplicateDeleteAndUndoThroughEditor(t *testing.T) {
	g, driver := newDropTestGui(t)
	doc := workspacePDF(t, 4)
	g.addDocument(doc, nil, nil)
	t.Cleanup(func() {
		if !g.closed {
			g.closeWindow()
		}
	})
	g.window.Resize(fyne.NewSize(700, 600))
	g.window.Show()
	original := doc.PageIDs()
	g.setThumbnailSelection([]int{0, 2})
	g.duplicatePDFPages(g.selectedPDFPages())
	for g.working {
		driver.runNext(t)
	}
	if doc.PageCount() != 6 || !slices.Equal(g.selectedPDFPages(), []int{3, 4}) {
		t.Fatal("editor did not select the duplicated group", g.selectedPDFPages())
	}
	if !g.pdfThumbBorders[3].Visible() || !g.pdfThumbBorders[4].Visible() || g.pdfThumbBorders[0].Visible() {
		t.Fatal("rebuilding the PDF lost multiple selection feedback")
	}
	if path := os.Getenv("PROVIEW_THUMBNAIL_PREVIEW"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, g.window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}
	g.deletePDFPages(g.selectedPDFPages())
	for g.working {
		driver.runNext(t)
	}
	if !slices.Equal(doc.PageIDs(), original) {
		t.Fatal("group deletion removed originals")
	}
	g.undo()
	for g.working {
		driver.runNext(t)
	}
	if doc.PageCount() != 6 {
		t.Fatal("one Undo did not restore the deleted group")
	}
	g.undo()
	for g.working {
		driver.runNext(t)
	}
	if !slices.Equal(doc.PageIDs(), original) || doc.Dirty() || doc.CanUndo() {
		t.Fatal("second Undo did not remove exactly the duplicated group")
	}
	revision := doc.Revision()
	g.deletePDFPages([]int{0, 1, 2, 3})
	if doc.PageCount() != 4 || doc.Revision() != revision || doc.Dirty() {
		t.Fatal("deleting every page changed the document")
	}
}

func TestThumbnailClickCapturesModifiersAtMouseDown(t *testing.T) {
	g := thumbnailDragTestGui(t, 4)
	g.setThumbnailSelection([]int{0})
	tap := newThumbnailTap(nil)
	tap.onSelect = func(modifiers fyne.KeyModifier) { g.selectThumbnail(2, modifiers) }
	tap.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary, Modifier: fyne.KeyModifierShortcutDefault})
	// Releasing the modifier before the mouse must not turn this into a plain click.
	tap.MouseUp(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	tap.Tapped(&fyne.PointEvent{})
	if !slices.Equal(g.selectedPDFPages(), []int{0, 2}) {
		t.Fatal("click ignored captured modifier")
	}
}
