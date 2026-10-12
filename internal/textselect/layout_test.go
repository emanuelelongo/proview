package textselect

import "testing"

func TestSelectionOrderAndTransforms(t *testing.T) {
	l := Layout{Width: 1000, Height: 500, Words: []Word{
		{Text: "Caffè", Quad: Rect(.1, .1, .3, .2)},
		{Text: "ready", Quad: Rect(.4, .1, .6, .2)},
		{Text: "Second", Line: 1, Quad: Rect(.1, .3, .3, .4)},
		{Text: "Column", Block: 1, Line: 2, Quad: Rect(.7, .1, .9, .2)},
	}}
	if got := l.Text(3, 0); got != "Caffè ready\nSecond\n\nColumn" {
		t.Fatal(got)
	}
	if l.WordAt(Point{.35, .15}, false) != -1 {
		t.Fatal("gap must not start text selection")
	}
	if l.WordAt(Point{.35, .15}, true) < 0 {
		t.Fatal("drag must find nearest word")
	}
	for _, test := range []struct {
		turns int
		x, y  bool
	}{{1, false, false}, {2, false, false}, {3, false, false}, {0, true, false}, {1, true, true}} {
		copy := Layout{Width: l.Width, Height: l.Height, Words: append([]Word(nil), l.Words...)}
		copy.Transform(test.turns, test.x, test.y)
		q := copy.Words[0].Quad
		p := Point{}
		for _, v := range q {
			p.X += v.X / 4
			p.Y += v.Y / 4
		}
		if copy.WordAt(p, false) != 0 || copy.Text(0, 1) != "Caffè ready" {
			t.Fatal("transform broke selection")
		}
	}
}
