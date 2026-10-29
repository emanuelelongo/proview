package gui

import (
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
)

type documentThumbnail struct {
	entry   *editorDocument
	card    *fyne.Container
	preview *canvas.Image
	marker  *thumbnailDropIndicator
}

type documentDragState struct {
	entries   []*editorDocument
	indices   []int
	insertion int
	pointer   fyne.Position
	inside    bool
	timer     *time.Timer
	previews  map[*canvas.Image]float64
}

func (g *Gui) configureDocumentDrag(entry *editorDocument, card *fyne.Container, tap *thumbnailTap, preview *canvas.Image) {
	marker := newThumbnailDropIndicator()
	card.Add(marker)
	g.documentThumbs = append(g.documentThumbs, documentThumbnail{entry, card, preview, marker})
	tap.onMouseDown = func(e *desktop.MouseEvent) {
		if e.Button == desktop.MouseButtonPrimary && !g.working {
			g.documentPress = entry
		}
	}
	tap.onMouseUp = func(e *desktop.MouseEvent) {
		if e.Button != desktop.MouseButtonPrimary {
			return
		}
		g.documentPress = nil
		if g.documentDrag != nil {
			g.updateDocumentDrop(e.AbsolutePosition)
		}
	}
	tap.onDrag = func(e *fyne.DragEvent) { g.dragDocument(entry, e.AbsolutePosition) }
	tap.onDragEnd = g.finishDocumentDrag
	tap.onCursor = func() desktop.Cursor {
		if g.documentDrag != nil {
			return desktop.VResizeCursor
		}
		return desktop.PointerCursor
	}
}

func (g *Gui) dragDocument(entry *editorDocument, absolute fyne.Position) {
	if g.closed || g.working || g.pdfOpenDialog != nil {
		return
	}
	if g.documentPress != nil {
		entry = g.documentPress
	}
	if g.documentDrag == nil {
		if !slices.Contains(g.documents, entry) {
			return
		}
		state := &documentDragState{entries: slices.Clone(g.documents), previews: make(map[*canvas.Image]float64)}
		for i, candidate := range g.documents {
			if candidate == entry || g.documentSelection[entry] && g.documentSelection[candidate] {
				state.indices = append(state.indices, i)
			}
		}
		g.documentDrag = state
		for _, i := range state.indices {
			if i < len(g.documentThumbs) {
				if image := g.documentThumbs[i].preview; image != nil {
					state.previews[image] = image.Translucency
					image.Translucency = .5
					image.Refresh()
				}
			}
		}
	}
	g.updateDocumentDrop(absolute)
	g.scheduleDocumentDragScroll(g.documentDrag)
}

func (g *Gui) updateDocumentDrop(absolute fyne.Position) {
	state := g.documentDrag
	origin := g.app.Driver().AbsolutePositionForObject(g.sidebar.documents)
	state.pointer = absolute.Subtract(origin)
	g.positionDocumentDrop(state)
}

func (g *Gui) positionDocumentDrop(state *documentDragState) {
	size := g.sidebar.documents.Size()
	p := state.pointer
	state.inside = p.X >= 0 && p.X < size.Width && p.Y >= 0 && p.Y < size.Height
	for _, thumb := range g.documentThumbs {
		thumb.marker.Hide()
	}
	if !state.inside || len(g.documentThumbs) == 0 {
		return
	}
	y := p.Y + g.sidebar.documents.Offset.Y
	state.insertion = len(g.documentThumbs)
	for i, thumb := range g.documentThumbs {
		if y < thumb.card.Position().Y+thumb.card.Size().Height/2 {
			state.insertion = i
			break
		}
	}
	i := state.insertion
	bottom := i == len(g.documentThumbs)
	if bottom {
		i--
	}
	thumb := g.documentThumbs[i]
	thumb.marker.bottom = bottom
	thumb.marker.Resize(thumb.card.Size())
	thumb.marker.Show()
	thumb.marker.Refresh()
}

func (g *Gui) scheduleDocumentDragScroll(state *documentDragState) {
	if state.timer != nil {
		return
	}
	state.timer = time.AfterFunc(30*time.Millisecond, func() {
		fyne.Do(func() {
			if g.documentDrag != state {
				return
			}
			state.timer = nil
			if g.closed || g.working || !slices.Equal(g.documents, state.entries) {
				g.cancelDocumentDrag()
				return
			}
			if !state.inside {
				return
			}
			height := g.sidebar.documents.Size().Height
			const edge = float32(40)
			delta := float32(0)
			if state.pointer.Y < edge {
				delta = -4 - 12*(edge-state.pointer.Y)/edge
			} else if state.pointer.Y > height-edge {
				delta = 4 + 12*(state.pointer.Y-height+edge)/edge
			}
			if delta == 0 {
				return
			}
			old := g.sidebar.documents.Offset.Y
			g.sidebar.documents.ScrollToOffset(fyne.NewPos(0, old+delta))
			g.positionDocumentDrop(state)
			if old != g.sidebar.documents.Offset.Y {
				g.scheduleDocumentDragScroll(state)
			}
		})
	})
}

func (g *Gui) cancelDocumentDrag() bool {
	pressed := g.documentPress != nil
	g.documentPress = nil
	state := g.documentDrag
	if state == nil {
		return pressed
	}
	g.documentDrag = nil
	if state.timer != nil {
		state.timer.Stop()
	}
	for image, opacity := range state.previews {
		image.Translucency = opacity
		image.Refresh()
	}
	for _, thumb := range g.documentThumbs {
		thumb.marker.Hide()
	}
	return true
}

func (g *Gui) finishDocumentDrag() {
	state := g.documentDrag
	g.cancelDocumentDrag()
	if state == nil || !state.inside || g.closed || g.working || !slices.Equal(g.documents, state.entries) {
		return
	}
	order, _ := thumbnailPagesOrder(len(state.entries), state.indices, state.insertion)
	if order == nil {
		return
	}
	for i, original := range order {
		g.documents[i] = state.entries[original]
	}
	g.refreshDocumentSidebar()
}
