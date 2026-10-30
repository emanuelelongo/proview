//go:build cgo && !nocgo && !extlib

package pdfengine

/*
#include "textedit.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"golang.org/x/text/unicode/norm"
	"math"
	"strings"
	"unicode/utf8"
	"unsafe"
)

// CorrectionTextLayout extracts only page contents, excluding widget/annotation
// appearances. Indexes belong to these exact bytes, not a later document version.
func CorrectionTextLayout(data []byte, page int) (Layout, error) {
	if len(data) == 0 || page < 0 || page > math.MaxInt32 {
		return Layout{}, errors.New("pdfengine: invalid text editing layout arguments")
	}
	input := cBytes(data)
	var result C.pv_result
	defer C.pv_free_result(&result)
	C.pv_correct_text((*C.uchar)(input), C.size_t(len(data)), C.int(page), 0, 0, nil, 0, 0, 0, &result)
	if err := resultError(&result); err != nil {
		return Layout{}, err
	}
	l := Layout{Width: float64(result.width), Height: float64(result.height)}
	for _, g := range unsafe.Slice(result.glyphs, int(result.glyph_count)) {
		l.Glyphs = append(l.Glyphs, Glyph{Rune: rune(g.codepoint), Line: int(g.line), Quad: goQuad(g.quad), Origin: Point{X: float64(g.origin.x), Y: float64(g.origin.y)}})
	}
	return l, nil
}

// CorrectText replaces one uniformly styled horizontal range in page contents.
// dx/dy are visible-page points. Unsupported text states fail without output.
// ErrTextOverlap identifies a proposed edit that intersects other source text.
var ErrTextOverlap = errors.New("text would overlap existing text")

func CorrectText(data []byte, page, start, count int, replacement string, dx, dy float64) ([]byte, error) {
	return editText(data, page, start, count, replacement, dx, dy, false)
}

// DuplicateText retains the source range and inserts native searchable text at
// the requested position. The copy must not overlap the original or other text.
func DuplicateText(data []byte, page, start, count int, replacement string, dx, dy float64) ([]byte, error) {
	return editText(data, page, start, count, replacement, dx, dy, true)
}

func editText(data []byte, page, start, count int, replacement string, dx, dy float64, duplicate bool) ([]byte, error) {
	replacement = norm.NFC.String(replacement)
	if duplicate && strings.TrimSpace(replacement) == "" {
		return nil, errors.New("pdfengine: text duplication requires nonempty text")
	}
	if len(data) == 0 || page < 0 || page > math.MaxInt32 || start < 0 || start > math.MaxInt32 || count <= 0 || count > 4096 ||
		!utf8.ValidString(replacement) || strings.ContainsAny(replacement, "\x00\r\n\t") || len([]rune(replacement)) > 4096 ||
		math.IsNaN(dx) || math.IsNaN(dy) || math.IsInf(dx, 0) || math.IsInf(dy, 0) || math.Abs(dx) > 100000 || math.Abs(dy) > 100000 {
		return nil, errors.New("pdfengine: invalid text editing arguments")
	}
	input := cBytes(data)
	runes := []rune(replacement)
	// C owns all buffers used across its exception boundary.
	codes := C.malloc(C.size_t((len(runes) + 1) * int(unsafe.Sizeof(C.int(0)))))
	if codes == nil {
		return nil, errors.New("pdfengine: cannot allocate text editing input")
	}
	defer C.free(codes)
	for i, r := range runes {
		unsafe.Slice((*C.int)(codes), len(runes))[i] = C.int(r)
	}
	var result C.pv_result
	defer C.pv_free_result(&result)
	if duplicate {
		C.pv_duplicate_text((*C.uchar)(input), C.size_t(len(data)), C.int(page), C.int(start), C.int(count), (*C.int)(codes), C.int(len(runes)), C.float(dx), C.float(dy), &result)
	} else {
		C.pv_correct_text((*C.uchar)(input), C.size_t(len(data)), C.int(page), C.int(start), C.int(count),
			(*C.int)(codes), C.int(len(runes)), C.float(dx), C.float(dy), &result)
	}
	if err := resultError(&result); err != nil {
		if strings.Contains(err.Error(), "would overlap existing text") {
			return nil, fmt.Errorf("%w: %v", ErrTextOverlap, err)
		}
		return nil, err
	}
	if uint64(result.length) > math.MaxInt32 {
		return nil, errors.New("pdfengine: text editing result too large")
	}
	return C.GoBytes(unsafe.Pointer(result.bytes), C.int(result.length)), nil
}
