package gui

import "fyne.io/fyne/v2"

func (g *Gui) toggleMarkdownSource() {
	v := g.markdownView
	if v == nil {
		return
	}
	g.closeSearch()
	g.window.Canvas().Focus(nil)
	v.sourceMode = !v.sourceMode
	v.rebuild()
	v.restoreReadingOffset(fyne.Position{})
	g.captureDocument()
	g.updateHistoryActions()
}
