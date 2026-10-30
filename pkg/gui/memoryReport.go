package gui

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"time"

	"emanuelelongo/proview/internal/applog"
	"emanuelelongo/proview/internal/buildinfo"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type memoryWindowSummary struct {
	DocumentFormats        map[string]int `json:"document_formats"`
	PDFResidentPages       int            `json:"pdf_resident_pages"`
	PDFResidentThumbnails  int            `json:"pdf_resident_thumbnails"`
	PDFPageJobs            int            `json:"pdf_page_jobs"`
	PDFThumbnailJobs       int            `json:"pdf_thumbnail_jobs"`
	MarkdownLoadedImages   int            `json:"markdown_loaded_images"`
	MarkdownAnimatedImages int            `json:"markdown_animated_images"`
	MarkdownGIFPlayers     int            `json:"markdown_gif_players"`
	SourceBytes            int64          `json:"source_bytes"`
	RasterBytes            int64          `json:"raster_bytes"`
	MarkdownImageBytes     int64          `json:"markdown_image_bytes"`
	MarkdownAnimationBytes int64          `json:"markdown_animation_bytes"`
	PreviewCacheBytes      int64          `json:"preview_cache_bytes"`
	// Estimated GPU/driver texture memory for displayed rasters (4 bytes per
	// pixel). Fyne does not expose texture sizes, so this is an approximation.
	DisplayedTextureBytes int64 `json:"displayed_texture_bytes_estimate"`
	PendingFiles          int   `json:"pending_files"`
}

type memoryReportContext struct {
	CapturedAt time.Time             `json:"captured_at"`
	Version    string                `json:"version"`
	Commit     string                `json:"commit"`
	OS         string                `json:"os"`
	Arch       string                `json:"arch"`
	GoVersion  string                `json:"go_version"`
	Windows    []memoryWindowSummary `json:"windows"`
}

type memoryRuntimeSnapshot struct {
	CapturedAt     time.Time `json:"captured_at"`
	ProcessRSS     uint64    `json:"process_rss_bytes,omitempty"`
	RSSError       string    `json:"process_rss_error,omitempty"`
	GoManaged      uint64    `json:"go_managed_bytes"`
	HeapAlloc      uint64    `json:"heap_alloc_bytes"`
	HeapInuse      uint64    `json:"heap_inuse_bytes"`
	HeapReleased   uint64    `json:"heap_released_bytes"`
	StackInuse     uint64    `json:"stack_inuse_bytes"`
	TotalAllocated uint64    `json:"total_allocated_bytes"`
	Goroutines     uint64    `json:"routines"`
	GCCount        uint32    `json:"gc_count"`
}

func (g *Gui) memoryReportContext() memoryReportContext {
	info := buildinfo.Current()
	result := memoryReportContext{CapturedAt: time.Now().UTC(), Version: info.Version,
		Commit: info.Commit, OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version()}
	windows := []*Gui{g}
	if g.editors != nil {
		windows = g.editors.windows
	}
	for _, window := range windows {
		if window.closed {
			continue
		}
		entry := memoryWindowSummary{DocumentFormats: make(map[string]int),
			PDFResidentPages: len(window.pdfResident), PDFResidentThumbnails: len(window.pdfThumbResident),
			PDFPageJobs: len(window.pdfPageJobs), PDFThumbnailJobs: len(window.pdfThumbJobs),
			PendingFiles: len(window.pendingFiles)}
		if window.previewRenderer != nil {
			entry.PreviewCacheBytes = window.previewRenderer.RetainedRasterBytes()
		}
		entry.DisplayedTextureBytes = window.displayedTextureBytes()
		for _, document := range window.documents {
			switch {
			case document.markdown != nil:
				entry.DocumentFormats["markdown"]++
				entry.MarkdownLoadedImages += document.markdown.ImageCount()
				view := document.markdownView
				if document == window.activeDocument && window.markdownView != nil {
					view = window.markdownView
				}
				if view != nil {
					entry.MarkdownAnimatedImages += len(view.animatedImages)
					entry.MarkdownGIFPlayers += len(view.gifGroups)
				}
			case document.mesh != nil:
				entry.DocumentFormats["stl"]++
			case document.doc != nil:
				entry.DocumentFormats[string(document.doc.Format())]++
			}
			if document.markdown != nil {
				entry.MarkdownImageBytes += document.markdown.ImageBytes()
				entry.MarkdownAnimationBytes += document.markdown.AnimationBytes()
			}
			if document.doc != nil {
				sourceBytes, rasterBytes := document.doc.MemoryUsage()
				entry.SourceBytes += sourceBytes
				entry.RasterBytes += rasterBytes
			}
		}
		result.Windows = append(result.Windows, entry)
	}
	return result
}

