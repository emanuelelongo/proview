package gui

import (
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
)

func (g *Gui) openStartupFiles(paths []string) {
	for index, path := range paths {
		reader, err := startupFileReader(path)
		if err != nil {
			errorEditor := g
			if g.closed {
				errorEditor = newGui(g.app)
				errorEditor.window.Show()
			}
			BuildOpenFileCallback(errorEditor)(nil, err)
			continue
		}
		editor := g
		if index > 0 || g.hasOpenContent() || g.working || g.closed {
			editor = newGui(g.app)
			editor.window.Show()
		}
		BuildOpenFileCallback(editor)(reader, nil)
	}
}

func (g *Gui) openFilesFromFinder(paths []string) {
	if len(paths) == 0 {
		return
	}
	fyne.Do(func() {
		g.openStartupFiles(paths)
	})
}

func (g *Gui) openDroppedFiles(_ fyne.Position, uris []fyne.URI) {
	paths := make([]string, 0, len(uris))
	for _, uri := range uris {
		if uri != nil && uri.Scheme() == "file" {
			paths = append(paths, uri.Path())
		}
	}
	if len(paths) == 0 {
		return
	}
	// Native drop callbacks run outside Fyne's UI goroutine.
	fyne.Do(func() {
		if !g.closed {
			g.openStartupFiles(paths)
		}
	})
}

func startupFileReader(path string) (reader fyne.URIReadCloser, err error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	reader, err = storage.Reader(storage.NewFileURI(filepath.Clean(absolute)))
	if err != nil && reader != nil {
		_ = reader.Close()
		reader = nil
	}
	return reader, err
}
