package gui

import (
	"bytes"
	"emanuelelongo/proview/pkg/document"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func tapDialogButton(t *testing.T, g *Gui, label string) {
	t.Helper()
	overlay := g.window.Canvas().Overlays().Top()
	if overlay == nil {
		t.Fatal("no dialog shown")
	}
	for _, object := range fyneTest.LaidOutObjects(overlay) {
		if button, ok := object.(*widget.Button); ok && button.Text == label {
			fyneTest.Tap(button)
			return
		}
	}
	t.Fatalf("missing dialog button %q", label)
}

func TestCloseOffersSaveDiscardAndCancel(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.showDocument(testDocument(t, 40, 20))
	g.document.MarkUnsaved()
	g.window.Show()
	baseline := len(app.Driver().AllWindows())
	g.requestClose()
	labels := map[string]bool{}
	for _, object := range fyneTest.LaidOutObjects(g.window.Canvas().Overlays().Top()) {
		if button, ok := object.(*widget.Button); ok {
			labels[button.Text] = true
		}
	}
	for _, label := range []string{"Save", "Don't save", "Cancel"} {
		if !labels[label] {
			t.Fatalf("missing %s", label)
		}
	}
	tapDialogButton(t, g, "Cancel")
	if g.document == nil || !g.document.Dirty() || len(app.Driver().AllWindows()) != baseline {
		t.Fatal("Cancel closed or discarded the document")
	}
	g.requestClose()
	tapDialogButton(t, g, "Don't save")
	if g.document != nil || len(app.Driver().AllWindows()) != baseline-1 {
		t.Fatal("Don't save did not close the window")
	}
}

func TestSaveCompletionClosesOnlyAfterSuccessfulWrite(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.showDocument(testDocument(t, 40, 20))
	g.document.MarkUnsaved()
	g.window.Show()
	data, err := g.exportForName("saved.png")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	previousDirectory := t.TempDir()
	app.Preferences().SetString(lastSaveDirectoryKey, previousDirectory)
	closed := false
	afterSave := func() { closed = true; g.requestClose() }
	g.finishFileSave(data, storage.NewFileURI(filepath.Join(root, "missing", "failed.png")), g.document.Revision(), afterSave)
	if closed || g.document == nil || !g.document.Dirty() {
		t.Fatal("failed write closed the window or marked changes saved")
	}
	if app.Preferences().String(lastSaveDirectoryKey) != previousDirectory {
		t.Fatal("failed save changed the remembered directory")
	}
	tapDialogButton(t, g, "OK")
	path := filepath.Join(root, "saved.png")
	g.finishFileSave(data, storage.NewFileURI(path), g.document.Revision(), afterSave)
	if !closed || g.document != nil {
		t.Fatal("successful save did not finish closing")
	}
	if saved, err := os.ReadFile(path); err != nil || len(saved) == 0 {
		t.Fatalf("saved file missing: %v", err)
	}
}

func TestCancellingSaveAsDoesNotRunCloseCompletion(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.showDocument(testDocument(t, 40, 20))
	g.document.MarkUnsaved()
	g.window.Show()
	exports, err := g.prepareExports()
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	g.showSaveAsDialog(exports, func() { closed = true; g.closeWindow() })
	tapDialogButton(t, g, "Cancel")
	if closed || g.document == nil || !g.document.Dirty() {
		t.Fatal("cancelling Save As closed or discarded the document")
	}
}

func TestReplacingDocumentOffersSaveDiscardAndCancel(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.showDocument(testDocument(t, 40, 20))
	g.document.MarkUnsaved()
	g.window.Show()
	continued := false
	action := func() { continued = true }
	g.confirmDiscard(action)
	labels := map[string]bool{}
	for _, object := range fyneTest.LaidOutObjects(g.window.Canvas().Overlays().Top()) {
		if button, ok := object.(*widget.Button); ok {
			labels[button.Text] = true
		}
	}
	for _, label := range []string{"Save", "Don't save", "Cancel"} {
		if !labels[label] {
			t.Fatalf("missing %s when replacing document", label)
		}
	}
	tapDialogButton(t, g, "Cancel")
	if continued || !g.document.Dirty() {
		t.Fatal("Cancel continued with document replacement")
	}
	g.confirmDiscard(action)
	tapDialogButton(t, g, "Don't save")
	if !continued {
		t.Fatal("Don't save did not continue with replacement")
	}
}

func TestCloseDocumentKeepsWindowAndResetsEditor(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.window.Show()
	fileMenu := g.window.MainMenu().Items[0]
	var closeItem *fyne.MenuItem
	for _, item := range fileMenu.Items {
		if item.Label == "Close" {
			closeItem = item
		}
		if item.Label == "Quit" {
			t.Fatal("duplicate quit action")
		}
	}
	if closeItem == nil || !closeItem.Disabled || !fileMenu.Items[len(fileMenu.Items)-1].IsQuit {
		t.Fatal("incorrect Close/Exit menu state")
	}
	g.showDocument(testDocument(t, 40, 20))
	g.document.MarkUnsaved()
	g.saveTarget = storage.NewFileURI("old.png")
	g.zoomIn()
	baseline := len(app.Driver().AllWindows())
	closeItem.Action()
	tapDialogButton(t, g, "Cancel")
	if g.document == nil || closeItem.Disabled {
		t.Fatal("Cancel discarded the document")
	}
	closeItem.Action()
	tapDialogButton(t, g, "Don't save")
	if g.document != nil || g.closed || len(app.Driver().AllWindows()) != baseline {
		t.Fatal("Close did not preserve the editor window")
	}
	if !closeItem.Disabled || g.saveTarget != nil || g.image.Image != nil || g.zoomLevel != 0 || g.imageSignatureLayer.document != nil {
		t.Fatal("Close left stale editor state")
	}
	g.showDocument(testDocument(t, 60, 30))
	if closeItem.Disabled || g.document == nil || g.image.Image == nil {
		t.Fatal("editor cannot open a document after Close")
	}
	g.requestCloseDocument()
	if g.document != nil || g.closed {
		t.Fatal("clean document did not close immediately")
	}
}

func TestClosePDFClearsPagesAndAllowsReopening(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.window.Show()
	source, err := document.Load("pages.pdf", bytes.NewReader(scrollTestPDF(2)))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	g.showDocument(doc)
	generation := g.pdfGeneration
	g.requestCloseDocument()
	if g.document != nil || len(g.pdfPages.Objects) != 0 || len(g.thumbList.Objects) != 0 ||
		g.pdfScroll.Visible() || g.sidebar.container.Visible() || g.pdfGeneration <= generation {
		t.Fatal("Close left PDF pages or pending render generation active")
	}
	g.showDocument(testDocument(t, 40, 20))
	if g.document == nil || !g.imageScroll.Visible() || g.pdfScroll.Visible() {
		t.Fatal("opening an image after closing a PDF failed")
	}
	g.requestCloseDocument()
}
