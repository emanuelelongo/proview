package gui

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"emanuelelongo/proview/pkg/document"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestStartupFileReaderUsesTheSharedDocumentLoader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image with spaces.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	input := image.NewNRGBA(image.Rect(0, 0, 12, 8))
	input.SetNRGBA(3, 4, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(file, input); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	reader, err := startupFileReader(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := loadDocumentReader(reader)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	if doc.Name() != filepath.Base(path) || doc.Format() != document.FormatPNG || doc.Location() == "" {
		t.Fatalf("loaded startup document = name %q, format %q, location %q", doc.Name(), doc.Format(), doc.Location())
	}
}

func TestStartupFileReaderRejectsMissingFile(t *testing.T) {
	reader, err := startupFileReader(filepath.Join(t.TempDir(), "missing.pdf"))
	if err == nil || reader != nil {
		t.Fatalf("missing startup file returned reader %v and error %v", reader, err)
	}
}

func TestOpenStartupFilesPreservesExistingDocumentWhenPathIsMissing(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	existing := testDocument(t, 20, 20)
	g.showDocument(existing)

	g.openStartupFiles([]string{filepath.Join(t.TempDir(), "missing.pdf")})

	if g.document != existing {
		t.Fatal("opening a missing file replaced the current document")
	}
}

func TestStartupJPEGFileReaderHandlesFinderPaths(t *testing.T) {
	for _, name := range []string{"IMG_2064.jpeg", "image with spaces.JPG", "immagine à 東京.jpeg"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := jpeg.Encode(file, image.NewRGBA(image.Rect(0, 0, 12, 8)), nil); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			reader, err := startupFileReader(path)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := loadDocumentReader(reader)
			if err != nil {
				t.Fatal(err)
			}
			defer doc.Close()
			if doc.Format() != document.FormatJPEG || doc.Name() != name {
				t.Fatalf("loaded name %q, format %q", doc.Name(), doc.Format())
			}
		})
	}
}

func TestNewWindowLaunchKeepsExistingWorkspaceAndAppliesWindowDirectory(t *testing.T) {
	g, driver := newDropTestGui(t)
	existing := testDocument(t, 20, 20)
	existing.MarkUnsaved()
	g.addDocument(existing, nil, nil)
	originalDirectory := t.TempDir()
	g.workDir = originalDirectory
	directory := t.TempDir()
	before := len(driver.AllWindows())
	g.OpenFilesWithOptions(nil, OpenOptions{NewWindow: true, WorkDir: directory})
	for g.desktopEditor() == g {
		driver.runNext(t)
	}
	editor := g.desktopEditor()
	if editor == g || len(driver.AllWindows()) != before+1 || editor.app != g.app || editor.document != nil || editor.workDir != directory {
		t.Fatal("launch did not create a dedicated empty window")
	}
	if g.document != existing || !existing.Dirty() || len(g.documents) != 1 || g.workDir != originalDirectory {
		t.Fatal("new-window altered the existing editor")
	}
	if editor.initialOpenDirectory() != directory || editor.initialSaveDirectory() != directory {
		t.Fatal("new window did not apply dialog directory")
	}
	inherited := editor.newEditor()
	if inherited.workDir != directory {
		t.Fatal("child editor lost the session directory")
	}
	previous := g.desktopEditor()
	g.OpenFilesWithOptions(nil, OpenOptions{NewWindow: true})
	for g.desktopEditor() == previous {
		driver.runNext(t)
	}
	if g.desktopEditor().workDir != "" {
		t.Fatal("a separate launch inherited another window's work directory")
	}
	for _, window := range append([]*Gui(nil), g.editors.windows...) {
		window.closeWindow()
	}
}

func TestWorkDirOnlyLaunchUsesCurrentEditorAndOrdinaryFilesKeepRouting(t *testing.T) {
	g, driver := newDropTestGui(t)
	directory := t.TempDir()
	before := len(driver.AllWindows())
	g.OpenFilesWithOptions(nil, OpenOptions{WorkDir: directory})
	for g.workDir != directory {
		driver.runNext(t)
	}
	if g.desktopEditor() != g || len(driver.AllWindows()) != before || g.workDir != directory {
		t.Fatal("work-dir-only launch created an extra window")
	}
	path := filepath.Join(directory, "test.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(file, image.NewNRGBA(image.Rect(0, 0, 12, 8))); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	g.OpenFilesWithOptions([]string{path}, OpenOptions{NewWindow: true, WorkDir: directory})
	for g.desktopEditor() == g {
		driver.runNext(t)
	}
	editor := g.desktopEditor()
	for editor.openingFiles || len(editor.pendingFiles) > 0 {
		driver.runNext(t)
	}
	if editor == g || editor.document == nil || g.document != nil || editor.workDir != directory {
		t.Fatal("forwarded files did not open in the requested window")
	}
	for _, window := range append([]*Gui(nil), g.editors.windows...) {
		window.closeWindow()
	}
}

func TestDesktopFileRequestsReuseEmptyWindowThenOpenSeparateWorkspace(t *testing.T) {
	g, driver := newDropTestGui(t)
	path := filepath.Join(t.TempDir(), "incoming.png")
	doc := testDocument(t, 12, 8)
	data, err := doc.Export(document.ExportOptions{Format: document.FormatPNG})
	doc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	before := len(driver.AllWindows())
	g.OpenFiles([]string{path})
	for g.document == nil {
		driver.runNext(t)
	}
	for g.openingFiles {
		driver.runNext(t)
	}
	if g.document == nil || len(driver.AllWindows()) != before {
		t.Fatal("desktop request did not reuse the empty editor")
	}
	existing := g.document
	g.OpenFiles([]string{path, path})
	for g.desktopEditor() == g {
		driver.runNext(t)
	}
	opened := g.desktopEditor()
	for opened.openingFiles || len(opened.pendingFiles) > 0 {
		driver.runNext(t)
	}
	if g.document != existing || len(g.documents) != 1 || len(opened.documents) != 2 || len(driver.AllWindows()) != before+1 {
		t.Fatal("desktop batch did not use its own workspace")
	}
	opened.closeWindow()
	g.closeWindow()
}
