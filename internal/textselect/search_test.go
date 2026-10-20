package textselect

import (
	"reflect"
	"testing"
)

func TestFindUnicodeWhitespaceAndPartialWords(t *testing.T) {
	l := Layout{Words: []Word{{Text: "Caffè"}, {Text: "CAFÉ", Line: 1}, {Text: "banana"}, {Text: "fine", Line: 2}}}
	for _, tc := range []struct {
		query string
		limit int
		want  []Match
	}{
		{"CAFFE\u0300", 10, []Match{{0, 0, 0, 6}}},
		{"café\n banana", 10, []Match{{1, 2, 0, 6}}},
		{"ana", 10, []Match{{2, 2, 1, 4}, {2, 2, 3, 6}}},
		{"ana", 1, []Match{{2, 2, 1, 4}}},
		{"banana fine", 10, []Match{{2, 3, 0, 4}}},
		{"", 10, nil}, {"missing", 10, nil}, {"fine", 0, nil},
	} {
		if got := l.Find(tc.query, tc.limit); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Find(%q)=%v, want %v", tc.query, got, tc.want)
		}
	}
}
