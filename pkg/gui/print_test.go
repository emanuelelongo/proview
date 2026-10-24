package gui

import (
	"context"
	"errors"
	"slices"
	"testing"

	"emanuelelongo/proview/internal/printing"
	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestPrintPageRanges(t *testing.T) {
	pages, err := parsePrintPages("5, 1-3, 2", 5)
	if err != nil || !slices.Equal(pages, []int{0, 1, 2, 4}) {
		t.Fatal(pages, err)
	}
	for _, input := range []string{"", "0", "6", "2-1", "1,,2", "1-9999999999999999999999", "1-2-3", "-1"} {
		if _, err := parsePrintPages(input, 5); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestPrintingUnsavedImageDoesNotSaveAndReleasesEditor(t *testing.T) {
	for _, result := range []error{nil, printing.ErrCanceled, errors.New("printer unavailable")} {
		t.Run(func() string {
			if result == nil {
				return "success"
			}
			return result.Error()
		}(), func(t *testing.T) {
			g, driver := newDropTestGui(t)
			defer g.closeWindow()
			g.window.Show()
			doc := testDocument(t, 40, 20)
			doc.MarkUnsaved()
			g.openNewImage(doc)
			revision := doc.Revision()
			jobs := make(chan printing.Job, 1)
			g.printBackend = func(ctx context.Context, job printing.Job) error {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				jobs <- job
				return result
			}
			g.requestPrint()
			if !g.working || g.printJob == nil {
				t.Fatal("printing did not reserve the active document")
			}
			for g.printJob != nil {
				driver.runNext(t)
			}
			job := <-jobs
			if len(job.Pages) != 1 || len(job.PDF) == 0 {
				t.Fatal("backend received no prepared document")
			}
			if g.document != doc || !doc.Dirty() || doc.Revision() != revision || g.saveTarget != nil || g.working || printOwner != nil {
				t.Fatal("printing changed document/save state or left the editor blocked")
			}
		})
	}
}

func TestPrintCurrentPDFPageAndCancelPageChoice(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.closeWindow()
	g.window.Show()
	g.addDocument(workspacePDF(t, 3), nil, nil)
	g.activatePDFPage(2)
	jobs := make(chan printing.Job, 1)
	g.printBackend = func(_ context.Context, job printing.Job) error { jobs <- job; return printing.ErrCanceled }
	g.requestPrint()
	tapDialogButton(t, g, "Cancel")
	if g.printJob != nil || g.working || printOwner != nil {
		t.Fatal("cancel did not release the print operation")
	}
	select {
	case <-jobs:
		t.Fatal("cancel opened the native print dialog")
	default:
	}
	g.requestPrint()
	for _, object := range fyneTest.LaidOutObjects(g.window.Canvas().Overlays().Top()) {
		if choice, ok := object.(*widget.Select); ok {
			choice.SetSelected("Current page")
		}
	}
	tapDialogButton(t, g, "Continue")
	for g.printJob != nil {
		driver.runNext(t)
	}
	if job := <-jobs; len(job.Pages) != 1 {
		t.Fatalf("current-page print prepared %d pages", len(job.Pages))
	}
}

func TestPrintIsUnavailableWithoutAnImageOrPDF(t *testing.T) {
	g, _ := newDropTestGui(t)
	defer g.closeWindow()
	g.window.Show()
	g.requestPrint()
	if g.printJob != nil || g.working {
		t.Fatal("empty window started printing")
	}
	var printItem *fyne.MenuItem
	for _, item := range g.menuDocumentItems {
		if item.Label == "Print…" {
			printItem = item
		}
	}
	g.updateHistoryActions()
	if printItem == nil || !printItem.Disabled {
		t.Fatal("print menu enabled without a document")
	}
}
