package gui

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"emanuelelongo/proview/internal/textselect"
	"fyne.io/fyne/v2"
)

func (g *Gui) startMarkdownSearch(ctx context.Context, cancel context.CancelFunc, query string, initialMatch int) {
	s, v := g.search, g.markdownView
	serial := s.serial
	// Capture immutable strings only. Workers never read widget state or geometry.
	layout := textselect.Layout{Words: make([]textselect.Word, len(v.textBlocks))}
	for i, block := range v.textBlocks {
		layout.Words[i].Text = block.text
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
		// Strikethrough is rendered using combining strokes. Exclude those
		// decoration runes from matching while retaining original byte boundaries.
		offsets := make([][]int, len(layout.Words))
		for i := range layout.Words {
			layout.Words[i].Text, offsets[i] = markdownSearchText(layout.Words[i].Text)
		}
		originalOffset := func(block, offset int) int {
			if offsets[block] != nil {
				return offsets[block][offset]
			}
			return offset
		}
		matches := layout.Find(query, searchResultLimit)
		if ctx.Err() != nil {
			return
		}
		hits := make([]searchHit, len(matches))
		for i, match := range matches {
			hits[i].markdown = &markdownTextSelection{
				anchor: markdownTextPoint{block: match.Start, offset: originalOffset(match.Start, match.StartByte)},
				end:    markdownTextPoint{block: match.End, offset: originalOffset(match.End, match.EndByte)},
			}
		}
		fyne.Do(func() {
			if g.closed || s.serial != serial || g.markdownView != v || !s.bar.Visible() {
				return
			}
			s.hits, s.running, s.limited = hits, false, len(hits) >= searchResultLimit
			if len(hits) > 0 {
				g.showSearchHit(min(initialMatch, len(hits)-1))
			} else {
				g.updateSearchStatus()
			}
		})
	}()
}

func (g *Gui) showMarkdownSearchHit(hit searchHit) {
	v := g.markdownView
	if hit.markdown == nil || v == nil {
		return
	}
	point := hit.markdown.anchor
	if point.block >= len(v.textBlocks) {
		return
	}
	block := v.textBlocks[point.block]
	// Retained fragments include offscreen text. Reflow updates them without
	// changing byte offsets, so navigation remains valid after window resizing.
	y := block.pagePosition.Y
	for _, f := range block.fragments() {
		if point.offset >= f.start && point.offset < f.start+len(f.text) {
			y += f.position.Y
			if scroll := block.horizontal; scroll != nil {
				x := scroll.Offset.X + block.horizontalX + f.position.X + f.width(f.text[:point.offset-f.start])
				scroll.ScrollToOffset(fyne.NewPos(max(0, x-scroll.Size().Width/3), 0))
				v.updateTextViewport()
			}
			break
		}
	}
	v.pendingReadingOffset = nil
	v.scroll.ScrollToOffset(fyne.NewPos(v.scroll.Offset.X, max(0, y-v.scroll.Size().Height/3)))
}

func markdownSearchText(text string) (string, []int) {
	if !strings.ContainsRune(text, '\u0336') {
		return text, nil
	}
	var clean strings.Builder
	offsets := []int{0}
	for start, r := range text {
		size := utf8.RuneLen(r)
		if r == '\u0336' {
			offsets[len(offsets)-1] = start + size
			continue
		}
		clean.WriteRune(r)
		for i := 1; i <= size; i++ {
			offsets = append(offsets, start+i)
		}
	}
	return clean.String(), offsets
}
