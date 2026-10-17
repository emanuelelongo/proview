package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

type Sidebar struct {
	container *container.Scroll
	panel     *fyne.Container
	documents *container.Scroll
	g         *Gui
}

func NewSidebar(g *Gui) *Sidebar {

	if g.documentList == nil {
		g.documentList = container.NewVBox()
	}
	documents := container.NewVScroll(g.documentList)
	documents.SetMinSize(fyne.NewSize(0, 140))
	documents.Hide()
	scroll := container.NewVScroll(g.thumbList)
	scroll.SetMinSize(fyne.NewSize(0, 0))
	scroll.Hide()

	panel := container.NewBorder(documents, nil, nil, nil, scroll)
	panel.Hide()
	return &Sidebar{
		panel:     panel,
		documents: documents,
		g:         g,
		container: scroll,
	}
}
