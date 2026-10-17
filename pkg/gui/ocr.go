package gui

import (
	"context"
	"errors"
	"log"
	"time"

	"emanuelelongo/proview/internal/ocr"
	"emanuelelongo/proview/internal/textselect"
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
)

type ocrEntry struct {
	layout textselect.Layout
	used   uint64
}
type ocrJob struct {
	doc    *document.Document
	page   int
	key    string
	cancel context.CancelFunc
}
type ocrState struct {
	engine    ocr.Engine
	doc       *document.Document
	revision  uint64
	cache     map[string]*ocrEntry
	requested []int
	running   *ocrJob
	clock     uint64
}

var ocrSetupWarningShown bool // Accessed only on the Fyne event thread.

func (g *Gui) stopOCR() {
	g.stopQRCodes()
	if g.ocr != nil {
		if g.ocr.running != nil {
			g.ocr.running.cancel()
		}
		g.ocr.doc = nil
		g.ocr.revision = 0
		g.ocr.cache = map[string]*ocrEntry{}
		g.ocr.requested = nil
	}
	g.clearTextSelection()
}

// syncOCR is called on view/content changes. Unlike annotation jobs it never
// sets g.working, and only a single OCR job per editor may run at a time.
func (g *Gui) syncOCR() {
	defer g.syncQRCodes()
	if g.closed || g.document == nil {
		return
	}
	if g.ocr == nil {
		if !ocr.Enabled() {
			return
		}
		g.ocr = &ocrState{engine: ocr.NewEngine(), cache: map[string]*ocrEntry{}}
	}
	s := g.ocr
	rev := g.document.Revision()
	docChanged := s.doc != g.document
	if docChanged {
		g.stopOCR()
		s.doc = g.document
	}
	// Selection must never outlive the source geometry or sanitation state.
	if g.textSelection != nil && !g.validTextSelection() {
		g.clearTextSelection()
	}
	if docChanged || s.revision != rev {
		s.revision = rev
		valid := make(map[string]bool, g.document.PageCount())
		for page := 0; page < g.document.PageCount(); page++ {
			valid[g.document.TextSelectionKey(page)] = true
		}
		for key := range s.cache {
			if !valid[key] {
				delete(s.cache, key)
			}
		}
	}
	pages := g.recognitionPages()
	s.requested = pages
	if job := s.running; job != nil {
		wanted := false
		for _, p := range pages {
			if p == job.page {
				wanted = true
			}
		}
		if job.doc != g.document || g.document.TextSelectionKey(job.page) != job.key || !wanted {
			job.cancel()
		}
		return
	}
	g.dispatchOCR()
}
func (g *Gui) dispatchOCR() {
	s := g.ocr
	if s == nil || s.running != nil || g.closed || s.doc != g.document || g.document == nil {
		return
	}
	for _, page := range s.requested {
		key := g.document.TextSelectionKey(page)
		if key == "" {
			continue
		}
		if e := s.cache[key]; e != nil {
			s.clock++
			e.used = s.clock
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		job := &ocrJob{doc: g.document, page: page, key: key, cancel: cancel}
		s.running = job
		snapshot := g.document.RenderingSnapshot()
		started := time.Now()
		go func() {
			layout, err := snapshot.SelectableText(ctx, page, s.engine)
			// Do not log recognized text or file paths.
			elapsed := time.Since(started)
			canceled := errors.Is(ctx.Err(), context.Canceled)
			cancel()
			fyne.Do(func() { g.completeOCR(job, layout, err, canceled, elapsed) })
		}()
		return
	}
}
func (l *signatureLayer) selectableLayout() *textselect.Layout {
	g := l.editor
	if g == nil || g.ocr == nil || g.document == nil || l.document != g.document {
		return nil
	}
	e := g.ocr.cache[g.document.TextSelectionKey(l.page)]
	if e == nil {
		return nil
	}
	return &e.layout
}

func (g *Gui) completeOCR(job *ocrJob, layout textselect.Layout, err error, canceled bool, elapsed time.Duration) {
	s := g.ocr
	if s == nil {
		return
	}
	if s.running != job {
		return
	}
	s.running = nil
	if g.closed || g.document != job.doc || g.document.TextSelectionKey(job.page) != job.key || canceled {
		g.syncOCR()
		return
	}
	if err != nil {
		layout = textselect.Layout{}
		log.Printf("OCR page %d failed: %v", job.page+1, err)
		var setup *ocr.SetupError
		if errors.As(err, &setup) && !ocrSetupWarningShown && g.window != nil {
			ocrSetupWarningShown = true
			dialog.ShowInformation("Text recognition unavailable", "The text recognition engine could not start. Please reinstall Proview if the problem persists.", g.window)
		}
	} else {
		log.Printf("OCR page %d: %d words, %s", job.page+1, len(layout.Words), elapsed.Round(time.Millisecond))
	}
	s.clock++
	s.cache[job.key] = &ocrEntry{layout: layout, used: s.clock}
	for len(s.cache) > 20 {
		oldest := ""
		age := ^uint64(0)
		for k, e := range s.cache {
			if e.used < age {
				oldest, age = k, e.used
			}
		}
		delete(s.cache, oldest)
	}
	g.dispatchOCR()
}

func (g *Gui) recognitionPages() []int {
	pages := []int{0}
	if g.document.Format() == document.FormatPDF {
		pages = nil
		if g.activePDFPage >= 0 && g.activePDFPage < g.document.PageCount() {
			pages = append(pages, g.activePDFPage)
		}
		if g.pdfScroll != nil {
			top, bottom := g.pdfScroll.Offset.Y, g.pdfScroll.Offset.Y+g.pdfScroll.Size().Height
			y := float32(0)
			for page, obj := range g.pdfPageContainers {
				end := y + obj.MinSize().Height
				if end > top && y < bottom && page != g.activePDFPage {
					pages = append(pages, page)
				}
				y = end + theme.Padding()
			}
		}
	}
	if len(pages) > 20 {
		pages = pages[:20]
	}
	return pages
}
