package textselect

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Match indexes words, including partial-word matches. Geometry deliberately
// covers the whole word because OCR does not provide character positions.
type Match struct{ Start, End int }

// Find searches literal text case-insensitively, treating whitespace and line
// breaks alike. It keeps word indexes independent of UTF-8 byte lengths.
func (l Layout) Find(query string, limit int) []Match {
	normalize := func(s string) []rune {
		return []rune(strings.Map(unicode.ToLower, norm.NFC.String(strings.Join(strings.Fields(s), " "))))
	}
	needle := normalize(query)
	if len(needle) == 0 || limit <= 0 {
		return nil
	}
	var hay []rune
	var owners []int
	for i, word := range l.Words {
		if i > 0 {
			hay = append(hay, ' ')
			owners = append(owners, i)
		}
		for _, r := range normalize(word.Text) {
			hay = append(hay, r)
			owners = append(owners, i)
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
			matches = append(matches, Match{owners[i], owners[i+len(needle)-1]})
			if len(matches) == limit {
				break
			}
		}
	}
	return matches
}
