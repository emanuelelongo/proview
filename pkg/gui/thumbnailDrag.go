package gui

import (
	"emanuelelongo/proview/pkg/document"
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type thumbnailDragState struct {
	document        *document.Document
	generation      uint64
	from, insertion int
	pointer         fyne.Position // relative to the sidebar viewport, independent of scrolling
	inside          bool
	timer           *time.Timer
	pages           []int
	modifiers       fyne.KeyModifier
	previews        map[*canvas.Image]float64
}

// A passive marker paints an insertion line without changing card geometry or
// intercepting input. No popup overlays are used during dragging.
type thumbnailDropIndicator struct {
	widget.BaseWidget
	bottom bool
}

func newThumbnailDropIndicator() *thumbnailDropIndicator {
	m := &thumbnailDropIndicator{}
	m.ExtendBaseWidget(m)
	m.Hide()
	return m
}
func (m *thumbnailDropIndicator) CreateRenderer() fyne.WidgetRenderer {
	return &thumbnailDropRenderer{marker: m, line: canvas.NewRectangle(theme.PrimaryColor())}
}

type thumbnailDropRenderer struct {
	marker *thumbnailDropIndicator
	line   *canvas.Rectangle
}

func (r *thumbnailDropRenderer) Layout(size fyne.Size) {
	y := float32(0)
	if r.marker.bottom {
		y = max(0, size.Height-4)
	}
	r.line.Move(fyne.NewPos(0, y))
	r.line.Resize(fyne.NewSize(size.Width, 4))
}
func (*thumbnailDropRenderer) MinSize() fyne.Size { return fyne.Size{} }
func (r *thumbnailDropRenderer) Refresh() {
	r.line.FillColor = theme.PrimaryColor()
	r.Layout(r.marker.Size())
	r.line.Refresh()
	canvas.Refresh(r.marker)
}
func (r *thumbnailDropRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.line} }
func (*thumbnailDropRenderer) Destroy()                       {}

func (g *Gui) pressThumbnail(page int, doc *document.Document, generation uint64, event *desktop.MouseEvent) {
	if event.Button != desktop.MouseButtonPrimary || g.working || g.document != doc || g.pdfGeneration != generation {
		return
	}
	g.thumbnailPress = &thumbnailDragState{document: doc, generation: generation, from: page, modifiers: event.Modifier}
}

func (g *Gui) releaseThumbnail(event *desktop.MouseEvent) {
	if event.Button != desktop.MouseButtonPrimary {
		return
	}
	g.thumbnailPress = nil
	if state := g.thumbnailDrag; state != nil {
		origin := g.app.Driver().AbsolutePositionForObject(g.sidebar.container)
		state.pointer = event.AbsolutePosition.Subtract(origin)
		g.updateThumbnailDrop(state)
	}
}

func (g *Gui) thumbnailCursor() desktop.Cursor {
	if g.thumbnailDrag != nil {
		return desktop.VResizeCursor
	}
	return desktop.PointerCursor
}

func (g *Gui) dragThumbnail(page int, doc *document.Document, generation uint64, event *fyne.DragEvent) {
	// GLFW may choose a different card after crossing its drag threshold.
	// Keep the source captured at mouse-down throughout the gesture.
	if press := g.thumbnailPress; press != nil {
		page, doc, generation = press.from, press.document, press.generation
	}
	if g.working || g.document != doc || g.pdfGeneration != generation {
		return
	}
	if g.thumbnailDrag == nil {
		if page < 0 || page >= doc.PageCount() {
			return
		}
		pages := g.selectedPDFPages()
		if !slices.Contains(pages, page) {
			modifiers := fyne.KeyModifier(0)
			if g.thumbnailPress != nil {
				modifiers = g.thumbnailPress.modifiers
			}
			g.selectThumbnail(page, modifiers)
			pages = g.selectedPDFPages()
		}
		g.thumbnailDrag = &thumbnailDragState{document: doc, generation: generation, from: page, pages: pages, previews: make(map[*canvas.Image]float64)}
		for _, index := range pages {
			if index < len(g.pdfThumbImages) {
				preview := g.pdfThumbImages[index]
				g.thumbnailDrag.previews[preview] = preview.Translucency
				preview.Translucency = .5
				preview.Refresh()
			}
		}
		g.activatePDFPage(page)
	}
	state := g.thumbnailDrag
	if state.document != doc || state.generation != generation {
		return
	}
	origin := g.app.Driver().AbsolutePositionForObject(g.sidebar.container)
	state.pointer = event.AbsolutePosition.Subtract(origin)
	g.updateThumbnailDrop(state)
	g.scheduleThumbnailScroll(state)
}

