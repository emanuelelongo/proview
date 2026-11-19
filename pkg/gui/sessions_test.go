package gui

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/storage"
)

func TestEmptyLaunchWaitsForExplicitRestoreAndExplicitFilesTakePriority(t *testing.T) {
	g, driver := newDropTestGui(t)
	path := filepath.Join(t.TempDir(), "reading.md")
	if err := os.WriteFile(path, []byte("# Previous session\n"), 0600); err != nil {
		t.Fatal(err)
	}
	g.app.Preferences().SetStringList(sessionPreferenceKey, []string{path})
	g.updateHistoryActions()
	if g.reopenSessionItem == nil || g.reopenSessionItem.Disabled {
		t.Fatal("saved session was not offered in the File menu")
	}
	other := g.newEditor()
	other.openLaunchRequest(nil, OpenOptions{ReuseWindow: true})
	for other.openingFiles || len(other.pendingFiles) > 0 {
		driver.runNext(t)
	}
	if other.hasOpenContent() || len(other.documents) != 0 {
		t.Fatal("no-file launch reopened the previous session")
	}
	if other.reopenSessionItem.Disabled {
		t.Fatal("restore item disabled with a saved session")
	}
	other.reopenLastSession()
	for len(other.documents) == 0 || other.openingFiles || len(other.pendingFiles) > 0 {
		driver.runNext(t)
	}
	if !other.showingMarkdown() || len(other.documents) != 1 {
		t.Fatal("explicit restore did not reopen the previous session")
	}
	// An occupied workspace keeps its documents and restores into a new window.
	before := len(driver.AllWindows())
	other.reopenLastSession()
	for other.desktopEditor() == other {
		driver.runNext(t)
	}
	restored := other.desktopEditor()
	for restored.openingFiles || len(restored.pendingFiles) > 0 || len(restored.documents) == 0 {
		driver.runNext(t)
	}
	if restored == other || len(driver.AllWindows()) != before+1 || len(other.documents) != 1 || len(restored.documents) != 1 {
		t.Fatal("restore did not use a separate window for occupied content")
	}
	// Explicit launch files never fall back to the saved session.
	explicitPath := filepath.Join(t.TempDir(), "explicit.md")
	if err := os.WriteFile(explicitPath, []byte("# Explicit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	explicit := other.newEditor()
	explicit.openLaunchRequest([]string{explicitPath}, OpenOptions{ReuseWindow: true})
	for len(explicit.documents) == 0 || explicit.openingFiles || len(explicit.pendingFiles) > 0 {
		driver.runNext(t)
	}
	if len(explicit.documents) != 1 || explicit.documents[0].target.Path() != explicitPath {
		t.Fatalf("explicit launch opened %v", explicit.documents)
	}
	for _, window := range append([]*Gui(nil), g.editors.windows...) {
		window.closeWindow()
	}
}

func TestSessionLastWriterWinsAndEmptyWindowKeepsPreviousSet(t *testing.T) {
	g, _ := newDropTestGui(t)
	other := g.newEditor()
	firstPath := filepath.Join(t.TempDir(), "first.png")
	lastPath := filepath.Join(t.TempDir(), "last.png")
	g.addDocument(testDocument(t, 10, 10), nil, storage.NewFileURI(firstPath))
	other.addDocument(testDocument(t, 10, 10), nil, storage.NewFileURI(lastPath))
	g.closeWindow()
	if paths := other.savedSessionPaths(); len(paths) != 1 || paths[0] != firstPath {
		t.Fatalf("first session = %v", paths)
	}
	last := other.newEditor()
	other.closeWindow()
	if paths := last.savedSessionPaths(); len(paths) != 1 || paths[0] != lastPath {
		t.Fatalf("last session = %v", paths)
	}
	last.closeWindow()
	if paths := last.savedSessionPaths(); len(paths) != 1 || paths[0] != lastPath {
		t.Fatalf("empty window cleared session: %v", paths)
	}
}

func TestSessionItemDisabledWithoutSavedFiles(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.updateHistoryActions()
	if g.reopenSessionItem == nil || !g.reopenSessionItem.Disabled {
		t.Fatal("restore item enabled without a saved session")
	}
	g.closeWindow()
}

func TestCommandLineOpeningReusesOccupiedWorkspace(t *testing.T) {
	g, driver := newDropTestGui(t)
	path := filepath.Join(t.TempDir(), "incoming.md")
	if err := os.WriteFile(path, []byte("# Incoming\n"), 0600); err != nil {
		t.Fatal(err)
	}
	existing := testDocument(t, 10, 10)
	existing.MarkUnsaved()
	g.addDocument(existing, nil, nil)
	g.working = true
	g.openLaunchRequest([]string{path}, OpenOptions{ReuseWindow: true})
	if len(g.editors.windows) != 1 || len(g.pendingFiles) != 1 {
		t.Fatal("busy CLI request was not queued in existing window")
	}
	g.working = false
	g.processPendingFiles()
	for g.openingFiles || len(g.pendingFiles) > 0 {
		driver.runNext(t)
	}
	if len(g.editors.windows) != 1 || len(g.documents) != 2 || !g.showingMarkdown() || !existing.Dirty() {
		t.Fatal("CLI opening did not retain existing workspace")
	}
	g.closeWindow()
}

func TestSessionCanceledCloseKeepsPreviousFileSet(t *testing.T) {
	g, _ := newDropTestGui(t)
	previous := filepath.Join(t.TempDir(), "previous.md")
	g.app.Preferences().SetStringList(sessionPreferenceKey, []string{previous})
	doc := testDocument(t, 10, 10)
	doc.MarkUnsaved()
	g.addDocument(doc, nil, storage.NewFileURI(filepath.Join(t.TempDir(), "current.png")))
	g.requestClose()
	tapDialogButton(t, g, "Cancel")
	if g.closed {
		t.Fatal("cancel closed the window")
	}
	if paths := g.savedSessionPaths(); len(paths) != 1 || paths[0] != previous {
		t.Fatalf("canceled close replaced previous session: %v", paths)
	}
	g.closeWindow()
}
