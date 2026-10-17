package gui

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	fyneTest "fyne.io/fyne/v2/test"
)

func workspacePDF(t *testing.T, pages int) *document.Document {
	t.Helper()
	source, err := document.Load("pages.pdf", bytes.NewReader(scrollTestPDF(pages)))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	return doc
}

func TestDocumentSwitchRetainsHistoryDestinationAndPDFPage(t *testing.T) {
	g, driver := newDropTestGui(t)
	pdf := workspacePDF(t, 3)
	target := storage.NewFileURI(filepath.Join(t.TempDir(), "first.pdf"))
	g.addDocument(pdf, nil, target)
	g.activatePDFPage(2)
	g.zoom = 1.5
	g.zoomLevel = 2
	g.applyZoom()
	g.pdfScroll.ScrollToOffset(fyne.NewPos(0, g.pdfPageTop(2)))
	g.activatePDFPage(2)
	redacted := map[string][]string{"destination": {"mark"}}
	g.redactedTargets = redacted
	if err := pdf.DuplicatePage(0); err != nil {
		t.Fatal(err)
	}
	image := testDocument(t, 20, 30)
	g.addDocument(image, nil, nil)
	first, second := g.documents[0], g.documents[1]
	if !g.sidebar.panel.Visible() || !g.sidebar.documents.Visible() {
		t.Fatal("document selector is hidden")
	}
	g.selectDocument(first)
	if g.document != pdf || g.saveTarget != target || !pdf.Dirty() || !pdf.CanUndo() || g.activePDFPage != 2 || g.zoom != 1.5 || len(g.redactedTargets) != 1 {
		t.Fatalf("state doc=%p want=%p target=%v want=%v dirty=%v undo=%v page=%d zoom=%v targets=%v entry=%+v", g.document, pdf, g.saveTarget, target, pdf.Dirty(), pdf.CanUndo(), g.activePDFPage, g.zoom, g.redactedTargets, first)
	}
	g.undo()
	for g.working {
		driver.runNext(t)
	}
	if pdf.PageCount() != 3 || image.Dirty() {
		t.Fatal("Undo crossed document boundaries")
	}
	g.selectDocument(second)
	if g.document != image || g.saveTarget != nil || !g.sidebar.documents.Visible() || g.sidebar.container.Visible() {
		t.Fatal("image state or sidebar is incorrect")
	}
	g.closeDocument()
	if g.document != pdf || g.saveTarget != target || len(g.documents) != 1 {
		t.Fatal("closing did not restore the remaining document")
	}
	g.closeDocument()
	if g.hasOpenContent() || g.sidebar.panel.Visible() {
		t.Fatal("last document did not close")
	}
}

func TestPDFOpenChoices(t *testing.T) {
	for _, choice := range []string{"Cancel", "New window", "Append pages"} {
		t.Run(choice, func(t *testing.T) {
			g, driver := newDropTestGui(t)
			current, incoming := workspacePDF(t, 2), workspacePDF(t, 3)
			g.addDocument(current, nil, nil)
			baseline := len(driver.AllWindows())
			completed := 0
			g.receiveDocument(incoming, nil, storage.NewFileURI("/incoming.pdf"), func() { completed++ })
			if g.pdfOpenDialog == nil || completed != 0 {
				t.Fatal("PDF decision was skipped")
			}
			tapDialogButton(t, g, choice)
			if completed != 1 || g.pdfOpenDialog != nil || g.document != current {
				t.Fatal("PDF decision did not complete safely")
			}
			switch choice {
			case "Cancel":
				if current.PageCount() != 2 || current.Dirty() || len(driver.AllWindows()) != baseline {
					t.Fatal("cancel modified current PDF")
				}
			case "New window":
				if len(driver.AllWindows()) != baseline+1 || current.PageCount() != 2 || current.Dirty() {
					t.Fatal("new window modified current PDF")
				}
			case "Append pages":
				for g.working {
					driver.runNext(t)
				}
				if current.PageCount() != 5 || g.activePDFPage != 2 || !current.Dirty() {
					t.Fatal("append failed")
				}
				if !current.Undo() || current.PageCount() != 2 || current.CanUndo() {
					t.Fatal("append is not one Undo operation")
				}
				if !current.Redo() || current.PageCount() != 5 {
					t.Fatal("append Redo failed")
				}
				if _, err := current.Preview(4); err != nil {
					t.Fatal("transferred source was closed", err)
				}
			}
			current.Close()
			if choice == "New window" {
				incoming.Close()
			}
		})
	}
}

func TestWindowCloseChecksInactiveDocumentsAndCancelPreservesAll(t *testing.T) {
	g, _ := newDropTestGui(t)
	first, second := testDocument(t, 20, 20), testDocument(t, 30, 30)
	first.MarkUnsaved()
	second.MarkUnsaved()
	g.addDocument(first, nil, nil)
	g.addDocument(second, nil, nil)
	g.requestClose()
	tapDialogButton(t, g, "Don't save")
	tapDialogButton(t, g, "Cancel")
	if g.closed || len(g.documents) != 2 || !first.Dirty() || !second.Dirty() {
		t.Fatal("cancel discarded retained documents")
	}
	g.requestClose()
	tapDialogButton(t, g, "Don't save")
	tapDialogButton(t, g, "Don't save")
	if !g.closed || len(g.documents) != 0 {
		t.Fatal("window did not close all documents")
	}
}

