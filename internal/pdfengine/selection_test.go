//go:build cgo && !nocgo && !extlib

package pdfengine

import (
	"math"
	"reflect"
	"testing"
)

func TestSelectionSkipsWhitespaceOnlyLinesAndReverses(t *testing.T) {
	layout := Layout{Width: 100, Height: 100}
	for line, text := range []string{"PUBLIC ", "   ", "SECRET KEEP"} {
		for x, r := range text {
			left, top := float64(x)*.04, float64(line)*.2
			q := Quad{UL: Point{left, top}, UR: Point{left + .03, top}, LL: Point{left, top + .1}, LR: Point{left + .03, top + .1}}
			layout.Glyphs = append(layout.Glyphs, Glyph{Rune: r, Line: line, Quad: q})
		}
	}
	quads := layout.Selection(2, len(layout.Glyphs)-2)
	if len(quads) != 2 {
		t.Fatalf("line bands=%d", len(quads))
	}
	if !reflect.DeepEqual(quads, layout.Selection(len(layout.Glyphs)-2, 2)) {
		t.Fatal("backwards selection differs")
	}
	if math.Abs(quads[0].UR.X-layout.Glyphs[5].Quad.UR.X) > 1e-9 {
		t.Fatal("trailing whitespace widened band")
	}
	if len(layout.Selection(-1, 3)) != 0 || len(layout.Selection(0, 999)) != 0 {
		t.Fatal("invalid selection accepted")
	}
	bad := quads[0]
	bad.UL.X = math.NaN()
	if bad.Valid() {
		t.Fatal("NaN accepted")
	}
	bad = quads[0]
	bad.UR, bad.LR = bad.LR, bad.UR
	if bad.Valid() {
		t.Fatal("crossed quad accepted")
	}
}

func TestLineBandEnclosesMixedFontHeights(t *testing.T) {
	a := Quad{UL: Point{.1, .2}, UR: Point{.2, .2}, LL: Point{.1, .3}, LR: Point{.2, .3}}
	b := Quad{UL: Point{.2, .1}, UR: Point{.3, .1}, LL: Point{.2, .4}, LR: Point{.3, .4}}
	c := Quad{UL: Point{.3, .2}, UR: Point{.4, .2}, LL: Point{.3, .3}, LR: Point{.4, .3}}
	layout := Layout{Width: 100, Height: 100, Glyphs: []Glyph{{Rune: 'a', Quad: a}, {Rune: 'b', Quad: b}, {Rune: 'c', Quad: c}}}
	q := layout.Selection(0, 2)
	if len(q) != 1 {
		t.Fatal("missing band")
	}
	_, y0, _, y1 := q[0].Bounds()
	if math.Abs(y0-.1) > 1e-9 || math.Abs(y1-.4) > 1e-9 {
		t.Fatal("taller middle glyph was not covered")
	}
}
