package gui

import (
	"bytes"
	"encoding/xml"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestAllTablerIconsRenderAsThemeableOutlines(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	entries, err := tablerAssets.ReadDir("icons/tabler")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".svg") {
			continue
		}
		resource := tablerIcon(strings.TrimSuffix(entry.Name(), ".svg"))
		decoder := xml.NewDecoder(bytes.NewReader(resource.Content()))
		found := false
		for {
			token, err := decoder.Token()
			if err != nil {
				break
			}
			if tag, ok := token.(xml.StartElement); ok && tag.Name.Local == "path" {
				for _, a := range tag.Attr {
					if a.Name.Local == "d" && a.Value != "" {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("%s has no geometry after theme adaptation", entry.Name())
		}
	}
	custom := largerIconTheme{Theme: theme.DefaultTheme()}
	if !strings.Contains(custom.Icon(theme.IconNameViewZoomIn).Name(), "tabler-zoom-in") {
		t.Fatal("zoom icon was not replaced")
	}
}

func TestImageWindowsShareAppAndCloseIndependently(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	baseline := len(app.Driver().AllWindows())
	first := newGui(app)
	first.showDocument(testDocument(t, 100, 50))
	first.window.Show()
	second := newGui(app)
	second.showDocument(testDocument(t, 40, 30))
	second.window.Show()
	if first.app != second.app || len(app.Driver().AllWindows()) != baseline+2 {
		t.Fatal("new image opened a separate application")
	}
	first.requestClose()
	if len(app.Driver().AllWindows()) != baseline+1 {
		t.Fatal("closing one image closed another window")
	}
	if second.document.PageCount() != 1 || second.saveTarget != nil {
		t.Fatal("independent image lost its document or destination")
	}
}

func TestEscapeCancelsPDFCrop(t *testing.T) {
	crop := newCropOverlay()
	crop.Activate(image.Rect(0, 0, 100, 100), nil)
	g := &Gui{pdfCrops: map[int]*cropOverlay{1: crop}}
	if !g.cancelCurrentOperation() || crop.active {
		t.Fatal("Escape did not cancel PDF crop")
	}
}

func TestNewActionsAvailableAndToolbarUsesIcons(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	labels := map[string]bool{}
	for _, menu := range g.window.MainMenu().Items {
		for _, item := range menu.Items {
			labels[item.Label] = true
		}
	}
	for _, label := range []string{"New page", "New from clipboard", "Open PDFs Together", "Append PDFs", "Duplicate Pages", "Delete Pages"} {
		if !labels[label] {
			t.Fatalf("missing action %s", label)
		}
	}
	for _, action := range g.annotationActions {
		if action.button.Text != "" || action.button.Icon == nil || action.button.tooltip == "" {
			t.Fatal("tool lacks an icon or readable hover name")
		}
	}
	g.window.Resize(fyne.NewSize(1000, 700))
	if path := os.Getenv("PROVIEW_TOOLBAR_PREVIEW"); path != "" {
		g.window.Show()
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, g.window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewImageReusesEmptyWindowAndPreservesOpenDocument(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	windows := len(app.Driver().AllWindows())
	first := testDocument(t, 100, 50)
	g.openNewImage(first)
	if g.document != first || g.saveTarget != nil || len(app.Driver().AllWindows()) != windows {
		t.Fatal("new image did not reuse the empty window")
	}
	second := testDocument(t, 40, 30)
	g.openNewImage(second)
	if g.document != first || len(app.Driver().AllWindows()) != windows+1 {
		t.Fatal("new image replaced the open document instead of opening another window")
	}
}
