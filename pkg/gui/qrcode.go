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
	revision  uint64
	cache     map[string]*ocrEntry
	requested []int
	running   *ocrJob
	clock     uint64
}

func (g *Gui) stopQRCodes() {
	if g.qr == nil {
		return
	}
	if g.qr.running != nil {
		g.qr.running.cancel()
	}
	g.qr.doc = nil
	g.qr.revision = 0
	clear(g.qr.cache)
	g.qr.requested = nil
}

func (g *Gui) syncQRCodes() {
	if g.closed || g.document == nil {
		return
	}
	rev := g.document.Revision()
	if g.qr == nil {
		g.qr = &qrState{doc: g.document, revision: rev, cache: map[string]*ocrEntry{}}
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
	if docChanged || s.revision != rev {
		s.revision = rev
		if len(s.cache) > 0 {
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
	g.dispatchQRCodes()
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
		go func() {
			layout, err := snapshot.SelectableQRCodes(ctx, page)
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
		g.syncQRCodes()
		return
	}
	if err != nil {
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
	g.dispatchQRCodes()
}
