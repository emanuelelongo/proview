package gui

import (
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

type fileInfoSurface struct {
	widget.BaseWidget
	editor *Gui
}

func newFileInfoSurface(g *Gui) *fileInfoSurface {
	s := &fileInfoSurface{editor: g}
	s.ExtendBaseWidget(s)
	return s
}

func (s *fileInfoSurface) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(s.editor.fileInfo)
}

func (s *fileInfoSurface) TappedSecondary(event *fyne.PointEvent) {
	if menu := s.editor.fileInfoMenu(); menu != nil {
		widget.ShowPopUpMenuAtPosition(menu, s.editor.window.Canvas(), event.AbsolutePosition)
	}
}

func (g *Gui) fileInfoMenu() *fyne.Menu {
	if g.closed || !g.hasOpenContent() {
		return nil
	}
	target := g.saveTarget
	name := ""
	switch {
	case g.showingMarkdown():
		name = g.markdownView.doc.Name
		if g.activeDocument != nil {
			target = g.activeDocument.target
		}
	case g.showingModel():
		name = g.modelViewport.mesh.Name
		if g.activeDocument != nil {
			target = g.activeDocument.target
		}
	case g.document != nil:
		name = g.document.Name()
	}
	path := ""
	if target != nil {
		name = target.Name()
		if target.Scheme() == "file" {
			if absolute, err := filepath.Abs(target.Path()); err == nil {
				path = absolute
			}
		}
	}
	copyName := fyne.NewMenuItem("Copy filename", func() { g.window.Clipboard().SetContent(name) })
	copyName.Disabled = name == ""
	copyPath := fyne.NewMenuItem("Copy filename with full path", func() { g.window.Clipboard().SetContent(path) })
	copyPath.Disabled = path == ""
	return fyne.NewMenu("", copyName, copyPath)
}
