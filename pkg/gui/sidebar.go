package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

type Sidebar struct {
	container *container.Scroll
	g         *Gui
}

func NewSidebar(g *Gui) *Sidebar {

	scroll := container.NewVScroll(g.thumbList)
	scroll.SetMinSize(fyne.NewSize(0, 0))
	scroll.Hide()

	return &Sidebar{
		g:         g,
		container: scroll,
	}
}
