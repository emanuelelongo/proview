package gui

import (
	"image"
	"time"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
)

type pdfBasePreviewState struct {
	doc        *document.Document
	generation uint64
	timer      *time.Timer
	running    bool
	failed     map[int]document.PageBaseKey
}

func (g *Gui) clearPDFBasePreviews() {
	if s := g.pdfBasePreviews; s != nil && s.timer != nil {
		s.timer.Stop()
	}
	g.pdfBasePreviews = nil
}

// Fields and native source replacement share a bounded latest-state worker.
// Overlay-only Undo/Redo and edits to unrelated pages produce no render jobs.
func (g *Gui) refreshPDFBasePreviews() {
	if g.closed || g.document == nil || g.document.Format() != document.FormatPDF {
		return
	}
	s := g.pdfBasePreviews
	if s == nil || s.doc != g.document || s.generation != g.pdfGeneration {
		g.clearPDFBasePreviews()
		s = &pdfBasePreviewState{doc: g.document, generation: g.pdfGeneration, failed: map[int]document.PageBaseKey{}}
		g.pdfBasePreviews = s
	}
	if s.running || s.timer != nil {
		return
	}
	changed := false
	for page, loaded := range g.pdfPageLoaded {
		if loaded {
			key := g.document.BasePageKey(page)
			if g.pdfBaseKeys[page] != key && s.failed[page] != key {
				changed = true
				break
			}
		}
	}
	if !changed {
		return
	}
	s.timer = time.AfterFunc(50*time.Millisecond, func() { fyne.Do(func() { s.timer = nil; g.renderPDFBasePreviews(s) }) })
}

func (g *Gui) renderPDFBasePreviews(s *pdfBasePreviewState) {
	if g.closed || g.pdfBasePreviews != s || g.document != s.doc || g.pdfGeneration != s.generation || s.running {
		return
	}
	keys := map[int]document.PageBaseKey{}
	for page, loaded := range g.pdfPageLoaded {
		if loaded {
			key := s.doc.BasePageKey(page)
			if g.pdfBaseKeys[page] != key && s.failed[page] != key {
				keys[page] = key
			}
		}
	}
	if len(keys) == 0 {
		return
	}
	snapshot := s.doc.RenderingSnapshot()
	s.running = true
	go func() {
		pictures, failures := map[int]image.Image{}, map[int]error{}
		for page := range keys {
			picture, err := snapshot.RenderBasePage(page)
			if err != nil {
				failures[page] = err
			} else {
				pictures[page] = picture
			}
		}
		fyne.Do(func() {
			s.running = false
			if g.closed || g.pdfBasePreviews != s || g.document != s.doc || g.pdfGeneration != s.generation {
				return
			}
			for page, key := range keys {
				if s.doc.BasePageKey(page) != key || !g.pdfPageWanted(page) || !g.pdfPageLoaded[page] {
					continue
				}
				if err := failures[page]; err != nil {
					s.failed[page] = key
					g.logEvent("pdf_base_preview_failed", "level=WARN page=%d error=%q", page+1, err)
					continue
				}
				g.pdfPageImages[page].Image = pictures[page]
				g.pdfPageImages[page].Refresh()
				g.retainPDFPageImage(page)
				g.refreshPagePreview(page)
				g.scheduleLensPreviews(g.signatureLayers[page])
			}
			g.refreshPDFDetail()
			g.refreshPDFBasePreviews()
		})
	}()
}
