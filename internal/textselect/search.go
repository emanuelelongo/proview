package textselect

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Match addresses words and UTF-8 byte offsets within the first and last word.
// EndByte is exclusive. Words without character geometry retain word highlights.
type Match struct{ Start, End, StartByte, EndByte int }

type searchOrigin struct{ word, start, end int }

// Find searches literal text case-insensitively, treating whitespace and line
// breaks alike. Normalization segments map matches back to original UTF-8 text.
func (l Layout) Find(query string, limit int) []Match {
	needle := []rune(strings.Map(unicode.ToLower, norm.NFC.String(strings.Join(strings.Fields(query), " "))))
	if len(needle) == 0 || limit <= 0 {
		return nil
	}
	var hay []rune
	var owners []searchOrigin
	for i, word := range l.Words {
		if i > 0 && !word.JoinPrevious {
			hay = append(hay, ' ')
			owners = append(owners, searchOrigin{word: i})
		}
		var iter norm.Iter
		iter.InitString(norm.NFC, word.Text)
		first := len(hay)
		for !iter.Done() {
			start := iter.Pos()
			segment := iter.Next()
			origin := searchOrigin{word: i, start: start, end: iter.Pos()}
			for _, r := range string(segment) {
				r = unicode.ToLower(r)
				if unicode.IsSpace(r) {
					if len(hay) == first || hay[len(hay)-1] == ' ' {
						continue
					}
					r = ' '
				}
				hay = append(hay, r)
				owners = append(owners, origin)
			}
		}
		if len(hay) > first && hay[len(hay)-1] == ' ' {
			hay = hay[:len(hay)-1]
			owners = owners[:len(owners)-1]
		}
	}
	var matches []Match
	for i := 0; i+len(needle) <= len(hay); i++ {
		found := true
		for j, r := range needle {
			if hay[i+j] != r {
				found = false
				break
			}
		}
		if found {
			a, b := owners[i], owners[i+len(needle)-1]
			matches = append(matches, Match{a.word, b.word, a.start, b.end})
			if len(matches) == limit {
				break
			}
		}
	}
	return matches
}

func (l Layout) MatchQuads(m Match) []Quad {
	var quads []Quad
	for i := m.Start; i <= m.End && i < len(l.Words); i++ {
		if i < 0 {
			continue
		}
		w := l.Words[i]
		start, end := 0, len(w.Text)
		if i == m.Start {
			start = m.StartByte
		}
		if i == m.End {
			end = m.EndByte
		}
		before := len(quads)
		for _, c := range w.Characters {
			if c.End > start && c.Start < end {
				quads = append(quads, c.Quad)
			}
		}
		if len(quads) == before {
			quads = append(quads, w.Quad)
		}
	}
	return quads
}
