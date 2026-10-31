package gui

import (
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"slices"
)

func (g *Gui) thumbnailPageIDs(doc *document.Document) []uint64 {
	if doc == g.document && len(g.pdfPageIDs) == doc.PageCount() {
		return g.pdfPageIDs
	}
	return doc.PageIDs()
}

// Selection belongs to page identities, independently of the page visible in
// the main viewport. Scrolling must never collapse a multiple selection.
func (g *Gui) prepareThumbnailSelection(doc *document.Document) {
	if g.thumbnailSelectionDocument != doc {
		g.thumbnailSelectionDocument = doc
		g.thumbnailSelection = nil
		g.thumbnailAnchor = 0
	}
	valid := make(map[uint64]bool)
	ids := g.thumbnailPageIDs(doc)
	for _, id := range ids {
		if g.thumbnailSelection[id] {
			valid[id] = true
		}
	}
	if len(valid) == 0 && doc.PageCount() > 0 {
		valid[ids[0]] = true
	}
	anchorValid := false
	for _, id := range ids {
		if id == g.thumbnailAnchor {
			anchorValid = true
			break
		}
	}
	if !anchorValid {
		for _, id := range ids {
			if valid[id] {
				g.thumbnailAnchor = id
				break
			}
		}
	}
	g.thumbnailSelection = valid
}

func (g *Gui) setThumbnailSelection(pages []int) {
	if g.document == nil {
		return
	}
	old := cloneThumbnailSelection(g.thumbnailSelection)
	g.thumbnailSelectionDocument = g.document
	g.thumbnailSelection = make(map[uint64]bool, len(pages))
	ids := g.thumbnailPageIDs(g.document)
	for _, page := range pages {
		if page >= 0 && page < len(ids) {
			g.thumbnailSelection[ids[page]] = true
		}
	}
	if len(pages) > 0 && pages[0] >= 0 && pages[0] < len(ids) {
		g.thumbnailAnchor = ids[pages[0]]
	}
	g.refreshThumbnailSelectionDiff(old, g.thumbnailSelection)
}

func (g *Gui) selectedPDFPages() []int {
	if g.document == nil || g.document.Format() != document.FormatPDF {
		return nil
	}
	var pages []int
	if g.thumbnailSelectionDocument == g.document {
		for page, id := range g.thumbnailPageIDs(g.document) {
			if g.thumbnailSelection[id] {
				pages = append(pages, page)
			}
		}
	}
	if len(pages) == 0 {
		pages = []int{g.activePDFPage}
	}
	return pages
}

func (g *Gui) selectThumbnail(page int, modifiers fyne.KeyModifier) {
	if g.document == nil || page < 0 || page >= g.document.PageCount() {
		return
	}
	g.prepareThumbnailSelection(g.document)
	old := cloneThumbnailSelection(g.thumbnailSelection)
	ids := g.thumbnailPageIDs(g.document)
	toggle := modifiers&fyne.KeyModifierShortcutDefault != 0
	if modifiers&fyne.KeyModifierShift != 0 {
		anchor := page
		for index, id := range ids {
			if id == g.thumbnailAnchor {
				anchor = index
				break
			}
		}
		if !toggle {
			g.thumbnailSelection = make(map[uint64]bool)
		}
		for index := min(anchor, page); index <= max(anchor, page); index++ {
			g.thumbnailSelection[ids[index]] = true
		}
	} else if toggle {
		if g.thumbnailSelection[ids[page]] && len(g.thumbnailSelection) > 1 {
			delete(g.thumbnailSelection, ids[page])
		} else {
			g.thumbnailSelection[ids[page]] = true
		}
		g.thumbnailAnchor = ids[page]
	} else {
		g.setThumbnailSelection([]int{page})
		return
	}
	g.refreshThumbnailSelectionDiff(old, g.thumbnailSelection)
}

func (g *Gui) refreshThumbnailSelection() {
	if g.document == nil || g.thumbnailSelectionDocument != g.document {
		return
	}
	for page, id := range g.thumbnailPageIDs(g.document) {
		if page >= len(g.pdfThumbBorders) {
			break
		}
		if g.thumbnailSelection[id] {
			g.pdfThumbBorders[page].Show()
		} else {
			g.pdfThumbBorders[page].Hide()
		}
	}
	g.updateSelectionActions()
}

func cloneThumbnailSelection(selection map[uint64]bool) map[uint64]bool {
	if len(selection) == 0 {
		return nil
	}
	copy := make(map[uint64]bool, len(selection))
	for id := range selection {
		copy[id] = true
	}
	return copy
}

func (g *Gui) refreshThumbnailSelectionDiff(old, current map[uint64]bool) {
	if g.document == nil || g.thumbnailSelectionDocument != g.document {
		return
	}
	for id := range old {
		if !current[id] {
			g.setThumbnailBorder(id, false)
		}
	}
	for id := range current {
		if !old[id] {
			g.setThumbnailBorder(id, true)
		}
	}
	g.updateSelectionActions()
}

func (g *Gui) setThumbnailBorder(id uint64, selected bool) {
	page := slices.Index(g.thumbnailPageIDs(g.document), id)
	if page < 0 || page >= len(g.pdfThumbBorders) {
		return
	}
	if selected {
		g.pdfThumbBorders[page].Show()
	} else {
		g.pdfThumbBorders[page].Hide()
	}
}
