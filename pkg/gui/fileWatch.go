package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"emanuelelongo/proview/internal/markdown"
	"emanuelelongo/proview/internal/model3d"
	"emanuelelongo/proview/pkg/document"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const fileChangeCheckInterval = 2 * time.Second

// fileStamp identifies the on-disk version of an opened file. A reload after
// our own save or an external write records the new stamp, avoiding a loop.
type fileStamp struct {
	modTime int64
	size    int64
	valid   bool
}

func stampForPath(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{modTime: info.ModTime().UnixNano(), size: info.Size(), valid: true}
}

func stampForURI(uri fyne.URI) fileStamp {
	if uri == nil || uri.Scheme() != "file" {
		return fileStamp{}
	}
	return stampForPath(uri.Path())
}

func (g *Gui) startFileWatch() {
	if g.closed || g.fileWatchTimer != nil {
		return
	}
	g.fileWatchGeneration++
	g.scheduleFileWatch(g.fileWatchGeneration)
}

func (g *Gui) stopFileWatch() {
	g.fileWatchGeneration++
	if g.fileWatchTimer != nil {
		g.fileWatchTimer.Stop()
		g.fileWatchTimer = nil
	}
}

func (g *Gui) scheduleFileWatch(generation uint64) {
	g.fileWatchTimer = time.AfterFunc(fileChangeCheckInterval, func() {
		fyne.Do(func() {
			if g.closed || generation != g.fileWatchGeneration {
				return
			}
			g.fileWatchTimer = nil
			g.checkActiveFileChange()
			g.scheduleFileWatch(generation)
		})
	})
}

// activeFileTarget returns the active file-backed entry and its current path.
// Prefer the save target after Save As, before the entry is captured again.
func (g *Gui) activeFileTarget() (*editorDocument, fyne.URI) {
	entry := g.activeDocument
	if entry == nil {
		return nil, nil
	}
	uri := entry.target
	if entry.doc != nil && g.saveTarget != nil {
		uri = g.saveTarget
	}
	if uri == nil || uri.Scheme() != "file" {
		return nil, nil
	}
	return entry, uri
}

func (g *Gui) checkActiveFileChange() {
	if g.closed || g.working || g.openingFiles || g.externalOpenPauses > 0 ||
		len(g.pendingFiles) > 0 || g.pdfOpenDialog != nil || g.reloadPrompt != nil ||
		g.window == nil || g.window.Canvas().Overlays().Top() != nil {
		return
	}
	entry, uri := g.activeFileTarget()
	if entry == nil {
		return
	}
	path := uri.Path()
	go func() {
		stamp := stampForPath(path)
		if !stamp.valid {
			return
		}
		fyne.Do(func() {
			if g.closed || entry != g.activeDocument || !slices.Contains(g.documents, entry) || entry.stamp == stamp {
				return
			}
			entry.stamp = stamp
			if entry.doc != nil && entry.doc.Dirty() {
				g.promptFileReload(entry, uri, true)
				return
			}
			g.reloadFileEntry(entry, uri)
		})
	}()
}

// reloadCurrentFile implements File > Reload from Disk. Unsaved edits are never
// discarded without an explicit confirmation.
func (g *Gui) reloadCurrentFile() {
	if g.closed || g.working {
		return
	}
	entry, uri := g.activeFileTarget()
	if entry == nil {
		return
	}
	if entry.doc != nil && entry.doc.Dirty() {
		g.promptFileReload(entry, uri, false)
		return
	}
	g.reloadFileEntry(entry, uri)
}

