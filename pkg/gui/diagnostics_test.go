package gui

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/signatures/cloud"

	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func dialogLabelText(g *Gui) string {
	var labels []string
	for _, object := range fyneTest.LaidOutObjects(g.window.Canvas().Overlays().Top()) {
		if label, ok := object.(*widget.Label); ok {
			labels = append(labels, label.Text)
		}
	}
	return strings.Join(labels, " ")
}

func TestOperationFailuresKeepTechnicalDetailsInLogs(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	reason := "pdfengine: internal failure in sweep status at /private/source.pdf"
	for _, operation := range []string{"draw", "annotation_edit", "shape_resize", "pdf_redact", "pdf_form", "clipboard_copy", "clipboard_paste", "open", "save", "preview", "pdf_pages", "signature_library", "signature_trace", "signature_cloud", "open_url", "unknown"} {
		output.Reset()
		g.showOperationError(operation, errors.New(reason))
		if !strings.Contains(output.String(), reason) || !strings.Contains(output.String(), "operation=") {
			t.Fatal("failure lost technical reason or operation", operation, output.String())
		}
		message := dialogLabelText(g)
		if strings.Contains(message, "pdfengine") || strings.Contains(message, "sweep status") || strings.Contains(message, "/private") || !strings.Contains(message, "Couldn't") {
			t.Fatal("unfriendly operation message", operation, message)
		}
	}
	output.Reset()
	data := annotations.Default(annotations.Drawing)
	data.Points = []annotations.Point{{X: 10, Y: 20}, {X: 30, Y: 40}}
	g.showAnnotationError(1, data, errors.New(reason))
	if !strings.Contains(output.String(), "page=2") || !strings.Contains(output.String(), "points=2") || !strings.Contains(output.String(), "style=round") || !strings.Contains(dialogLabelText(g), "stroke") {
		t.Fatal("drawing failure lost gesture context or useful user instruction")
	}
	permission := &os.PathError{Op: "open", Path: "/private/secret.pdf", Err: os.ErrPermission}
	if message := operationErrorMessage("open", permission); !strings.Contains(message, "Access was denied") || strings.Contains(message, permission.Path) {
		t.Fatal("wrapped permission error was not translated", message)
	}
}

func TestDiagnosticErrorsMaskCredentialsButKeepCause(t *testing.T) {
	err := errors.New(`HTTP 403 while syncing https://user:privatepass@example.test/object?X-Amz-Signature=signedprivate&token=queryprivate Authorization: Bearer private-token {"access_token":"json private token","refresh_token":"refreshprivate","client_secret":"clientprivate"}`)
	detail := diagnosticError(err)
	for _, secret := range []string{"privatepass", "signedprivate", "queryprivate", "private-token", "json private token", "refreshprivate", "clientprivate"} {
		if strings.Contains(detail, secret) {
			t.Fatal("diagnostic exposed a credential", detail)
		}
	}
	if !strings.Contains(detail, "HTTP 403") || !strings.Contains(detail, "example.test/object") {
		t.Fatal("credential masking removed the failure cause", detail)
	}
}

func TestCloudFailureIsFriendlyAndLogsOnceUntilStatusChanges(t *testing.T) {
	service := &signatureCloudService{}
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	status := cloud.Status{Connected: true, Err: errors.New("HTTP 503 internal upstream detail")}
	for range 3 {
		if message := service.message(status); strings.Contains(message, "internal upstream") || !strings.Contains(message, "Couldn't sync") {
			t.Fatal("cloud status exposed technical error", message)
		}
	}
	if strings.Count(output.String(), "event=operation_failed") != 1 || !strings.Contains(output.String(), "HTTP 503 internal upstream detail") {
		t.Fatal("cloud error was lost or repeatedly logged", output.String())
	}
}

func TestErrorDialogsUseTheReportingBoundary(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if path == "diagnostics.go" || strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if ok && pkg.Name == "dialog" && (selector.Sel.Name == "ShowError" || selector.Sel.Name == "NewError") {
				t.Errorf("%s bypasses diagnostic logging and friendly errors", path)
			}
			return true
		})
	}
}

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