func (g *Gui) displayedTextureBytes() int64 {
	var total int64
	add := func(picture *canvas.Image) {
		if picture == nil || picture.Image == nil {
			return
		}
		size := picture.Image.Bounds().Size()
		total += int64(size.X) * int64(size.Y) * 4
	}
	for page := range g.pdfResident {
		if page >= 0 && page < len(g.pdfPageImages) {
			add(g.pdfPageImages[page])
		}
	}
	for page := range g.pdfThumbResident {
		if page >= 0 && page < len(g.pdfThumbImages) {
			add(g.pdfThumbImages[page])
		}
	}
	add(g.image)
	return total
}

func captureMemoryRuntime() memoryRuntimeSnapshot {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	samples := []metrics.Sample{{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"}, {Name: "/sched/goroutines:goroutines"}}
	metrics.Read(samples)
	result := memoryRuntimeSnapshot{CapturedAt: time.Now().UTC(),
		GoManaged: samples[0].Value.Uint64() - samples[1].Value.Uint64(),
		HeapAlloc: stats.HeapAlloc, HeapInuse: stats.HeapInuse, HeapReleased: stats.HeapReleased,
		StackInuse: stats.StackInuse, TotalAllocated: stats.TotalAlloc,
		Goroutines: samples[2].Value.Uint64(), GCCount: stats.NumGC}
	var err error
	result.ProcessRSS, err = processResidentBytes()
	if err != nil {
		result.RSSError = err.Error()
	}
	return result
}

func writeMemoryReport(writer io.Writer, context memoryReportContext) (err error) {
	before := captureMemoryRuntime()
	runtime.GC()
	after := captureMemoryRuntime()
	archive := zip.NewWriter(writer)
	defer func() { err = errors.Join(err, archive.Close()) }()
	summary, err := archive.Create("summary.json")
	if err != nil {
		return err
	}
	if err = json.NewEncoder(summary).Encode(struct {
		memoryReportContext
		BeforeGC memoryRuntimeSnapshot `json:"before_gc"`
		AfterGC  memoryRuntimeSnapshot `json:"after_gc"`
	}{context, before, after}); err != nil {
		return err
	}
	readme, err := archive.Create("README.txt")
	if err != nil {
		return err
	}
	if _, err = io.WriteString(readme, "Proview memory report\n\nsummary.json includes process resident memory, Go runtime memory before/after an explicit GC, and per-window cache counts. Document names and contents are excluded. displayed_texture_bytes_estimate approximates GPU texture memory for displayed rasters at 4 bytes per pixel; Fyne does not report actual texture sizes. The heap profile covers sampled Go allocations that remain live after GC; native libraries, GPU resources, and subprocesses are not attributed. Process resident memory is not the sum of the other fields.\n\nInspect heap.pprof with: go tool pprof -top -inuse_space heap.pprof\nUse -alloc_space to inspect cumulative allocation activity. A single report shows current retention; compare reports from before and after the memory increase to locate growth.\n"); err != nil {
		return err
	}
	heap, err := archive.Create("heap.pprof")
	if err != nil {
		return err
	}
	return pprof.Lookup("heap").WriteTo(heap, 0)
}

func saveMemoryReportFile(directory string, context memoryReportContext) (string, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	name := "memory-report-" + context.CapturedAt.In(time.Local).Format("20060102-150405") + ".zip"
	path := filepath.Join(directory, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("a memory report already exists for this second; try again: %w", err)
		}
		return "", err
	}
	err = writeMemoryReport(file, context)
	err = errors.Join(err, file.Close())
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func (g *Gui) saveMemoryReport() {
	if g.closed || g.memoryReportPreparing {
		return
	}
	g.memoryReportPreparing = true
	context := g.memoryReportContext()
	progress := dialog.NewCustomWithoutButtons("Preparing memory report", widget.NewProgressBarInfinite(), g.window)
	progress.Show()
	go func() {
		directory, err := applog.Directory()
		var path string
		if err == nil {
			path, err = saveMemoryReportFile(directory, context)
		}
		fyne.Do(func() {
			progress.Hide()
			g.memoryReportPreparing = false
			if g.closed || err != nil {
				if err != nil && !g.closed {
					g.showOperationError("memory_report", err)
				}
				return
			}
			dialog.ShowInformation("Memory report saved", fmt.Sprintf("Saved %s in the logs folder. Open it from Help → Open logs folder.", filepath.Base(path)), g.window)
		})
	}()
}
