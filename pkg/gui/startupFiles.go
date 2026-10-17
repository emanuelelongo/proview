package gui

import (
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
)

func (g *Gui) openStartupFiles(paths []string) {
	if g.closed {
		editor := g.newEditor()
		editor.window.Show()
		editor.openStartupFiles(paths)
		return
	}
	g.pendingFiles = append(g.pendingFiles, paths...)
	g.processPendingFiles()
}

// OpenFiles accepts desktop requests from a background worker.
func (g *Gui) OpenFiles(paths []string) { g.openFilesFromFinder(paths) }

func (g *Gui) openFilesFromFinder(paths []string) {
	if len(paths) == 0 {
		return
	}
	fyne.Do(func() {
		editor := g.desktopEditor()
		editor.openStartupFiles(paths)
		if !editor.closed {
			editor.window.RequestFocus()
		}
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
			g.activateEditor()
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
