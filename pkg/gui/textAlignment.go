package gui

import (
	"math"
	"unicode"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
)

const textSnapDistance float64 = 6
const textSnapReleaseDistance float64 = 10

type textAlignmentReference struct {
	rect     document.NormalizedRect
	baseline float64
}
type textSnapAxis struct {
	active    bool
	reference int
}

func textRangeRect(layout document.TextLayout, start, end int) document.NormalizedRect {
	x0, y0, x1, y1 := layout.Glyphs[start].Quad.Bounds()
	for _, g := range layout.Glyphs[start+1 : end+1] {
		a, b, c, d := g.Quad.Bounds()
		x0 = math.Min(x0, a)
		y0 = math.Min(y0, b)
		x1 = math.Max(x1, c)
		y1 = math.Max(y1, d)
	}
	return document.NormalizedRect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}
func textAlignmentReferences(layout document.TextLayout, start, end int) []textAlignmentReference {
	var refs []textAlignmentReference
	for i := 0; i < len(layout.Glyphs); {
		if unicode.IsSpace(layout.Glyphs[i].Rune) {
			i++
			continue
		}
		j := i
		for j+1 < len(layout.Glyphs) && layout.Glyphs[j+1].Line == layout.Glyphs[i].Line && !unicode.IsSpace(layout.Glyphs[j+1].Rune) {
			j++
		}
		g := layout.Glyphs[i]
		if (j < start || i > end) && g.Quad.UR.X > g.Quad.UL.X && math.Abs(g.Quad.UR.Y-g.Quad.UL.Y)*layout.Height < .1 {
			refs = append(refs, textAlignmentReference{rect: textRangeRect(layout, i, j), baseline: g.Origin.Y})
		}
		i = j + 1
	}
	return refs
}

// Raw pointer displacement stays separate from snapped displacement. This
// avoids accumulated jumps when entering/leaving a guide or pressing Shift.
func (g *Gui) refreshTextMoveAlignment() {
	m := g.textMove
	if m == nil || !m.dragging || g.working {
		return
	}
	l := m.layer
	size := l.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	rect := m.base
	rect.X = math.Max(0, math.Min(math.Max(0, 1-rect.Width), rect.X+float64(m.rawOffset.X)))
	rect.Y = math.Max(0, math.Min(math.Max(0, 1-rect.Height), rect.Y+float64(m.rawOffset.Y)))
	m.rawOffset = fyne.NewPos(float32(rect.X-m.base.X), float32(rect.Y-m.base.Y))
	baselineOffset := l.layout.Glyphs[m.start].Origin.Y - m.base.Y
	choose := func(axis *textSnapAxis, coordinate, scale float64, horizontal bool) {
		value := func(ref textAlignmentReference) float64 {
			if horizontal {
				return ref.baseline
			}
			return ref.rect.X
		}
		if axis.active && axis.reference < len(m.references) && math.Abs(value(m.references[axis.reference])-coordinate)*scale <= textSnapReleaseDistance {
			return
		}
		axis.active = false
		best, proximity := textSnapDistance, math.Inf(1)
		for i, ref := range m.references {
			distance := math.Abs(value(ref)-coordinate) * scale
			near := math.Abs((ref.rect.Y+ref.rect.Height/2)-(rect.Y+rect.Height/2)) * float64(size.Height)
			if horizontal {
				near = math.Abs((ref.rect.X+ref.rect.Width/2)-(rect.X+rect.Width/2)) * float64(size.Width)
			}
			if distance <= textSnapDistance && (distance < best || math.Abs(distance-best) < .01 && near < proximity) {
				best = distance
				proximity = near
				axis.reference = i
				axis.active = true
			}
		}
	}
	choose(&m.vertical, rect.X, float64(size.Width), false)
	choose(&m.horizontal, rect.Y+baselineOffset, float64(size.Height), true)
	free := g.selectionShiftKeys != 0 || g.modifiers()&fyne.KeyModifierShift != 0
	if !free {
		if m.vertical.active {
			rect.X = m.references[m.vertical.reference].rect.X
		}
		if m.horizontal.active {
			rect.Y = m.references[m.horizontal.reference].baseline - baselineOffset
		}
		// Never snap beyond the page bounds.
		if rect.X < 0 || rect.X+rect.Width > 1 {
			rect.X = m.base.X + float64(m.rawOffset.X)
			m.vertical.active = false
		}
		if rect.Y < 0 || rect.Y+rect.Height > 1 {
			rect.Y = m.base.Y + float64(m.rawOffset.Y)
			m.horizontal.active = false
		}
	}
	m.offset = fyne.NewPos(float32(rect.X-m.base.X), float32(rect.Y-m.base.Y))
	l.selectionRect = rect
	l.Refresh()
}
func (l *textCorrectionLayer) hideMoveGuides() { l.horizontalGuide.Hide(); l.verticalGuide.Hide() }
func (l *textCorrectionLayer) layoutMoveGuides(size fyne.Size) {
	m := l.editor.textMove
	if m == nil || m.layer != l || !m.dragging {
		l.hideMoveGuides()
		return
	}
	q := l.selectionRect
	if m.horizontal.active {
		ref := m.references[m.horizontal.reference]
		l.horizontalGuide.Position1 = fyne.NewPos(float32(math.Min(q.X, ref.rect.X))*size.Width, float32(ref.baseline)*size.Height)
		l.horizontalGuide.Position2 = fyne.NewPos(float32(math.Max(q.X+q.Width, ref.rect.X+ref.rect.Width))*size.Width, float32(ref.baseline)*size.Height)
		l.horizontalGuide.Show()
	} else {
		l.horizontalGuide.Hide()
	}
	if m.vertical.active {
		ref := m.references[m.vertical.reference]
		l.verticalGuide.Position1 = fyne.NewPos(float32(ref.rect.X)*size.Width, float32(math.Min(q.Y, ref.rect.Y))*size.Height)
		l.verticalGuide.Position2 = fyne.NewPos(float32(ref.rect.X)*size.Width, float32(math.Max(q.Y+q.Height, ref.rect.Y+ref.rect.Height))*size.Height)
		l.verticalGuide.Show()
	} else {
		l.verticalGuide.Hide()
	}
}

func findMovedText(layout document.TextLayout, value string, origin document.TextPoint) (int, int, bool) {
	runes := []rune(value)
	if len(runes) == 0 {
		return 0, 0, false
	}
	for i, g := range layout.Glyphs {
		if i+len(runes) > len(layout.Glyphs) {
			break
		}
		if math.Abs(g.Origin.X-origin.X)*layout.Width > .05 || math.Abs(g.Origin.Y-origin.Y)*layout.Height > .05 {
			continue
		}
		match := true
		for j, r := range runes {
			if layout.Glyphs[i+j].Rune != r {
				match = false
				break
			}
		}
		if match {
			return i, i + len(runes) - 1, true
		}
	}
	return 0, 0, false
}
