package gui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"emanuelelongo/proview/internal/ocr"
	"emanuelelongo/proview/internal/textselect"
	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/image/vector"
)

const searchResultLimit = 10000

type searchHit struct {
	page  int
	quads []textselect.Quad
}
type documentSearch struct {
	bar                      *fyne.Container
	host                     *fyne.Container
	entry                    *searchEntry
	status                   *widget.Label
	cancel                   context.CancelFunc
	serial                   uint64
	doc                      *document.Document
	key                      string
	hits                     []searchHit
	current, scanned, failed int
	running, limited         bool
}

type searchEntry struct {
	widget.Entry
	editor *Gui
}

func (e *searchEntry) TypedKey(k *fyne.KeyEvent) {
	switch k.Name {
	case fyne.KeyEscape:
		e.editor.closeSearch()
	case fyne.KeyReturn, fyne.KeyEnter:
		e.editor.nextSearch(e.editor.modifiers()&fyne.KeyModifierShift != 0)
	default:
		e.Entry.TypedKey(k)
	}
}

func (g *Gui) newSearchBar() *fyne.Container {
	s := &documentSearch{current: -1}
	g.search = s
	s.entry = &searchEntry{editor: g}
	s.entry.ExtendBaseWidget(s.entry)
	s.entry.SetPlaceHolder("Find in document")
	s.entry.OnChanged = func(string) { g.startSearch() }
	s.status = widget.NewLabel("")
	previous := newIconButton(tablerIcon("chevron-up"), "Previous match (Shift+F3)", func() { g.nextSearch(true) })
	next := newIconButton(tablerIcon("chevron-down"), "Next match (F3)", func() { g.nextSearch(false) })
	close := newIconButton(tablerIcon("x"), "Close search (Escape)", g.closeSearch)
	s.bar = container.NewBorder(nil, nil, widget.NewLabel("Find"), container.NewHBox(s.status, previous, next, close), s.entry)
	s.bar.Hide()
	return s.bar
}
func (g *Gui) openSearch() {
	if g.document == nil || g.showingModel() {
		return
	}
	g.setAnnotationTool(toolSelect)
	g.search.bar.Show()
	if g.search.host != nil {
		g.search.host.Refresh()
	}
	g.window.Canvas().Focus(g.search.entry)
	g.search.entry.TypedShortcut(&fyne.ShortcutSelectAll{})
	g.startSearch()
}
func (g *Gui) closeSearch() {
	s := g.search
	if s == nil {
		return
	}
	s.serial++
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.running = false
	s.hits = nil
	s.current = -1
	s.bar.Hide()
	if s.host != nil {
		s.host.Refresh()
	}
	g.refreshSearchHighlight()
	if g.window != nil && g.window.Canvas().Focused() == s.entry {
		g.window.Canvas().Focus(nil)
	}
}
func (g *Gui) searchKey() string {
	if g.document == nil {
		return ""
	}
	var b strings.Builder
	for p := 0; p < g.document.PageCount(); p++ {
		b.WriteString(g.document.TextSelectionKey(p))
		b.WriteByte('|')
	}
	return b.String()
}
func (g *Gui) syncSearch() {
	if s := g.search; s != nil && s.bar.Visible() {
		if g.document == nil || g.showingModel() {
			g.closeSearch()
			return
		}
		if s.doc != g.document || s.key != g.searchKey() {
			g.startSearch()
		} else {
			g.refreshSearchHighlight()
		}
	}
}
func (g *Gui) startSearch() {
	s := g.search
	if s == nil || !s.bar.Visible() {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.serial++
	s.doc = g.document
	s.key = g.searchKey()
	s.hits = nil
	s.current = -1
	s.scanned = 0
	s.failed = 0
	s.limited = false
	g.refreshSearchHighlight()
	query := strings.TrimSpace(s.entry.Text)
	s.running = query != "" && g.document != nil
	g.updateSearchStatus()
	if !s.running {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	serial, doc, key := s.serial, g.document, s.key
	snapshot := doc.RenderingSnapshot()
	var engine ocr.Engine
	if g.ocr != nil {
		engine = g.ocr.engine
	} else if ocr.Enabled() {
		engine = ocr.NewEngine()
	}
	// Reuse immutable recognition results that already exist for visible pages.
	cached := map[int]textselect.Layout{}
	if g.ocr != nil {
		for p := 0; p < doc.PageCount(); p++ {
			if e := g.ocr.cache[doc.TextSelectionKey(p)]; e != nil {
				cached[p] = e.layout
			}
		}
	}
	go func() {
		defer cancel()
		timer := time.NewTimer(180 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		walkSearchPages(ctx, snapshot, query, engine, cached, func(page int, hits []searchHit, err error, last, limited bool) {
			fyne.Do(func() {
				if g.closed || s.serial != serial || g.document != doc || !s.bar.Visible() {
					return
				}
				if g.searchKey() != key {
					g.startSearch()
					return
				}
				s.hits = append(s.hits, hits...)
				s.scanned = page + 1
				if err != nil {
					s.failed++
				}
				if last {
					s.running = false
					s.limited = limited
				}
				if s.current < 0 && len(s.hits) > 0 {
					g.showSearchHit(0)
				} else {
					g.updateSearchStatus()
				}
			})
		})
	}()
}

// A single cancellable worker scans pages in document order. Recognition data
// stays transient; only match geometry is retained for navigation.
func walkSearchPages(ctx context.Context, snapshot *document.Document, query string, engine ocr.Engine, cached map[int]textselect.Layout, emit func(int, []searchHit, error, bool, bool)) {
	total := 0
	for page := 0; page < snapshot.PageCount(); page++ {
		if ctx.Err() != nil {
			return
		}
		layout, ok := cached[page]
		var err error
		if !ok {
			pageCtx, stop := context.WithTimeout(ctx, 30*time.Second)
			layout, err = snapshot.SelectableText(pageCtx, page, engine)
			stop()
		}
		if ctx.Err() != nil {
			return
		}
		var hits []searchHit
		if err == nil {
			for _, m := range layout.Find(query, searchResultLimit-total) {
				hit := searchHit{page: page}
				for _, w := range layout.Range(m.Start, m.End) {
					hit.quads = append(hit.quads, w.Quad)
				}
				hits = append(hits, hit)
			}
		}
		total += len(hits)
		limited := total >= searchResultLimit
		last := page == snapshot.PageCount()-1 || limited
		emit(page, hits, err, last, limited)
		if last {
			return
		}
	}
}
func (g *Gui) updateSearchStatus() {
	s := g.search
	if s == nil {
		return
	}
	value := ""
	if strings.TrimSpace(s.entry.Text) != "" {
		value = fmt.Sprintf("%d / %d", s.current+1, len(s.hits))
		if s.running {
			value += fmt.Sprintf(" · searching %d/%d", s.scanned, s.doc.PageCount())
		}
		if s.failed > 0 {
			value += fmt.Sprintf(" · %d pages unavailable", s.failed)
		}
		if s.limited {
			value += " · limit reached"
		}
	}
	s.status.SetText(value)
}
func (g *Gui) nextSearch(back bool) {
	s := g.search
	if s == nil || !s.bar.Visible() {
		g.openSearch()
		return
	}
	if len(s.hits) == 0 {
		return
	}
	delta := 1
	if back {
		delta = -1
	}
	g.showSearchHit((s.current + delta + len(s.hits)) % len(s.hits))
}
func (g *Gui) showSearchHit(index int) {
	s := g.search
	if index < 0 || index >= len(s.hits) {
		return
	}
	s.current = index
	hit := s.hits[index]
	if g.document.Format() == document.FormatPDF {
		g.activatePDFPage(hit.page)
		if hit.page < len(g.pdfPageContainers) {
			y := g.pdfPageTop(hit.page)
			if len(hit.quads) > 0 {
				y += float32(hit.quads[0][0].Y)*g.pdfPageContainers[hit.page].Size().Height - g.pdfScroll.Size().Height/3
			}
			g.pdfScroll.ScrollToOffset(fyne.NewPos(g.pdfScroll.Offset.X, max(0, y)))
		}
	} else if len(hit.quads) > 0 {
		origin, size := g.imageSignatureLayer.displayRect()
		p := hit.quads[0][0]
		g.imageScroll.ScrollToOffset(fyne.NewPos(max(0, origin.X+float32(p.X)*size.Width-g.imageScroll.Size().Width/3), max(0, origin.Y+float32(p.Y)*size.Height-g.imageScroll.Size().Height/3)))
	}
	g.refreshSearchHighlight()
	g.updateSearchStatus()
}
func (g *Gui) refreshSearchHighlight() {
	layers := []*signatureLayer{g.imageSignatureLayer}
	for _, l := range g.signatureLayers {
		layers = append(layers, l)
	}
	for _, l := range layers {
		if l == nil {
			continue
		}
		if l.searchHighlight != nil {
			l.searchHighlight.Hide()
		}
		s := g.search
		if s != nil && s.bar.Visible() && s.current >= 0 && s.current < len(s.hits) && l.document == g.document && s.hits[s.current].page == l.page {
			quads := s.hits[s.current].quads
			l.searchHighlight = canvas.NewRaster(func(w, h int) image.Image {
				out := image.NewRGBA(image.Rect(0, 0, w, h))
				r := vector.NewRasterizer(w, h)
				for _, q := range quads {
					for i, p := range q {
						x, y := float32(p.X*float64(w)), float32(p.Y*float64(h))
						if i == 0 {
							r.MoveTo(x, y)
						} else {
							r.LineTo(x, y)
						}
					}
					r.ClosePath()
				}
				r.Draw(out, out.Bounds(), image.NewUniform(color.NRGBA{R: 255, G: 170, B: 0, A: 110}), image.Point{})
				return out
			})
		}
		l.Refresh()
	}
}
