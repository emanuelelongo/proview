package gui

import (
	"context"
	"errors"
	"time"

	"emanuelelongo/proview/internal/ocr"
	"emanuelelongo/proview/internal/textselect"
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
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
	cache     map[string]*ocrEntry
	requested []int
	running   *ocrJob
	timer     *time.Timer
	clock     uint64
}

var ocrSetupWarningShown bool // Accessed only on the Fyne event thread.

const recognitionDebounce = 140 * time.Millisecond

func (g *Gui) stopOCR() {
	g.stopQRCodes()
	if g.ocr != nil {
		if g.ocr.timer != nil {
			g.ocr.timer.Stop()
			g.ocr.timer = nil
		}
		if g.ocr.running != nil {
			g.ocr.running.cancel()
		}
		g.ocr.doc = nil
		clear(g.ocr.cache)
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
		g.ocr = &ocrState{engine: ocr.NewEngine(), doc: g.document, cache: map[string]*ocrEntry{}}
	}
	s := g.ocr
	docChanged := s.doc != g.document
	if docChanged {
		g.stopOCR()
		s.doc = g.document
	}
	// Selection must never outlive the source geometry or sanitation state.
	if g.textSelection != nil && !g.validTextSelection() {
		g.clearTextSelection()
	}
	// Cache keys identify immutable source geometry. Old entries may remain for
	// Undo, but the bounded LRU never serves them to a different content key.

	pages := g.recognitionPages()
	g.setOCRRequests(pages)
	if job := s.running; job != nil {
		if job.doc != g.document || g.document.TextSelectionKey(job.page) != job.key || !containsPage(s.requested, job.page) {
			job.cancel()
		}
	}
	g.scheduleOCRDispatch(s)
}

func containsPage(pages []int, page int) bool {
	for _, candidate := range pages {
		if candidate == page {
			return true
		}
	}
	return false
}

func (g *Gui) setOCRRequests(pages []int) {
	if g.ocr == nil {
		return
	}
	g.ocr.requested = append(g.ocr.requested[:0], pages...)
}

func (g *Gui) scheduleOCRDispatch(s *ocrState) {
	if s == nil || s.timer != nil || len(s.requested) == 0 {
		return
	}
	pending := false
	for _, page := range s.requested {
		if g.document == nil || page < 0 || page >= g.document.PageCount() || s.cache[g.document.TextSelectionKey(page)] == nil {
			pending = true
			break
		}
	}
	if !pending {
		return
	}
	s.timer = time.AfterFunc(recognitionDebounce, func() {
		fyne.Do(func() {
			if g.ocr != s {
				return
			}
			s.timer = nil
			g.dispatchOCR()
		})
	})
}

func (g *Gui) requestOCRPage(page int) {
	if g.document == nil || !ocr.Enabled() {
		return
	}
	if g.ocr == nil {
		g.ocr = &ocrState{engine: ocr.NewEngine(), doc: g.document, cache: map[string]*ocrEntry{}}
	}
	s := g.ocr
	if s.doc != g.document {
		g.stopOCR()
		s.doc = g.document
	}
	if page < 0 || page >= g.document.PageCount() || containsPage(s.requested, page) {
		return
	}
	if s.cache[g.document.TextSelectionKey(page)] != nil {
		return
	}
	s.requested = append([]int{page}, s.requested...)
	if s.running != nil && (s.running.page != page || s.running.doc != g.document) {
		s.running.cancel()
	}
	g.scheduleOCRDispatch(s)
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
		slots := g.pdfRenderSlots()
		started := time.Now()
		go func() {
			var layout textselect.Layout
			err := withPDFRenderSlot(ctx, slots, func() error {
				var err error
				layout, err = snapshot.SelectableText(ctx, page, s.engine)
				return err
			})
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
		delete(s.cache, job.key)
		g.syncOCR()
		return
	}
	if err != nil {
		layout = textselect.Layout{}
		g.logEvent("text_recognition_failed", "level=WARN page=%d elapsed=%s error=%q", job.page+1, elapsed.Round(time.Millisecond), err)
		var setup *ocr.SetupError
		if errors.As(err, &setup) && !ocrSetupWarningShown && g.window != nil {
			ocrSetupWarningShown = true
			dialog.ShowInformation("Text recognition unavailable", "The text recognition engine could not start. Please reinstall Proview if the problem persists.", g.window)
		}
	} else {
		if elapsed >= time.Second {
			g.logEvent("text_recognition_completed", "page=%d words=%d elapsed=%s", job.page+1, len(layout.Words), elapsed.Round(time.Millisecond))
		}
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
	g.scheduleOCRDispatch(s)
}

func (g *Gui) recognitionPages() []int {
	pages := []int{0}
	if g.document.Format() == document.FormatPDF {
		if g.activePDFPage >= 0 && g.activePDFPage < g.document.PageCount() {
			pages[0] = g.activePDFPage
		} else {
			pages = nil
		}
	}
	return pages
}

func (g *Gui) recognitionEngine() ocr.Engine {
	if !ocr.Enabled() || g.closed || g.document == nil {
		return nil
	}
	if g.ocr == nil {
		g.ocr = &ocrState{engine: ocr.NewEngine(), doc: g.document, cache: map[string]*ocrEntry{}}
	} else if g.ocr.doc != g.document {
		engine := g.ocr.engine
		g.stopOCR()
		g.ocr.doc, g.ocr.engine = g.document, engine
	}
	return g.ocr.engine
}
