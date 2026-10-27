package gui

import (
	"bytes"
	"emanuelelongo/proview/pkg/document"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/storage"
)

func TestOpeningWindowSizeMatchesPageAndAvailableSpace(t *testing.T) {
	chrome, minimum := fyne.NewSize(210, 88), fyne.NewSize(760, 300)
	for _, area := range []fyne.Size{fyne.NewSize(1920, 1080), fyne.NewSize(1440, 900), fyne.NewSize(1024, 768), fyne.NewSize(3000, 2000)} {
		portrait := openingWindowSize(fyne.NewSize(210, 297), chrome, area, minimum)
		landscape := openingWindowSize(fyne.NewSize(297, 210), chrome, area, minimum)
		if portrait.Height < landscape.Height || portrait.Width >= landscape.Width {
			t.Fatal("page orientation did not affect the window")
		}
		for _, size := range []fyne.Size{portrait, landscape, openingWindowSize(fyne.Size{}, chrome, area, minimum)} {
			if size.Width > area.Width-32 || size.Height > area.Height-48 || size.Width <= 0 || size.Height <= 0 {
				t.Fatal("window does not fit the available work area")
			}
		}
	}
	if size := openingWindowSize(fyne.Size{}, chrome, fyne.NewSize(1920, 1080), minimum); size != fyne.NewSize(1000, 740) {
		t.Fatal("image window is not standard sized")
	}
}

type openingWindowRecorder struct {
	fyne.Window
	editor                                      *Gui
	shown, readyAtShow                          bool
	firstSize                                   fyne.Size
	centers, centersAfterShow, resizesAfterShow int
}

func (w *openingWindowRecorder) Show() {
	if !w.shown {
		w.firstSize = w.Canvas().Size()
		w.readyAtShow = w.editor.hasOpenContent()
	}
	w.shown = true
	w.Window.Show()
}
func (w *openingWindowRecorder) Resize(size fyne.Size) {
	if w.shown {
		w.resizesAfterShow++
	}
	w.Window.Resize(size)
}
func (w *openingWindowRecorder) CenterOnScreen() {
	w.centers++
	if w.shown {
		w.centersAfterShow++
	}
	w.Window.CenterOnScreen()
}

