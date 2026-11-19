package gui

import "path/filepath"

const sessionPreferenceKey = "workspace.previousFiles"

// Each window saves its file set on close. The last writer wins. An empty
// workspace does not clear the remembered set, so launching Proview without
// files and closing the empty window keeps Reopen Last Session available.
// Document edits and history remain outside the session.
func (g *Gui) saveSession() {
	g.captureDocument()
	paths := make([]string, 0, len(g.documents))
	for _, entry := range g.documents {
		if entry.target == nil || entry.target.Scheme() != "file" {
			continue
		}
		path, err := filepath.Abs(entry.target.Path())
		if err == nil {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return
	}
	g.app.Preferences().SetStringList(sessionPreferenceKey, paths)
	if g.editors == nil {
		g.updateRestoreSessionItem()
		return
	}
	for _, editor := range g.editors.windows {
		editor.updateRestoreSessionItem()
	}
}

func (g *Gui) savedSessionPaths() []string {
	return append([]string(nil), g.app.Preferences().StringList(sessionPreferenceKey)...)
}

func (g *Gui) updateRestoreSessionItem() {
	if g.reopenSessionItem != nil {
		g.reopenSessionItem.Disabled = len(g.savedSessionPaths()) == 0
	}
}

// reopenLastSession reopens the remembered file set. It uses the current
// workspace when empty and creates a fresh window when the user already has
// content, matching the routing of explicit desktop file requests.
func (g *Gui) reopenLastSession() {
	paths := g.savedSessionPaths()
	if len(paths) == 0 || g.closed {
		return
	}
	if g.hasOpenContent() || g.working || g.openingFiles || len(g.pendingFiles) > 0 {
		editor := g.newEditor()
		editor.prepareWindowOpening(paths)
		editor.openStartupFiles(paths)
		return
	}
	g.openStartupFiles(paths)
}
