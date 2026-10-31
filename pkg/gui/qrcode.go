package gui

import (
	"context"
	"errors"
	"time"

	"emanuelelongo/proview/internal/textselect"
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
)

type qrState struct {
	doc       *document.Document
	cache     map[string]*ocrEntry
	requested []int
	running   *ocrJob
	timer     *time.Timer
	clock     uint64
}

func (g *Gui) stopQRCodes() {
	if g.qr == nil {
		return
	}
	if g.qr.running != nil {
		g.qr.running.cancel()
	}
	if g.qr.timer != nil {
		g.qr.timer.Stop()
		g.qr.timer = nil
	}
	g.qr.doc = nil
	clear(g.qr.cache)
	g.qr.requested = nil
}

func (g *Gui) syncQRCodes() {
	if g.closed || g.document == nil {
		return
	}
	if g.qr == nil {
		g.qr = &qrState{doc: g.document, cache: map[string]*ocrEntry{}}
	}
	s := g.qr
	docChanged := s.doc != g.document
	if docChanged {
		g.stopQRCodes()
		s.doc = g.document
	}
	// Selection must never outlive the source geometry or sanitation state.
	if g.textSelection != nil && !g.validTextSelection() {
		g.clearTextSelection()
	}
	// Cache keys identify immutable source geometry. Old entries may remain for
	// Undo, but the bounded LRU never serves them to a different content key.

	pages := g.recognitionPages()
	s.requested = append(s.requested[:0], pages...)
	if job := s.running; job != nil {
		if job.doc != g.document || g.document.TextSelectionKey(job.page) != job.key || !containsPage(s.requested, job.page) {
			job.cancel()
		}
	}
	g.scheduleQRDispatch(s)
}

func (g *Gui) scheduleQRDispatch(s *qrState) {
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
			if g.qr != s {
				return
			}
			s.timer = nil
			g.dispatchQRCodes()
		})
	})
}

func (g *Gui) requestQRCodePage(page int) {
	if g.document == nil || g.closed {
		return
	}
	if g.qr == nil {
		g.qr = &qrState{doc: g.document, cache: map[string]*ocrEntry{}}
	}
	s := g.qr
	if s.doc != g.document {
		g.stopQRCodes()
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
	g.scheduleQRDispatch(s)
}
func (g *Gui) dispatchQRCodes() {
	s := g.qr
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
		go func() {
			var layout textselect.Layout
			err := withPDFRenderSlot(ctx, slots, func() error {
				var err error
				layout, err = snapshot.SelectableQRCodes(ctx, page)
				return err
			})
			// Do not log recognized text or file paths.
			canceled := errors.Is(ctx.Err(), context.Canceled)
			cancel()
			fyne.Do(func() { g.completeQRCodes(job, layout, err, canceled) })
		}()
		return
	}
}
func (l *signatureLayer) qrLayout() *textselect.Layout {
	g := l.editor
	if g == nil || g.qr == nil || g.document == nil || l.document != g.document {
		return nil
	}
	e := g.qr.cache[g.document.TextSelectionKey(l.page)]
	if e == nil {
		return nil
	}
	return &e.layout
}

func (g *Gui) completeQRCodes(job *ocrJob, layout textselect.Layout, err error, canceled bool) {
	s := g.qr
	if s == nil {
		return
	}
	if s.running != job {
		return
	}
	s.running = nil
	if g.closed || g.document != job.doc || g.document.TextSelectionKey(job.page) != job.key || canceled {
		delete(s.cache, job.key)
		g.syncQRCodes()
		return
	}
	if err != nil {
		g.logEvent("qr_recognition_failed", "level=WARN page=%d error=%q", job.page+1, err)
		layout = textselect.Layout{}
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
	g.scheduleQRDispatch(s)
}
