package gui

import (
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
)

// Selection belongs to page identities, independently of the page visible in
// the main viewport. Scrolling must never collapse a multiple selection.
func (g *Gui) prepareThumbnailSelection(doc *document.Document) {
	if g.thumbnailSelectionDocument != doc {
		g.thumbnailSelectionDocument = doc
		g.thumbnailSelection = nil
		g.thumbnailAnchor = 0
	}
	valid := make(map[uint64]bool)
	for _, id := range doc.PageIDs() {
		if g.thumbnailSelection[id] {
			valid[id] = true
		}
	}
	if len(valid) == 0 && doc.PageCount() > 0 {
		valid[doc.PageIDs()[0]] = true
	}
	anchorValid := false
	for _, id := range doc.PageIDs() {
		if id == g.thumbnailAnchor {
			anchorValid = true
			break
		}
	}
	if !anchorValid {
		for _, id := range doc.PageIDs() {
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
	g.thumbnailSelectionDocument = g.document
	g.thumbnailSelection = make(map[uint64]bool, len(pages))
	ids := g.document.PageIDs()
	for _, page := range pages {
		if page >= 0 && page < len(ids) {
			g.thumbnailSelection[ids[page]] = true
		}
	}
	if len(pages) > 0 && pages[0] >= 0 && pages[0] < len(ids) {
		g.thumbnailAnchor = ids[pages[0]]
	}
	g.refreshThumbnailSelection()
}

func (g *Gui) selectedPDFPages() []int {
	if g.document == nil || g.document.Format() != document.FormatPDF {
		return nil
	}
	var pages []int
	if g.thumbnailSelectionDocument == g.document {
		for page, id := range g.document.PageIDs() {
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
	ids := g.document.PageIDs()
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
	g.refreshThumbnailSelection()
}

func (g *Gui) refreshThumbnailSelection() {
	if g.document == nil || g.thumbnailSelectionDocument != g.document {
		return
	}
	for page, id := range g.document.PageIDs() {
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
