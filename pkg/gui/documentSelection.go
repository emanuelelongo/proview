package gui

import (
	"slices"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
)

// Workspace selection follows retained entry identities, independently of the
// file displayed in the reading viewport. Output uses sidebar order.
func (g *Gui) singleDocumentSelection(entry *editorDocument) {
	g.documentSelection = make(map[*editorDocument]bool)
	g.documentAnchor = entry
	if entry != nil {
		g.documentSelection[entry] = true
	}
}

func (g *Gui) normalizeDocumentSelection() {
	valid := make(map[*editorDocument]bool)
	for _, entry := range g.documents {
		if g.documentSelection[entry] {
			valid[entry] = true
		}
	}
	if len(valid) == 0 && g.activeDocument != nil {
		valid[g.activeDocument] = true
	}
	g.documentSelection = valid
	if !slices.Contains(g.documents, g.documentAnchor) {
		g.documentAnchor = g.activeDocument
	}
}

func (g *Gui) selectedDocuments() []*editorDocument {
	var entries []*editorDocument
	for _, entry := range g.documents {
		if g.documentSelection[entry] {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 && g.activeDocument != nil {
		entries = append(entries, g.activeDocument)
	}
	return entries
}

func (g *Gui) selectDocumentThumbnail(entry *editorDocument, modifiers fyne.KeyModifier) {
	g.activateEditor()
	if g.closed || !slices.Contains(g.documents, entry) {
		return
	}
	if g.waitAnnotationChanges(func() { g.selectDocumentThumbnail(entry, modifiers) }) || g.working || g.pdfOpenDialog != nil {
		return
	}
	g.normalizeDocumentSelection()
	toggle := modifiers&fyne.KeyModifierShortcutDefault != 0
	if modifiers&fyne.KeyModifierShift != 0 {
		anchor := slices.Index(g.documents, g.documentAnchor)
		clicked := slices.Index(g.documents, entry)
		if anchor < 0 {
			anchor = clicked
		}
		if !toggle {
			g.documentSelection = make(map[*editorDocument]bool)
		}
		for i := min(anchor, clicked); i <= max(anchor, clicked); i++ {
			g.documentSelection[g.documents[i]] = true
		}
	} else if toggle {
		if g.documentSelection[entry] && len(g.documentSelection) > 1 {
			delete(g.documentSelection, entry)
		} else {
			g.documentSelection[entry] = true
		}
		g.documentAnchor = entry
	} else {
		g.singleDocumentSelection(entry)
	}
	if entry == g.activeDocument {
		g.refreshDocumentSidebar()
		return
	}
	g.logEvent("document_switch", "next_document=%p model=%t", entry.doc, entry.mesh != nil)
	g.prepareDocumentSwitch()
	g.activateDocument(entry)
}

func (g *Gui) selectionCanOutput() bool {
	if g.document != nil && g.document.Format() == document.FormatPDF {
		return true
	}
	entries := g.selectedDocuments()
	if len(entries) == 0 {
		return g.document != nil || g.showingMarkdown()
	}
	for _, entry := range entries {
		if entry.doc == nil && entry.markdown == nil {
			return false
		}
	}
	return true
}

func (g *Gui) updateSelectionActions() {
	disabled := !g.selectionCanOutput()
	changed := false
	for _, item := range g.menuDocumentItems {
		if item.Label == "Print…" || item.Label == "Create PDF from Selection" {
			if item.Disabled != disabled {
				item.Disabled = disabled
				changed = true
			}
		}
	}
	if !changed {
		return
	}
	for _, menu := range g.stateMenus {
		refreshMenuState(menu)
	}
}
