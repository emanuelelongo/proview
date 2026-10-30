//go:build darwin

package gui

import (
	"emanuelelongo/proview/pkg/document"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFinderFilesReceivedDuringLaunchAreDeliveredOnceWhenReady(t *testing.T) {
	var inbox finderFileInbox
	inbox.receive([]string{"/tmp/first.jpeg"})
	inbox.receive([]string{"/tmp/second.pdf", "/tmp/third.png"})
	inbox.setCallback(nil)
	received := make(chan []string, 3)
	callback := func(files []string) { received <- files }
	inbox.setCallback(callback)
	select {
	case files := <-received:
		want := []string{"/tmp/first.jpeg", "/tmp/second.pdf", "/tmp/third.png"}
		if !reflect.DeepEqual(files, want) {
			t.Fatalf("files = %v, want %v", files, want)
		}
	case <-time.After(time.Second):
		t.Fatal("launch documents were lost")
	}
	inbox.setCallback(callback)
	inbox.receive(nil)
	inbox.receive([]string{"/tmp/later.jpeg"})
	select {
	case files := <-received:
		if !reflect.DeepEqual(files, []string{"/tmp/later.jpeg"}) {
			t.Fatalf("duplicate or unexpected files: %v", files)
		}
	case <-time.After(time.Second):
		t.Fatal("running application did not receive document")
	}
	select {
	case files := <-received:
		t.Fatalf("unexpected duplicate delivery: %v", files)
	default:
	}
}

func TestFinderLaunchSkipsSavedSessionAndOpensSelectedFilesTogether(t *testing.T) {
	g, driver := newDropTestGui(t)
	directory := t.TempDir()
	previous := filepath.Join(directory, "previous.md")
	first := filepath.Join(directory, "first.md")
	second := filepath.Join(directory, "second.md")
	for _, path := range []string{previous, first, second} {
		if err := os.WriteFile(path, []byte("# "+filepath.Base(path)+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	g.app.Preferences().SetStringList(sessionPreferenceKey, []string{previous})
	finderOpenHandler = finderFileInbox{}
	if !g.deferSessionRestoreForFinder(nil) {
		t.Fatal("Finder launch was not deferred")
	}
	finderOpenHandler.finishLaunch()
	finderOpenHandler.receive([]string{first})
	time.Sleep(finderBatchDelay / 2)
	finderOpenHandler.receive([]string{second})
	for len(g.documents) < 2 || g.openingFiles || len(g.pendingFiles) > 0 {
		driver.runNext(t)
	}
	if len(g.editors.windows) != 1 || len(g.documents) != 2 || g.documents[0].target.Path() != first || g.documents[1].target.Path() != second {
		t.Fatalf("Finder batch opened %d documents across %d windows", len(g.documents), len(g.editors.windows))
	}
	g.closeWindow()
}

func TestRunningMacGroupsSeparateFinderEventsInOneWorkspace(t *testing.T) {
	g, driver := newDropTestGui(t)
	directory := t.TempDir()
	first := filepath.Join(directory, "first.md")
	second := filepath.Join(directory, "second.png")
	if err := os.WriteFile(first, []byte("# First\n"), 0600); err != nil {
		t.Fatal(err)
	}
	image := testDocument(t, 12, 8)
	png, err := image.Export(document.ExportOptions{Format: document.FormatPNG})
	image.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, png, 0600); err != nil {
		t.Fatal(err)
	}
	g.addDocument(testDocument(t, 10, 10), nil, nil)
	finderOpenHandler = finderFileInbox{}
	finderOpenHandler.setCallback(g.openFilesFromFinder)
	finderOpenHandler.receive([]string{first})
	time.Sleep(finderBatchDelay / 2)
	finderOpenHandler.receive([]string{second})
	for len(g.editors.windows) < 2 || len(g.editors.windows[1].documents) < 2 || g.editors.windows[1].openingFiles {
		driver.runNext(t)
	}
	other := g.editors.windows[1]
	if len(g.editors.windows) != 2 || len(g.documents) != 1 || len(other.documents) != 2 || other.documents[0].target.Path() != first || other.documents[1].target.Path() != second {
		t.Fatalf("Finder events opened %d windows with %d documents in the new window", len(g.editors.windows), len(other.documents))
	}
	other.closeWindow()
	g.closeWindow()
}

func TestPlainMacLaunchRestoresSavedSessionAfterLaunch(t *testing.T) {
	g, driver := newDropTestGui(t)
	previous := filepath.Join(t.TempDir(), "previous.md")
	if err := os.WriteFile(previous, []byte("# Previous session\n"), 0600); err != nil {
		t.Fatal(err)
	}
	g.app.Preferences().SetStringList(sessionPreferenceKey, []string{previous})
	finderOpenHandler = finderFileInbox{}
	if !g.deferSessionRestoreForFinder(nil) {
		t.Fatal("plain macOS launch was not deferred")
	}
	finderOpenHandler.finishLaunch()
	for len(g.documents) == 0 || g.openingFiles || len(g.pendingFiles) > 0 {
		driver.runNext(t)
	}
	if len(g.documents) != 1 || g.documents[0].target.Path() != previous {
		t.Fatalf("plain launch restored %d documents", len(g.documents))
	}
	g.closeWindow()
}

func TestFinderLaunchDocumentsTakePriorityOverSavedSession(t *testing.T) {
	var inbox finderFileInbox
	launch := make(chan []string, 1)
	later := make(chan []string, 1)
	inbox.startLaunch(func(files []string) { launch <- files })
	inbox.setCallback(func(files []string) { later <- files })
	inbox.receive([]string{"/tmp/first.png"})
	inbox.receive([]string{"/tmp/second.md"})
	select {
	case files := <-later:
		t.Fatalf("launch file dispatched as a later request: %v", files)
	default:
	}
	inbox.finishLaunch()
	select {
	case files := <-launch:
		if !reflect.DeepEqual(files, []string{"/tmp/first.png", "/tmp/second.md"}) {
			t.Fatalf("launch files = %v", files)
		}
	case <-time.After(time.Second):
		t.Fatal("Finder launch file was not delivered")
	}
	inbox.receive([]string{"/tmp/later.pdf"})
	select {
	case files := <-later:
		if !reflect.DeepEqual(files, []string{"/tmp/later.pdf"}) {
			t.Fatalf("later files = %v", files)
		}
	case <-time.After(time.Second):
		t.Fatal("later Finder request was not delivered")
	}

	var empty finderFileInbox
	emptyLaunch := make(chan []string, 1)
	empty.startLaunch(func(files []string) { emptyLaunch <- files })
	empty.finishLaunch()
	select {
	case files := <-emptyLaunch:
		if len(files) != 0 {
			t.Fatalf("plain launch unexpectedly opened %v", files)
		}
	case <-time.After(time.Second):
		t.Fatal("plain launch did not finish")
	}
}
