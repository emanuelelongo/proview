package gui

import "path/filepath"

const sessionPreferenceKey = "workspace.previousFiles"

// Each window saves its file set on close. The last writer wins, including
// an empty set; document edits and history remain outside the session.
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
	g.app.Preferences().SetStringList(sessionPreferenceKey, paths)
}

func (g *Gui) launchPaths(paths []string) []string {
	if len(paths) != 0 {
		return paths
	}
	return append([]string(nil), g.app.Preferences().StringList(sessionPreferenceKey)...)
}
