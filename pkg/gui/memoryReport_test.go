package gui

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestMemoryReportIncludesLiveHeapAndCacheCountsWithoutDocumentNames(t *testing.T) {
	g, _ := newDropTestGui(t)
	g.addDocument(testDocument(t, 12, 8), nil, nil)
	context := g.memoryReportContext()
	if len(context.Windows) != 1 || context.Windows[0].DocumentFormats["png"] != 1 {
		t.Fatalf("wrong workspace summary: %+v", context.Windows)
	}
	var output bytes.Buffer
	if err := writeMemoryReport(&output, context); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string][]byte{}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents[file.Name], err = io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(contents["heap.pprof"]) == 0 || len(contents["README.txt"]) == 0 {
		t.Fatal("memory report is missing its profile or instructions")
	}
	var summary struct {
		Windows  []memoryWindowSummary `json:"windows"`
		BeforeGC memoryRuntimeSnapshot `json:"before_gc"`
		AfterGC  memoryRuntimeSnapshot `json:"after_gc"`
	}
	if err := json.Unmarshal(contents["summary.json"], &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Windows) != 1 || summary.Windows[0].DocumentFormats["png"] != 1 || summary.BeforeGC.CapturedAt.IsZero() || summary.AfterGC.CapturedAt.IsZero() || summary.AfterGC.Goroutines == 0 {
		t.Fatalf("incomplete memory summary: %+v", summary)
	}
	if strings.Contains(string(contents["summary.json"]), "Untitled") {
		t.Fatal("memory report included a document name")
	}
	g.closeWindow()
}

func TestMemoryReportUsesSortableLocalTimeWithoutOverwriting(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "logs")
	instant := time.Date(2026, time.October, 30, 8, 48, 22, 0, time.Local)
	context := memoryReportContext{CapturedAt: instant.UTC(), Version: "test"}
	first, err := saveMemoryReportFile(directory, context)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "memory-report-20261030-084822.zip" {
		t.Fatalf("unexpected report name %q", first)
	}
	context.CapturedAt = instant.Add(time.Second).UTC()
	second, err := saveMemoryReportFile(directory, context)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(second) != "memory-report-20261030-084823.zip" || first >= second {
		t.Fatalf("report names are not sorted by time: %q, %q", first, second)
	}
	info, err := os.Stat(first)
	if err != nil || info.Size() == 0 {
		t.Fatalf("missing first report %q: %v", first, err)
	}
	context.CapturedAt = instant.UTC()
	if _, err := saveMemoryReportFile(directory, context); !errors.Is(err, os.ErrExist) {
		t.Fatalf("same-second report did not preserve existing file: %v", err)
	}
	after, err := os.Stat(first)
	if err != nil || after.Size() != info.Size() {
		t.Fatalf("collision changed existing report: %v", err)
	}
}

func TestDebugCounterRightClickOffersMemoryReportOutsideHelp(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	g.setDebugEnabled(true)
	defer g.stopDebugInfo()
	g.tipSurface.TappedSecondary(&fyne.PointEvent{AbsolutePosition: fyne.NewPos(20, 20)})
	if g.window.Canvas().Overlays().Top() == nil {
		t.Fatal("debug counter has no context menu")
	}
	for _, menu := range g.window.MainMenu().Items {
		if menu.Label != "Help" {
			continue
		}
		for _, item := range menu.Items {
			if strings.Contains(item.Label, "memory report") {
				t.Fatal("memory report appeared in Help")
			}
		}
	}
}
