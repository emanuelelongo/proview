package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emanuelelongo/proview/pkg/document"
)

func TestCleanActiveFileReloadsAfterExternalChange(t *testing.T) {
	g, driver := newDropTestGui(t)
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("# First version\n"), 0600); err != nil {
		t.Fatal(err)
	}
	g.openStartupFiles([]string{path})
	for g.markdownView == nil || g.openingFiles || len(g.pendingFiles) > 0 {
		driver.runNext(t)
	}
	if g.reloadFileItem.Disabled {
		t.Fatal("reload item disabled for an open file")
	}
	if err := os.WriteFile(path, []byte("# Second version with more text\n"), 0600); err != nil {
		t.Fatal(err)
	}
	g.checkActiveFileChange()
	for attempts := 0; !strings.Contains(string(g.markdownView.doc.Source), "Second version"); attempts++ {
		if attempts == 10 {
			t.Fatal("clean file was not reloaded")
		}
		driver.runNext(t)
	}
	if g.document != nil || !g.showingMarkdown() {
		t.Fatal("reload changed the active document type")
	}
	g.closeWindow()
}

func TestDirtyFileChangePromptsBeforeReload(t *testing.T) {
	g, driver := newDropTestGui(t)
	path := filepath.Join(t.TempDir(), "image.png")
	first := testDocument(t, 12, 8)
	data, err := first.Export(document.ExportOptions{Format: document.FormatPNG})
	first.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	g.openStartupFiles([]string{path})
	for g.document == nil || g.openingFiles || len(g.pendingFiles) > 0 {
		driver.runNext(t)
	}
	g.document.MarkUnsaved()
	originalStamp := stampForPath(path)
	second := testDocument(t, 24, 16)
	replacement, err := second.Export(document.ExportOptions{Format: document.FormatPNG})
	second.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, replacement, 0600); err != nil {
		t.Fatal(err)
	}
	if stampForPath(path) == originalStamp {
		t.Fatal("test file stamp did not change")
	}
	before := g.document
	g.checkActiveFileChange()
	for attempts := 0; g.window.Canvas().Overlays().Top() == nil; attempts++ {
		if attempts == 10 {
			t.Fatal("dirty file change did not prompt")
		}
		driver.runNext(t)
	}
	if g.document != before || !g.document.Dirty() {
		t.Fatal("dirty document changed before confirmation")
	}
	tapDialogButton(t, g, "Reload")
	for g.document == before || g.working {
		driver.runNext(t)
	}
	if g.document.Dirty() || g.document.SourceSize() != int64(len(replacement)) {
		t.Fatal("confirmed reload did not replace the dirty document")
	}
	g.closeWindow()
}
