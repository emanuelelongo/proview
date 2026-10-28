package gui

import (
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
)

// OpenOptions applies only to the receiving editor window. A work directory
// controls dialogs; it is not a process working directory or a sandbox.
type OpenOptions struct {
	NewWindow   bool
	ReuseWindow bool
	WorkDir     string
}

func (g *Gui) openStartupFiles(paths []string) {
	if g.closed {
		editor := g.newEditor()
		editor.prepareWindowOpening(paths)
		editor.openStartupFiles(paths)
		return
	}
	g.pendingFiles = append(g.pendingFiles, paths...)
	g.processPendingFiles()
}

// OpenFiles accepts desktop requests from a background worker.
func (g *Gui) OpenFiles(paths []string) { g.OpenFilesWithOptions(paths, OpenOptions{}) }

// OpenFilesWithOptions accepts command-line requests from the IPC worker.
func (g *Gui) OpenFilesWithOptions(paths []string, options OpenOptions) {
	if len(paths) == 0 && !options.NewWindow && options.WorkDir == "" {
		return
	}
	paths = append([]string(nil), paths...)
	fyne.Do(func() { g.openLaunchRequest(paths, options) })
}

func (g *Gui) openLaunchRequest(paths []string, options OpenOptions) {
	editor := g.desktopEditor()
	if options.ReuseWindow {
		paths = g.launchPaths(paths)
	}
	if options.NewWindow || editor.closed || (!options.ReuseWindow && len(paths) > 0 && (editor.hasOpenContent() || editor.working || editor.openingFiles || len(editor.pendingFiles) > 0)) {
		editor = editor.newEditor()
		// Desktop file requests get a fresh workspace when the active editor
		// is occupied. In-window pickers and drops bypass this entry point.
		// Launches get their own settings, not the active window's.
		editor.workDir = options.WorkDir
		editor.prepareWindowOpening(paths)
	} else if options.WorkDir != "" {
		editor.workDir = options.WorkDir
	}
	editor.activateEditor()
	editor.openStartupFiles(paths)
	if !editor.closed && !editor.showAfterOpening {
		editor.window.RequestFocus()
	}
}

func (g *Gui) openFilesFromFinder(paths []string) { g.OpenFiles(paths) }

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