func TestFinderOpeningUsesSharedDocumentWorkspace(t *testing.T) {
	g, driver := newDropTestGui(t)
	existing := testDocument(t, 20, 20)
	g.addDocument(existing, nil, nil)
	path := filepath.Join(t.TempDir(), "incoming.pdf")
	if err := os.WriteFile(path, scrollTestPDF(2), 0600); err != nil {
		t.Fatal(err)
	}
	g.openFilesFromFinder([]string{path})
	driver.runNext(t)
	for len(g.documents) < 2 || g.openingFiles {
		driver.runNext(t)
	}
	if len(g.documents) != 2 || g.document.Format() != document.FormatPDF {
		t.Fatalf("Finder entries=%d format=%v", len(g.documents), g.document.Format())
	}
	g.selectDocument(g.documents[0])
	if g.document != existing {
		t.Fatal("Finder request discarded earlier document")
	}
	g.closeRetainedDocuments()
	existing.Close()
}

func TestModelDocumentSwitchRestoresCameraAndImageEdits(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.modelViewport.running = true // Keep this test independent of native rendering.
	image := testDocument(t, 20, 20)
	image.MarkUnsaved()
	g.addDocument(image, nil, nil)
	mesh := testSTLMesh(t)
	g.addDocument(nil, mesh, nil)
	g.modelViewport.camera.Yaw = .7
	modelEntry := g.activeDocument
	g.selectDocument(g.documents[0])
	if g.document != image || !image.Dirty() {
		t.Fatal("model opening lost image changes")
	}
	g.selectDocument(modelEntry)
	if !g.showingModel() || g.modelViewport.mesh != mesh || g.modelViewport.camera.Yaw != .7 {
		t.Fatal("model state was not restored")
	}
	g.closeDocument()
	if g.document != image || g.showingModel() {
		t.Fatal("closing model lost previous document")
	}
	g.closeDocument()
}

func TestDesktopRequestsReuseSurvivingEditorAfterFirstWindowCloses(t *testing.T) {
	first, driver := newDropTestGui(t)
	second := first.newEditor()
	second.addDocument(testDocument(t, 20, 20), nil, nil)
	first.closeWindow()
	baseline := len(driver.AllWindows())
	if first.desktopEditor() != second {
		t.Fatal("closed root did not route to surviving editor")
	}
	path := filepath.Join(t.TempDir(), "incoming.pdf")
	if err := os.WriteFile(path, scrollTestPDF(1), 0600); err != nil {
		t.Fatal(err)
	}
	first.openFilesFromFinder([]string{path})
	for len(second.documents) != 2 || second.openingFiles {
		driver.runNext(t)
	}
	if second.document.Format() != document.FormatPDF || len(driver.AllWindows()) != baseline {
		t.Fatal("desktop request opened an unrelated window")
	}
	second.closeWindow()
}

func TestQueuedFilesWaitForPDFDecisionAndContinueAfterCancel(t *testing.T) {
	g, driver := newDropTestGui(t)
	current := workspacePDF(t, 2)
	g.addDocument(current, nil, nil)
	path := filepath.Join(t.TempDir(), "incoming.pdf")
	if err := os.WriteFile(path, scrollTestPDF(1), 0600); err != nil {
		t.Fatal(err)
	}
	g.openStartupFiles([]string{path, path})
	for g.pdfOpenDialog == nil {
		driver.runNext(t)
	}
	if len(g.pendingFiles) != 1 || !g.openingFiles {
		t.Fatal("batch did not wait for PDF choice")
	}
	tapDialogButton(t, g, "Cancel")
	for g.pdfOpenDialog == nil {
		driver.runNext(t)
	}
	tapDialogButton(t, g, "Cancel")
	if g.openingFiles || len(g.pendingFiles) != 0 || g.document != current || current.PageCount() != 2 || current.Dirty() {
		t.Fatal("cancel did not preserve PDF and finish batch")
	}
	g.closeDocument()
}

func TestDesktopRequestWaitsForFilePickerLoadCompletion(t *testing.T) {
	g, driver := newDropTestGui(t)
	path := filepath.Join(t.TempDir(), "first.pdf")
	if err := os.WriteFile(path, scrollTestPDF(1), 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := startupFileReader(path)
	if err != nil {
		t.Fatal(err)
	}
	BuildOpenFileCallback(g)(reader, nil)
	modelPath, err := filepath.Abs("../../internal/model3d/testdata/cube.stl")
	if err != nil {
		t.Fatal(err)
	}
	g.openStartupFiles([]string{modelPath})
	if len(g.pendingFiles) != 1 {
		t.Fatal("desktop request was lost during load")
	}
	for len(g.documents) < 2 || g.openingFiles {
		driver.runNext(t)
	}
	if !g.showingModel() || g.documents[0].doc == nil {
		t.Fatal("queued request did not preserve first document")
	}
	g.closeWindow()
}
