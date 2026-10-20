package textselect

import (
	"reflect"
	"strings"
	"testing"
)

func characterWord(text string, x float64) Word {
	w := Word{Text: text, Quad: Rect(x, .1, x+.3, .2)}
	for start, r := range text {
		end := start + len(string(r))
		w.Characters = append(w.Characters, Character{Start: start, End: end, Quad: Rect(x+float64(len(w.Characters))*.03, .1, x+float64(len(w.Characters)+1)*.03, .2)})
	}
	return w
}

func TestCharacterSelectionAndPartialSearch(t *testing.T) {
	l := Layout{Width: 1000, Height: 500, Words: []Word{characterWord("Caffè", .1), characterWord("banana", .4), {Text: "fallback", Quad: Rect(.7, .1, .9, .2)}}}
	units := l.SelectionLayout()
	if units.Text(4, 6) != "è ba" || units.Text(6, 4) != "è ba" || units.Text(0, len(units.Words)-1) != l.Text(0, 2) {
		t.Fatal("selection altered text or spacing")
	}
	for _, tc := range []struct {
		query string
		count int
	}{{"CAFFE\u0300", 5}, {"ffè ba", 5}, {"fallback", 1}} {
		matches := l.Find(tc.query, 10)
		if len(matches) != 1 || len(l.MatchQuads(matches[0])) != tc.count {
			t.Fatalf("query %q: %+v", tc.query, matches)
		}
	}
	matches := l.Find("ana", 10)
	if len(matches) != 2 || reflect.DeepEqual(l.MatchQuads(matches[0]), l.MatchQuads(matches[1])) {
		t.Fatal("different substring matches share geometry")
	}
	// A decomposed accent must map to all original bytes without splitting its glyph.
	w := characterWord("cafe\u0301", .1)
	l.Words = []Word{w}
	m := l.Find("FÉ", 10)
	if len(m) != 1 || m[0].StartByte != 2 || m[0].EndByte != len(w.Text) || len(l.MatchQuads(m[0])) != 3 {
		t.Fatalf("normalization: %+v", m)
	}
}

func TestCharacterTransformsDoNotMutateSharedGeometry(t *testing.T) {
	source := Layout{Width: 1000, Height: 500, Words: []Word{characterWord("èx", .1)}}
	original := append([]Character(nil), source.Words[0].Characters...)
	for turns := 0; turns < 4; turns++ {
		for _, flip := range []bool{false, true} {
			l := Layout{Width: source.Width, Height: source.Height, Words: append([]Word(nil), source.Words...)}
			l.Transform(turns, flip, true)
			if !reflect.DeepEqual(source.Words[0].Characters, original) {
				t.Fatal("cached geometry mutated")
			}
			units := l.SelectionLayout()
			c := units.Words[1].Quad
			p := Point{}
			for _, corner := range c {
				p.X += corner.X / 4
				p.Y += corner.Y / 4
			}
			if units.WordAt(p, false) != 1 || units.Text(0, 1) != "èx" {
				t.Fatal("transform broke character selection")
			}
		}
	}
}

func BenchmarkCharacterSelection(b *testing.B) {
	l := Layout{Width: 1000, Height: 1000}
	for i := 0; i < 2000; i++ {
		l.Words = append(l.Words, characterWord("banana", float64(i%3)*.3))
	}
	b.Run("prepare", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = l.SelectionLayout()
		}
	})
	units := l.SelectionLayout()
	b.Run("drag", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = units.WordAt(Point{.99, .99}, true)
		}
	})
	b.Run("search", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = l.Find(strings.Repeat("a", 20), 10000)
		}
	})
}
