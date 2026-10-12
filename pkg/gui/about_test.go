package gui

import (
	"emanuelelongo/proview/internal/buildinfo"
	"image/png"
	"os"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestAboutShowsBuildMetadataWithoutDocument(t *testing.T) {
	previous := buildinfo.Current()
	oldCommit, oldDate := buildinfo.Commit, buildinfo.Date
	buildinfo.Version = "v1-dev.42"
	buildinfo.Commit = "0123456789abcdef0123456789abcdef01234567"
	buildinfo.Date = "2026-10-07T12:20:30+02:00"
	t.Cleanup(func() {
		buildinfo.Version = previous.Version
		buildinfo.Commit, buildinfo.Date = oldCommit, oldDate
	})
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.window.Resize(fyne.NewSize(1000, 700))
	g.window.Show()
	var help *fyne.Menu
	for _, menu := range g.window.MainMenu().Items {
		if menu.Label == "Help" {
			help = menu
		}
	}
	if help == nil {
		t.Fatal("Help menu is missing")
	}
	about := help.Items[len(help.Items)-1]
	if about.Label != "About" || about.Disabled {
		t.Fatal("About must be the last Help action and available without a document")
	}
	about.Action()
	want := map[string]bool{
		"Name": false, "Version": false, "Repository": false, "Commit": false, "Date": false,
		"Proview": false, "v1-dev.42": false, buildinfo.Repository: false,
		buildinfo.Commit: false, "2026-10-07T10:20:30Z": false,
	}
	linked := false
	for _, object := range fyneTest.LaidOutObjects(g.window.Canvas().Overlays().Top()) {
		if text, ok := object.(*canvas.Text); ok {
			if _, expected := want[text.Text]; expected {
				want[text.Text] = true
			}
		}
		if label, ok := object.(*widget.Label); ok {
			if _, expected := want[label.Text]; expected {
				want[label.Text] = true
			}
			if label.Text == buildinfo.Commit && !label.Selectable {
				t.Fatal("Commit must be selectable")
			}
		}
		if link, ok := object.(*widget.Hyperlink); ok && link.URL.String() == buildinfo.Repository && link.Text == buildinfo.Repository {
			linked = true
		}
	}
	for text, found := range want {
		if !found {
			t.Errorf("About is missing %q", text)
		}
	}
	if !linked {
		t.Error("About must link to the repository")
	}
	if path := os.Getenv("PROVIEW_ABOUT_PREVIEW"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, g.window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}
	tapDialogButton(t, g, "Close")
	if g.window.Canvas().Overlays().Top() != nil {
		t.Fatal("About did not close")
	}
}