func (g *Gui) updateThumbnailDrop(state *thumbnailDragState) {
	size := g.sidebar.container.Size()
	state.inside = state.pointer.X >= 0 && state.pointer.X < size.Width && state.pointer.Y >= 0 && state.pointer.Y < size.Height
	for _, marker := range g.pdfThumbDropIndicators {
		marker.Hide()
	}
	if !state.inside {
		return
	}
	y := state.pointer.Y + g.sidebar.container.Offset.Y
	top := float32(0)
	state.insertion = len(g.thumbList.Objects)
	for page, object := range g.thumbList.Objects {
		height := object.MinSize().Height
		if y < top+height/2 {
			state.insertion = page
			break
		}
		top += height + theme.Padding()
	}
	if len(g.pdfThumbDropIndicators) == 0 {
		return
	}
	index := state.insertion
	bottom := index == len(g.pdfThumbDropIndicators)
	if bottom {
		index--
	}
	marker := g.pdfThumbDropIndicators[index]
	marker.bottom = bottom
	// Match the card size before showing the marker, including its first
	// visible frame, and repaint after updating the line geometry.
	marker.Resize(g.thumbList.Objects[index].Size())
	marker.Show()
	marker.Refresh()
}

func (g *Gui) scrollThumbnailDrag(state *thumbnailDragState) bool {
	if !state.inside {
		return false
	}
	height := g.sidebar.container.Size().Height
	const edge = float32(40)
	delta := float32(0)
	if state.pointer.Y < edge {
		delta = -4 - 12*(edge-state.pointer.Y)/edge
	} else if state.pointer.Y > height-edge {
		delta = 4 + 12*(state.pointer.Y-(height-edge))/edge
	}
	if delta == 0 {
		return false
	}
	old := g.sidebar.container.Offset.Y
	g.sidebar.container.ScrollToOffset(fyne.NewPos(0, old+delta))
	g.updateThumbnailDrop(state)
	return old != g.sidebar.container.Offset.Y
}
func (g *Gui) scheduleThumbnailScroll(state *thumbnailDragState) {
	if state.timer != nil {
		return
	}
	state.timer = time.AfterFunc(30*time.Millisecond, func() {
		fyne.Do(func() {
			if g.thumbnailDrag != state {
				return
			}
			state.timer = nil
			if g.document != state.document || g.pdfGeneration != state.generation || g.working {
				g.cancelThumbnailDrag()
				return
			}
			if g.scrollThumbnailDrag(state) {
				g.scheduleThumbnailScroll(state)
			}
		})
	})
}
func (g *Gui) cancelThumbnailDrag() bool {
	pressed := g.thumbnailPress != nil
	g.thumbnailPress = nil
	state := g.thumbnailDrag
	if state == nil {
		return pressed
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	for preview, translucency := range state.previews {
		preview.Translucency = translucency
		preview.Refresh()
	}
	g.thumbnailDrag = nil
	for _, marker := range g.pdfThumbDropIndicators {
		marker.Hide()
	}
	return true
}

// The insertion index refers to the gaps in the original list, including the
// final gap. Removing the source first adjusts destinations below that source.
func thumbnailPageOrder(count, from, insertion int) ([]int, int) {
	return thumbnailPagesOrder(count, []int{from}, insertion)
}

func (g *Gui) finishThumbnailDrag() {
	state := g.thumbnailDrag
	if state == nil {
		return
	}
	g.cancelThumbnailDrag()
	g.finishThumbnailMove(state)
}

func (g *Gui) finishThumbnailMove(state *thumbnailDragState) {
	if g.waitAnnotationChanges(func() { g.finishThumbnailMove(state) }) {
		return
	}
	to, moved, err := g.applyThumbnailMove(state)
	if err != nil {
		g.showOperationError("pdf_pages", err)
		return
	}
	if moved {
		g.rebuildPDFWithCompletion(to, func() { g.revealThumbnail(to) })
	}
}

func (g *Gui) applyThumbnailMove(state *thumbnailDragState) (int, bool, error) {
	if !state.inside || g.working || g.document != state.document || g.pdfGeneration != state.generation {
		return 0, false, nil
	}
	pages := state.pages
	if len(pages) == 0 {
		pages = []int{state.from}
	}
	order, _ := thumbnailPagesOrder(g.document.PageCount(), pages, state.insertion)
	if order == nil {
		return 0, false, nil
	}
	unchanged := true
	to := 0
	for index, page := range order {
		if page != index {
			unchanged = false
		}
		if page == state.from {
			to = index
		}
	}
	if unchanged {
		return 0, false, nil
	}
	if err := g.document.SetPageOrder(order); err != nil {
		return 0, false, err
	}
	return to, true, nil
}

func (g *Gui) revealThumbnail(page int) {
	top := float32(0)
	for i := 0; i < page && i < len(g.thumbList.Objects); i++ {
		top += g.thumbList.Objects[i].MinSize().Height + theme.Padding()
	}
	if page < 0 || page >= len(g.thumbList.Objects) {
		return
	}
	bottom := top + g.thumbList.Objects[page].MinSize().Height
	offset := g.sidebar.container.Offset.Y
	if top < offset {
		offset = top
	} else if bottom > offset+g.sidebar.container.Size().Height {
		offset = bottom - g.sidebar.container.Size().Height
	}
	g.sidebar.container.ScrollToOffset(fyne.NewPos(0, max(0, offset)))
}

func (g *Gui) thumbnailContextMenu(page int, event *fyne.PointEvent) {
	if g.working {
		return
	}
	if !slices.Contains(g.selectedPDFPages(), page) {
		g.setThumbnailSelection([]int{page})
	}
	g.activatePDFPage(page)
	pages := g.selectedPDFPages()
	deleteLabel, duplicateLabel := "Delete page", "Duplicate page"
	if len(pages) > 1 {
		deleteLabel = "Delete selected pages"
		duplicateLabel = "Duplicate selected pages"
	}
	item := fyne.NewMenuItem(deleteLabel, func() { g.deletePDFPages(pages) })
	item.Icon = tablerIcon("trash")
	item.Disabled = g.document == nil || len(pages) >= g.document.PageCount()
	duplicate := fyne.NewMenuItem(duplicateLabel, func() { g.duplicatePDFPages(pages) })
	duplicate.Icon = tablerIcon("duplicate")
	widget.NewPopUpMenu(fyne.NewMenu("Pages", duplicate, item), g.window.Canvas()).ShowAtPosition(event.AbsolutePosition)
}

// Insertion is a gap in the original list. Selected pages retain document order.
func thumbnailPagesOrder(count int, pages []int, insertion int) ([]int, int) {
	if count < 1 || len(pages) == 0 || insertion < 0 || insertion > count {
		return nil, -1
	}
	selected := make(map[int]bool, len(pages))
	to := insertion
	for _, page := range pages {
		if page < 0 || page >= count || selected[page] {
			return nil, -1
		}
		selected[page] = true
		if page < insertion {
			to--
		}
	}
	remaining := make([]int, 0, count-len(pages))
	group := make([]int, 0, len(pages))
	for page := 0; page < count; page++ {
		if selected[page] {
			group = append(group, page)
		} else {
			remaining = append(remaining, page)
		}
	}
	order := make([]int, 0, count)
	order = append(order, remaining[:to]...)
	order = append(order, group...)
	order = append(order, remaining[to:]...)
	return order, to
}
