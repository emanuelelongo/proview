package gui

import (
	"bytes"
	"emanuelelongo/proview/pkg/document"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/storage"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestSuggestedExportName(t *testing.T) {
	tests := map[string]string{
		"photo.PNG":    "Untitled.png",
		"document.pdf": "Untitled.pdf",
		"untitled":     "Untitled.png",
	}
	for input, want := range tests {
		if got := suggestedExportName(input); got != want {
			t.Errorf("suggestedExportName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPrepareImageExports(t *testing.T) {
	g := &Gui{document: testDocument(t, 40, 20)}
	exports, err := g.prepareExports()
	if err != nil {
		t.Fatal(err)
	}
	if len(exports.data[document.FormatPNG]) == 0 || len(exports.data[document.FormatJPEG]) == 0 {
		t.Fatal("Save As did not prepare both image formats")
	}
	if _, err := png.Decode(bytes.NewReader(exports.data[document.FormatPNG])); err != nil {
		t.Fatalf("prepared PNG is invalid: %v", err)
	}
	if _, err := g.exportForName("invalid.pdf"); err == nil {
		t.Fatal("image export unexpectedly accepted a PDF target")
	}
}

func TestSuccessfulSaveIsSilentAndUpdatesCurrentTarget(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.showDocument(testDocument(t, 40, 20))
	g.document.MarkUnsaved()
	data, err := g.exportForName("saved.png")
	if err != nil {
		t.Fatal(err)
	}
	target := storage.NewFileURI(filepath.Join(t.TempDir(), "saved.png"))
	g.finishFileSave(data, target, g.document.Revision(), nil)
	if g.saveTarget.String() != target.String() || g.document.Dirty() {
		t.Fatal("save did not update the current destination and saved revision")
	}
	if g.window.Canvas().Overlays().Top() != nil {
		t.Fatal("successful save displayed a confirmation dialog")
	}
	wantDirectory := filepath.Dir(target.Path())
	if got := app.Preferences().String(lastSaveDirectoryKey); got != wantDirectory {
		t.Fatalf("remembered save directory = %q, want %q", got, wantDirectory)
	}
	other := newGui(app)
	other.saveTarget = storage.NewFileURI(filepath.Join(t.TempDir(), "opened.png"))
	if got := other.initialSaveDirectory(); got != wantDirectory {
		t.Fatalf("another window starts in %q, want last save directory %q", got, wantDirectory)
	}
}

func TestSaveDirectoryFallsBackWhenRememberedFolderIsUnavailable(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	directory := t.TempDir()
	g.saveTarget = storage.NewFileURI(filepath.Join(directory, "opened.png"))
	for _, remembered := range []string{"", filepath.Join(directory, "missing")} {
		app.Preferences().SetString(lastSaveDirectoryKey, remembered)
		if got := g.initialSaveDirectory(); got != directory {
			t.Fatalf("save directory = %q, want current document folder %q", got, directory)
		}
	}
	g.saveTarget = nil
	home, _ := os.UserHomeDir()
	if got := g.initialSaveDirectory(); got != home {
		t.Fatalf("save directory = %q, want home folder %q", got, home)
	}
}

func TestSignatureLayerZoomChangesMinimumSize(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	doc := testDocument(t, 200, 100)
	layer := newSignatureLayer(doc, 0, image.Pt(200, 100), nil, nil, nil)
	layer.SetZoom(2)
	if got := layer.MinSize(); got.Width != 400 || got.Height != 200 {
		t.Fatalf("zoomed layer minimum size = %v, want 400x200", got)
	}
}

func TestImportedWebPExportsToWritableImageFormats(t *testing.T) {
	data, err := os.ReadFile("../document/testdata/small.webp")
	if err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("image.webp", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	defer doc.Close()
	g := &Gui{document: doc}
	exports, err := g.prepareExports()
	if err != nil {
		t.Fatal(err)
	}
	if len(exports.data) != 2 || len(exports.data[document.FormatPNG]) == 0 || len(exports.data[document.FormatJPEG]) == 0 {
		t.Fatal("imported WebP did not offer PNG/JPEG export")
	}
	if _, err := g.exportForName("image.webp"); err == nil {
		t.Fatal("export could overwrite import-only WebP")
	}
	if _, err := g.exportForName("image.png"); err != nil {
		t.Fatal(err)
	}
	if suggestedExportName("image.webp") != "Untitled.png" {
		t.Fatal("Save As suggests an unsupported extension")
	}
}
