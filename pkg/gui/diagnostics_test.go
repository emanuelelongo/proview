package gui

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestTextEditFailureSeparatesUserMessageFromDiagnostic(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	reason := "pdfengine: Word spacing Tw=-0.036714 is not supported for text editing"
	g.showTextEditError(1, errors.New(reason))
	if !strings.Contains(output.String(), reason) || !strings.Contains(output.String(), "page=2") || !strings.Contains(output.String(), "operation=pdf_text_edit") {
		t.Fatal("technical context was not logged")
	}
	var labels []string
	for _, object := range fyneTest.LaidOutObjects(g.window.Canvas().Overlays().Top()) {
		if label, ok := object.(*widget.Label); ok {
			labels = append(labels, label.Text)
		}
	}
	message := strings.Join(labels, " ")
	if !strings.Contains(message, "can't edit yet") || strings.Contains(message, "pdfengine") || strings.Contains(message, "Tw=") || strings.Contains(message, "correction") {
		t.Fatalf("user message leaked engine details: %q", message)
	}
	if g.window.MainMenu() == nil {
		t.Fatal("missing menu")
	}
	found := false
	for _, menu := range g.window.MainMenu().Items {
		if menu.Label != "Help" {
			continue
		}
		for _, item := range menu.Items {
			if item.Label == "Open logs folder" && item.Action != nil && !item.Disabled {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("logs folder is not accessible without a document")
	}
}