func TestFileWindowFirstShowUsesLoadedPortraitGeometry(t *testing.T) {
	for _, name := range []string{"markdown", "pdf"} {
		t.Run(name, func(t *testing.T) {
			g, callbacks := newDropTestGui(t)
			window := &openingWindowRecorder{Window: g.window, editor: g}
			g.window = window
			g.windowWorkArea = func() fyne.Size { return fyne.NewSize(1920, 1200) }
			path := "../../demo/fixtures/showcase.pdf"
			if name == "markdown" {
				path = filepath.Join(t.TempDir(), "reading.md")
				if err := os.WriteFile(path, []byte("# Reading\n\nA short paragraph.\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			g.prepareWindowOpening([]string{path})
			g.openStartupFiles([]string{path})
			if window.shown {
				t.Fatal("file window was shown before the opening worker completed")
			}
			for g.openingFiles {
				callbacks.runNext(t)
			}
			if !window.shown || !window.readyAtShow || window.firstSize.Height <= window.firstSize.Width {
				t.Fatalf("first frame did not contain the loaded document in a portrait window: %+v", window)
			}
			if window.centers != 1 || window.centersAfterShow != 0 || window.resizesAfterShow != 0 {
				t.Fatal("opening changed window geometry after showing it")
			}
			// A subsequent file in the same visible editor may change size,
			// but must not move the window back to the monitor center.
			g.sizeWindowForOpenedFile()
			if window.centers != 1 {
				t.Fatal("opening into an existing editor recentered it")
			}
			g.closeWindow()
		})
	}
}

func TestFailedInitialOpeningStillShowsTheErrorWindow(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		g, callbacks := newDropTestGui(t)
		window := &openingWindowRecorder{Window: g.window, editor: g}
		g.window = window
		path := filepath.Join(t.TempDir(), "invalid.pdf")
		if corrupt {
			if err := os.WriteFile(path, []byte("Invalid PDF"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		g.prepareWindowOpening([]string{path})
		g.openStartupFiles([]string{path})
		for g.openingFiles {
			callbacks.runNext(t)
		}
		if !window.shown || g.showAfterOpening || g.window.Canvas().Overlays().Top() == nil {
			t.Fatal("failed opening left an invisible application or error dialog")
		}
		g.closeWindow()
	}
}

func TestInitialWindowScaleAppliesUserScalingBeforeNativeCreation(t *testing.T) {
	newDropTestGui(t)
	t.Setenv("FYNE_SCALE", "1.25")
	if got := initialWindowScale(1.6); got != 2 {
		t.Fatalf("initial user scale = %v, want 2", got)
	}
	t.Setenv("FYNE_SCALE", "auto")
	if got := initialWindowScale(1.6); got != 1.6 {
		t.Fatalf("initial automatic scale = %v, want 1.6", got)
	}
}

type nativeSizingWindow struct{ fyne.Window }

func (w nativeSizingWindow) RunNative(fn func(any)) { fn(driver.UnknownContext{}) }

func TestOpeningFitWaitsForNativeLayoutAndYieldsToUserZoom(t *testing.T) {
	g, callbacks := newDropTestGui(t)
	g.window = nativeSizingWindow{g.window}
	g.windowWorkArea = func() fyne.Size { return fyne.NewSize(1920, 1200) }
	g.window.Show()
	g.addDocument(testDocument(t, 1600, 1000), nil, storage.NewFileURI("/tmp/image.png"))
	defer g.closeDocument()
	for _, size := range []fyne.Size{fyne.NewSize(1050, 780), fyne.NewSize(1100, 820)} {
		g.window.Resize(size)
	}
	for g.openingFitQueued {
		callbacks.runNext(t)
	}
	if g.openingFitDocument != nil || g.zoom != g.calculateFitZoom() {
		t.Fatal("opening fit used an intermediate native viewport")
	}
	g.addDocument(testDocument(t, 1800, 1200), nil, storage.NewFileURI("/tmp/other.png"))
	g.zoomIn()
	zoom := g.zoom
	for g.openingFitQueued {
		callbacks.runNext(t)
	}
	if g.zoom != zoom || g.zoomLevel != 1 {
		t.Fatal("delayed opening fit replaced an explicit user zoom")
	}
}

func TestOpeningPDFShowsTheFirstPageAndHonorsFullscreen(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.windowWorkArea = func() fyne.Size { return fyne.NewSize(1920, 1200) }
	g.window.Show()
	data, err := os.ReadFile("../../demo/fixtures/showcase.pdf")
	if err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("portrait.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	g.addDocument(doc, nil, storage.NewFileURI("/tmp/pages.pdf"))
	defer g.closeDocument()
	if size := g.window.Canvas().Size(); size.Height <= size.Width {
		t.Fatalf("portrait PDF did not open in a tall window: %v", size)
	}
	page, viewport := g.pdfPageImages[0].MinSize(), g.pdfScroll.Size()
	if page.Width > viewport.Width || page.Height > viewport.Height {
		t.Fatalf("first PDF page does not fit: %v in %v", page, viewport)
	}
	manual := fyne.NewSize(1100, 800)
	g.window.Resize(manual)
	g.window.SetFullScreen(true)
	g.sizeWindowForOpenedFile()
	if g.window.Canvas().Size() != manual {
		t.Fatal("opening resized a fullscreen window")
	}
}

func TestOpeningMarkdownSizesWindowButSwitchingRetainsManualSize(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.windowWorkArea = func() fyne.Size { return fyne.NewSize(1920, 1200) }
	g.window.Show()
	g.addMarkdown(readingDocument(t), storage.NewFileURI("/tmp/reading.md"))
	defer g.clearMarkdown()
	reading := g.activeDocument
	if size := g.window.Canvas().Size(); size.Height <= size.Width || size.Height < 1000 {
		t.Fatalf("Markdown did not open in a tall window: %v", size)
	}
	g.addDocument(testDocument(t, 1600, 1000), nil, storage.NewFileURI("/tmp/image.png"))
	if size := g.window.Canvas().Size(); size != fyne.NewSize(1000, 740) {
		t.Fatalf("image did not open at standard size: %v", size)
	}
	if g.zoomLevel != 0 || g.zoom != g.calculateFitZoom() {
		t.Fatal("image was not fitted to the new window")
	}
	manual := fyne.NewSize(1000, 800)
	g.window.Resize(manual)
	g.selectDocument(reading)
	if g.window.Canvas().Size() != manual {
		t.Fatal("workspace switching replaced manual window geometry")
	}
}