func (g *Gui) promptFileReload(entry *editorDocument, uri fyne.URI, external bool) {
	if g.reloadPrompt != nil || g.closed {
		return
	}
	title := "Reload file"
	message := fmt.Sprintf("Reload %s and discard unsaved changes?", uri.Name())
	if external {
		title = "File changed"
		message = fmt.Sprintf("%s changed on disk. Reload it and discard unsaved changes?", uri.Name())
	}
	prompt := dialog.NewCustomWithoutButtons(title, widget.NewLabel(message), g.window)
	g.reloadPrompt = prompt
	prompt.SetOnClosed(func() {
		if g.reloadPrompt == prompt {
			g.reloadPrompt = nil
		}
	})
	prompt.SetButtons([]fyne.CanvasObject{
		widget.NewButton("Keep current", prompt.Hide),
		widget.NewButton("Reload", func() {
			prompt.Hide()
			g.reloadFileEntry(entry, uri)
		}),
	})
	prompt.Show()
}

// reloadFileEntry replaces an open entry's loaded content while keeping its
// sidebar identity and, for the active entry, page/scroll/zoom state.
func (g *Gui) reloadFileEntry(entry *editorDocument, uri fyne.URI) {
	if g.closed || g.working || entry == nil || uri == nil || !slices.Contains(g.documents, entry) {
		return
	}
	if entry == g.activeDocument {
		g.captureDocument()
	}
	path, name := uri.Path(), uri.Name()
	readStamp := stampForPath(path)
	markdownImages, markdownAnimations := int64(0), int64(0)
	if markdown.IsFile(name) {
		markdownImages, markdownAnimations = g.markdownLoadBudgets()
	}
	g.working = true
	g.logEvent("reload_started", "extension=%q", filepath.Ext(name))
	go func() {
		var loadedDoc *document.Document
		var loadedMarkdown *markdown.Document
		var loadedMesh *model3d.Mesh
		reader, err := startupFileReader(path)
		if err == nil {
			switch {
			case markdown.IsFile(name):
				loadedMarkdown, err = loadMarkdownReader(reader, markdownImages, markdownAnimations)
			case isModelFile(name):
				loadedMesh, err = loadModelReader(reader)
			default:
				loadedDoc, _, err = loadDocumentReader(reader)
			}
		}
		fyne.Do(func() {
			g.working = false
			defer g.processPendingFiles()
			if g.closed || !slices.Contains(g.documents, entry) {
				if loadedDoc != nil {
					loadedDoc.Close()
				}
				if loadedMarkdown != nil {
					loadedMarkdown.Close()
				}
				return
			}
			if err != nil {
				g.logEvent("reload_failed", "level=ERROR error=%q", err)
				g.showOperationError("reload", err)
				return
			}
			oldDoc, oldMarkdown := entry.doc, entry.markdown
			entry.doc, entry.mesh, entry.markdown = loadedDoc, loadedMesh, loadedMarkdown
			entry.markdownView = nil
			entry.redactedTargets = nil
			entry.thumbnail, entry.thumbnailSource = nil, nil
			entry.target = uri
			entry.stamp = readStamp
			if entry == g.activeDocument {
				g.activateDocument(entry)
			}
			g.refreshDocumentSidebar()
			if oldDoc != nil && oldDoc != entry.doc && !g.retainsDocument(oldDoc) {
				go oldDoc.Close()
			}
			if oldMarkdown != nil && oldMarkdown != entry.markdown && !g.markdownRetained(oldMarkdown) {
				go oldMarkdown.Close()
			}
			g.logEvent("reload_completed", "extension=%q", filepath.Ext(name))
			g.updateHistoryActions()
		})
	}()
}

func (g *Gui) markdownRetained(doc *markdown.Document) bool {
	for _, entry := range g.documents {
		if entry.markdown == doc {
			return true
		}
	}
	return g.markdownView != nil && g.markdownView.doc == doc
}

// recordSavedFileStamp prevents our own write from being mistaken for an
// external change on the next check.
func (g *Gui) recordSavedFileStamp(uri fyne.URI) {
	if uri == nil || uri.Scheme() != "file" {
		return
	}
	stamp := stampForPath(uri.Path())
	if !stamp.valid {
		return
	}
	for _, entry := range g.documents {
		if entry == g.activeDocument || entry.target != nil && entry.target.Scheme() == "file" && entry.target.Path() == uri.Path() {
			entry.stamp = stamp
		}
	}
}
