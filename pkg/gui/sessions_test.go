package gui

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/storage"
)

func TestSessionRestoresLastClosedWindowAndExplicitFilesTakePriority(t *testing.T) {
	g, driver := newDropTestGui(t)
	path := filepath.Join(t.TempDir(), "reading.md")
	if err := os.WriteFile(path, []byte("# Previous session\n"), 0600); err != nil {
		t.Fatal(err)
	}
	g.openStartupFiles([]string{path})
	for g.openingFiles || len(g.pendingFiles) > 0 {
		driver.runNext(t)
	}
	// An unsaved generated document has no restorable file path.
	g.addDocument(testDocument(t, 10, 10), nil, nil)
	other := g.newEditor()
	g.closeWindow()
	restored := other.launchPaths(nil)
	if len(restored) != 1 || restored[0] != path {
		t.Fatalf("restored paths = %v", restored)
	}
	explicit := other.launchPaths([]string{"explicit.md"})
	if len(explicit) != 1 || explicit[0] != "explicit.md" {
		t.Fatal("session replaced explicit file arguments")
	}
	other.openLaunchRequest(nil, OpenOptions{ReuseWindow: true})
	for other.openingFiles || len(other.pendingFiles) > 0 {
		driver.runNext(t)
	}
	if !other.showingMarkdown() || len(other.documents) != 1 {
		t.Fatal("no-file launch did not reopen previous session")
	}
	other.closeWindow()
}

func TestSessionLastWriterWinsIncludingEmptyWindow(t *testing.T) {
	g, _ := newDropTestGui(t)
	other := g.newEditor()
	firstPath := filepath.Join(t.TempDir(), "first.png")
	lastPath := filepath.Join(t.TempDir(), "last.png")
	g.addDocument(testDocument(t, 10, 10), nil, storage.NewFileURI(firstPath))
	other.addDocument(testDocument(t, 10, 10), nil, storage.NewFileURI(lastPath))
	g.closeWindow()
	if paths := other.launchPaths(nil); len(paths) != 1 || paths[0] != firstPath {
		t.Fatalf("first session = %v", paths)
	}
	last := other.newEditor()
	other.closeWindow()
	if paths := last.launchPaths(nil); len(paths) != 1 || paths[0] != lastPath {
		t.Fatalf("last session = %v", paths)
	}
	last.closeWindow()
	if paths := last.launchPaths(nil); len(paths) != 0 {
		t.Fatalf("empty window did not clear session: %v", paths)
	}
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
	if paths := g.launchPaths(nil); len(paths) != 1 || paths[0] != previous {
		t.Fatalf("canceled close replaced previous session: %v", paths)
	}
	g.closeWindow()
}
