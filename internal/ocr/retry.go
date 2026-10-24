package ocr

import (
	"context"
	"image"
	"image/color"
	"math"
	"strings"
	"time"
	"unicode"

	"emanuelelongo/proview/internal/textselect"
)

// Both passes use the same prepared pixels and geometry. Publish once so a
// delayed supplement cannot invalidate an existing selection's word indexes.
func recognizeWithRetry(ctx context.Context, img image.Image, run func(context.Context, int) (textselect.Layout, error)) (textselect.Layout, error) {
	primary, err := run(ctx, 2) // Preserve the established Sauvola path.
	if err != nil {
		return primary, err
	}
	if err := ctx.Err(); err != nil {
		return textselect.Layout{}, err
	}
	if !needsRetry(img, primary) {
		return primary, nil
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 4*time.Second {
		return primary, nil
	}
	retryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	secondary, err := run(retryCtx, 0) // Otsu recovers some dark screenshot text.
	if ctx.Err() != nil {
		return textselect.Layout{}, ctx.Err()
	}
	if err != nil || retryCtx.Err() != nil {
		return primary, nil
	}
	return supplementWords(ctx, primary, secondary)
}

func needsRetry(img image.Image, primary textselect.Layout) bool {
	// Leave confidently recognized pages and ordinary light scans untouched.
	if len(primary.Words) > 0 {
		good, total := 0, 0.
		for _, word := range primary.Words {
			total += word.Confidence
			if word.Confidence >= 80 {
				good++
			}
		}
		if good*10 >= len(primary.Words)*9 && total >= 90*float64(len(primary.Words)) {
			return false
		}
	}
	b := img.Bounds()
	dark, samples := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y += max(1, b.Dy()/100) {
		for x := b.Min.X; x < b.Max.X; x += max(1, b.Dx()/100) {
			v := color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y
			if v < 96 {
				dark++
			}
			samples++
		}
	}
	return samples > 0 && dark*5 >= samples*4
}

type wordBounds struct{ x0, y0, x1, y1 float64 }

func boundsOf(word textselect.Word) wordBounds {
	b := wordBounds{1, 1, 0, 0}
	for _, p := range word.Quad {
		b.x0, b.y0 = min(b.x0, p.X), min(b.y0, p.Y)
		b.x1, b.y1 = max(b.x1, p.X), max(b.y1, p.Y)
	}
	return b
}
func (b wordBounds) overlaps(other wordBounds) bool {
	return b.x0 < other.x1 && other.x0 < b.x1 && b.y0 < other.y1 && other.y0 < b.y1
}

// Supplement only uncovered regions; never replace primary text, confidence,
// geometry, line/block IDs or relative reading order, even for weak primary words.
func supplementWords(ctx context.Context, primary, secondary textselect.Layout) (textselect.Layout, error) {
	if primary.Width != secondary.Width || primary.Height != secondary.Height || primary.Width <= 0 || primary.Height <= 0 {
		return primary, nil
	}
	// Bound pairwise geometry work on unexpectedly dense/noisy pages.
	if len(primary.Words) > 4096 || len(secondary.Words) > 4096 {
		return primary, nil
	}
	occupied := make([]wordBounds, len(primary.Words))
	nextBlock, nextLine := 0, 0
	for i, word := range primary.Words {
		occupied[i] = boundsOf(word)
		nextBlock, nextLine = max(nextBlock, word.Block+1), max(nextLine, word.Line+1)
	}
	type lineID struct{ block, line int }
	newLines := map[lineID]lineID{}
	newBlocks := map[int]int{}
	slots := make([][]textselect.Word, len(primary.Words)+1)
	added := 0
	for _, word := range secondary.Words {
		if err := ctx.Err(); err != nil {
			return textselect.Layout{}, err
		}
		if word.Confidence < 85 || strings.IndexFunc(word.Text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) < 0 {
			continue
		}
		box := boundsOf(word)
		if box.x0 < 0 || box.y0 < 0 || box.x1 > 1 || box.y1 > 1 || box.x1 <= box.x0 || box.y1 <= box.y0 {
			continue
		}
		overlap := false
		for _, existing := range occupied {
			if box.overlaps(existing) {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		// Match an existing line only when its vertical span and nearby horizontal
		// position agree; do not join separate columns at the same height.
		slot, anchor := len(primary.Words), -1
		best := math.Inf(1)
		for i := range primary.Words {
			b := occupied[i]
			height := min(box.y1-box.y0, b.y1-b.y0)
			vertical := min(box.y1, b.y1) - max(box.y0, b.y0)
			gap := max(0, max(b.x0-box.x1, box.x0-b.x1)) * primary.Width
			if vertical >= height*.5 && gap <= max(box.y1-box.y0, b.y1-b.y0)*primary.Height*4 && gap < best {
				best, anchor = gap, i
			}
		}
		if anchor >= 0 {
			line := primary.Words[anchor]
			word.Block, word.Line = line.Block, line.Line
			slot = anchor
			for i, existing := range primary.Words {
				if existing.Block == line.Block && existing.Line == line.Line {
					if occupied[i].x0 > box.x0 {
						slot = i
						break
					}
					slot = i + 1
				}
			}
		} else {
			id := lineID{word.Block, word.Line}
			line, ok := newLines[id]
			if !ok {
				block, exists := newBlocks[id.block]
				if !exists {
					block = nextBlock
					newBlocks[id.block] = block
					nextBlock++
				}
				line = lineID{block, nextLine}
				nextLine++
				newLines[id] = line
			}
			word.Block, word.Line = line.block, line.line
			// Locate the closest original line above/below while leaving its
			// original order intact (including columns and rotated input).
			for i, b := range occupied[:len(primary.Words)] {
				distance := math.Abs((b.y0 + b.y1) - (box.y0 + box.y1))
				if distance < best {
					best = distance
					slot = i
					if box.y0 >= b.y0 {
						slot++
					}
				}
			}
			if slot > 0 && slot < len(primary.Words) {
				previous := primary.Words[slot-1]
				for slot < len(primary.Words) && primary.Words[slot].Block == previous.Block && primary.Words[slot].Line == previous.Line {
					slot++
				}
			}
		}
		slots[slot] = append(slots[slot], word)
		occupied = append(occupied, box)
		added++
	}
	if added == 0 {
		return primary, nil
	}
	result := textselect.Layout{Width: primary.Width, Height: primary.Height, Words: make([]textselect.Word, 0, len(primary.Words)+added)}
	for i, extra := range slots {
		result.Words = append(result.Words, extra...)
		if i < len(primary.Words) {
			result.Words = append(result.Words, primary.Words[i])
		}
	}
	return result, nil
}
