package gui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	fyneTest "fyne.io/fyne/v2/test"
)

// Run UI callbacks on the test goroutine, including asynchronous load completion.
type dropTestDriver struct {
	fyne.Driver
	callbacks chan func()
}

func (d *dropTestDriver) DoFromGoroutine(callback func(), _ bool) {
	d.callbacks <- callback
}

func (d *dropTestDriver) runNext(t *testing.T) {
	t.Helper()
	select {
	case callback := <-d.callbacks:
		callback()
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for file drop processing")
	}
}

type dropTestApp struct {
	fyne.App
	driver *dropTestDriver
}

func (a *dropTestApp) Driver() fyne.Driver { return a.driver }

func newDropTestGui(t *testing.T) (*Gui, *dropTestDriver) {
	t.Helper()
	base := fyneTest.NewApp()
	driver := &dropTestDriver{Driver: base.Driver(), callbacks: make(chan func(), 16)}
	app := &dropTestApp{App: base, driver: driver}
	fyne.SetCurrentApp(app)
	t.Cleanup(app.Quit)
	return newGui(app), driver
}

func TestDroppedFilesOpenWithoutReplacingCurrentWork(t *testing.T) {
	for _, mode := range []string{"empty", "dirty", "busy", "multiple"} {
		t.Run(mode, func(t *testing.T) {
			g, driver := newDropTestGui(t)
			if mode == "dirty" {
				g.showDocument(testDocument(t, 20, 20))
				g.document.MarkUnsaved()
			}
			g.working = mode == "busy"
			existing := g.document
			g.window.Show()
			baseline := len(driver.AllWindows())
			count := 1
			if mode == "multiple" {
				count = 2
			}
			var uris []fyne.URI
			for i := 0; i < count; i++ {
				path := filepath.Join(t.TempDir(), "image à 東京 #%.png")
				file, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(file, image.NewNRGBA(image.Rect(0, 0, 12, 8))); err != nil {
					file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				uris = append(uris, storage.NewFileURI(path))
			}
			g.openDroppedFiles(fyne.NewPos(500, 300), uris)
			loadedWindows := func() int {
				loaded := 0
				for _, window := range driver.AllWindows() {
					if window.Title() == uris[0].Name()+" — Proview" {
						loaded++
					}
				}
				return loaded
			}
			for loadedWindows() < count {
				driver.runNext(t)
			}
			wantWindows := baseline + count
			if mode == "empty" || mode == "multiple" {
				wantWindows--
				if g.document == nil || g.document.Name() != uris[0].Name() || g.saveTarget.String() != uris[0].String() {
					t.Fatal("first dropped file was not opened in the empty window")
				}
				defer g.document.Close()
			} else if g.document != existing || (existing != nil && !existing.Dirty()) {
				t.Fatal("drop replaced or saved the existing document")
			}
			if got := len(driver.AllWindows()); got != wantWindows {
				t.Fatalf("window count = %d, want %d", got, wantWindows)
			}
			if loaded := loadedWindows(); loaded != count {
				t.Fatalf("loaded windows = %d, want %d", loaded, count)
			}
		})
	}
}

func TestDroppedFilesIgnoreNonFilesAndClosedWindow(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.openDroppedFiles(fyne.Position{}, []fyne.URI{nil, storage.NewURI("https://example.com/image.png")})
	if len(driver.callbacks) != 0 {
		t.Fatal("non-file drop queued an opening request")
	}
	g.openDroppedFiles(fyne.Position{}, []fyne.URI{storage.NewFileURI(filepath.Join(t.TempDir(), "missing.pdf"))})
	g.closed = true // The window closes before the queued drop is processed.
	driver.runNext(t)
	if g.window.Canvas().Overlays().Top() != nil || g.working {
		t.Fatal("closed window processed a dropped file")
	}
}

func TestMissingDroppedFilePreservesExistingDocument(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.showDocument(testDocument(t, 20, 20))
	existing := g.document
	defer existing.Close()
	g.openDroppedFiles(fyne.Position{}, []fyne.URI{storage.NewFileURI(filepath.Join(t.TempDir(), "missing.pdf"))})
	for g.window.Canvas().Overlays().Top() == nil {
		driver.runNext(t)
	}
	if g.document != existing || g.window.Canvas().Overlays().Top() == nil {
		t.Fatal("missing file did not report an error while preserving current work")
	}
}
