package gui

import (
	"image/color"
	"math"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Keep byte offsets in the original segment text: wrapping must not introduce
// newlines or discard whitespace when the selected text is copied.
type markdownTextPoint struct {
	block  int
	offset int
}

type markdownTextSelection struct {
	anchor, end markdownTextPoint
	dragging    bool
}

type markdownSelectableText struct {
	widget.RichText
	view         *markdownView
	index        int
	text         string
	renderer     *markdownSelectionRenderer
	segmentTheme *container.ThemeOverride
}

func (v *markdownView) selectable(segments ...widget.RichTextSegment) *markdownSelectableText {
	b := &markdownSelectableText{view: v, index: len(v.textBlocks)}
	b.Segments = segments
	b.Scroll = fyne.ScrollNone
	b.Wrapping = fyne.TextWrapWord
	var text strings.Builder
	for _, s := range segments {
		text.WriteString(s.Textual())
	}
	b.text = text.String()
	b.ExtendBaseWidget(b)
	v.textBlocks = append(v.textBlocks, b)
	return b
}

func (b *markdownSelectableText) CreateRenderer() fyne.WidgetRenderer {
	b.renderer = &markdownSelectionRenderer{WidgetRenderer: b.RichText.CreateRenderer(), block: b}
	return b.renderer
}

func (b *markdownSelectableText) Resize(size fyne.Size) {
	if b.Size() == size {
		return
	}
	b.BaseWidget.Resize(size)
	b.RichText.Refresh()
}

func (b *markdownSelectableText) Cursor() desktop.Cursor         { return desktop.TextCursor }
func (b *markdownSelectableText) MouseIn(*desktop.MouseEvent)    {}
func (b *markdownSelectableText) MouseMoved(*desktop.MouseEvent) {}
func (b *markdownSelectableText) MouseOut()                      {}
func (b *markdownSelectableText) MouseDown(e *desktop.MouseEvent) {
	if e.Button != desktop.MouseButtonPrimary {
		return
	}
	p := b.point(e.Position)
	b.view.selection = &markdownTextSelection{anchor: p, end: p, dragging: true}
	b.view.refreshSelection()
}
func (b *markdownSelectableText) MouseUp(*desktop.MouseEvent) {
	if b.view.selection != nil {
		b.view.selection.dragging = false
	}
}
func (b *markdownSelectableText) Dragged(e *fyne.DragEvent) {
	v := b.view
	if v.selection == nil || !v.selection.dragging {
		start := e.Position.Subtract(fyne.NewPos(e.Dragged.DX, e.Dragged.DY))
		p := b.point(start)
		v.selection = &markdownTextSelection{anchor: p, end: p, dragging: true}
	}
	// The drag owner stays fixed while crossing paragraphs and table cells.
	best := b
	distance := float32(math.MaxFloat32)
	for _, candidate := range v.textBlocks {
		origin := v.editor.app.Driver().AbsolutePositionForObject(candidate)
		pos := e.AbsolutePosition.Subtract(origin)
		dx := max(0, max(-pos.X, pos.X-candidate.Size().Width))
		dy := max(0, max(-pos.Y, pos.Y-candidate.Size().Height))
		d := dx*dx + dy*dy
		if d < distance {
			best, distance = candidate, d
		}
	}
	origin := v.editor.app.Driver().AbsolutePositionForObject(best)
	v.selection.end = best.point(e.AbsolutePosition.Subtract(origin))
	v.refreshSelection()
}
func (b *markdownSelectableText) DragEnd() {
	if b.view.selection != nil {
		b.view.selection.dragging = false
	}
}
func (b *markdownSelectableText) Tapped(*fyne.PointEvent) {}
func (b *markdownSelectableText) TappedSecondary(e *fyne.PointEvent) {
	text := b.view.selectedText()
	if text == "" {
		text = b.text
	}
	menu := fyne.NewMenu("", fyne.NewMenuItem("Copy text", func() { b.view.editor.app.Clipboard().SetContent(text) }))
	widget.ShowPopUpMenuAtPosition(menu, b.view.editor.window.Canvas(), e.AbsolutePosition)
}

func (v *markdownView) selectionBounds() (markdownTextPoint, markdownTextPoint, bool) {
	if v.selection == nil {
		return markdownTextPoint{}, markdownTextPoint{}, false
	}
	a, b := v.selection.anchor, v.selection.end
	if a.block > b.block || a.block == b.block && a.offset > b.offset {
		a, b = b, a
	}
	return a, b, a != b
}
func (v *markdownView) selectedText() string {
	a, b, ok := v.selectionBounds()
	if !ok {
		return ""
	}
	var parts []string
	for i := a.block; i <= b.block; i++ {
		text := v.textBlocks[i].text
		start, end := 0, len(text)
		if i == a.block {
			start = a.offset
		}
		if i == b.block {
			end = b.offset
		}
		parts = append(parts, text[start:end])
	}
	return strings.ReplaceAll(strings.Join(parts, "\n\n"), "\u0336", "")
}
func (v *markdownView) refreshSelection() {
	for _, b := range v.textBlocks {
		if b.renderer != nil {
			b.renderer.updateSelection()
			canvas.Refresh(b)
		}
	}
}
func (v *markdownView) clearSelection() bool {
	if v.selection == nil {
		return false
	}
	v.selection = nil
	v.refreshSelection()
	return true
}

type markdownTextFragment struct {
	text     string
	start    int
	position fyne.Position
	size     fyne.Size
	fontSize float32
	style    fyne.TextStyle
	font     fyne.Resource
}

func (f markdownTextFragment) width(text string) float32 {
	s, _ := fyne.CurrentApp().Driver().RenderedTextSize(text, f.fontSize, f.style, f.font)
	return s.Width
}

func (b *markdownSelectableText) fragments() []markdownTextFragment {
	if b.renderer == nil {
		return nil
	}
	var fragments []markdownTextFragment
	cursor := 0
	for _, object := range b.renderer.WidgetRenderer.Objects() {
		f := markdownTextFragment{position: object.Position(), size: object.Size()}
		switch o := object.(type) {
		case *canvas.Text:
			f.text, f.fontSize, f.style, f.font = o.Text, o.TextSize, o.TextStyle, o.FontSource
		case *fyne.Container:
			if len(o.Objects) != 1 {
				continue
			}
			link, ok := o.Objects[0].(*widget.Hyperlink)
			if !ok {
				continue
			}
			f.text, f.fontSize, f.style = link.Text, theme.SizeForWidget(theme.SizeNameText, b), link.TextStyle
		default:
			continue
		}
		if f.text == "" {
			continue
		}
		relative := strings.Index(b.text[cursor:], f.text)
		if relative < 0 {
			continue
		}
		f.start = cursor + relative
		cursor = f.start + len(f.text)
		fragments = append(fragments, f)
	}
	return fragments
}
func (b *markdownSelectableText) point(pos fyne.Position) markdownTextPoint {
	fragments := b.fragments()
	point := markdownTextPoint{block: b.index}
	distance := float32(math.MaxFloat32)
	for _, f := range fragments {
		dx := max(0, max(f.position.X-pos.X, pos.X-f.position.X-f.size.Width))
		dy := max(0, max(f.position.Y-pos.Y, pos.Y-f.position.Y-f.size.Height))
		d := dx*dx + dy*dy
		if d >= distance {
			continue
		}
		distance = d
		x := pos.X - f.position.X
		boundaries := make([]int, 0, len(f.text)+1)
		for i := range f.text {
			boundaries = append(boundaries, i)
		}
		boundaries = append(boundaries, len(f.text))
		// Binary search avoids repeatedly measuring every prefix of long lines.
		index := sort.Search(len(boundaries)-1, func(i int) bool {
			left := f.width(f.text[:boundaries[i]])
			right := f.width(f.text[:boundaries[i+1]])
			return x < (left+right)/2
		})
		offset := boundaries[index]
		point.offset = f.start + offset
	}
	return point
}

type markdownSelectionRenderer struct {
	fyne.WidgetRenderer
	block      *markdownSelectableText
	highlights []fyne.CanvasObject
	pool       []*canvas.Rectangle
}

func (r *markdownSelectionRenderer) Layout(size fyne.Size) {
	r.WidgetRenderer.Layout(size)
	r.updateSelection()
}
func (r *markdownSelectionRenderer) Refresh() {
	r.WidgetRenderer.Refresh()
	r.updateSelection()
}
func (r *markdownSelectionRenderer) Objects() []fyne.CanvasObject {
	objects := append([]fyne.CanvasObject{}, r.WidgetRenderer.Objects()...)
	return append(objects, r.highlights...)
}
func (r *markdownSelectionRenderer) updateSelection() {
	r.highlights = r.highlights[:0]
	a, b, ok := r.block.view.selectionBounds()
	index := r.block.index
	if !ok || index < a.block || index > b.block {
		return
	}
	start, end := 0, len(r.block.text)
	if index == a.block {
		start = a.offset
	}
	if index == b.block {
		end = b.offset
	}
	for _, f := range r.block.fragments() {
		lo, hi := max(start, f.start)-f.start, min(end, f.start+len(f.text))-f.start
		if lo >= hi {
			continue
		}
		x := f.width(f.text[:lo])
		width := f.width(f.text[:hi]) - x
		// A translucent foreground works with RichText's inline-code backgrounds.
		c := theme.ColorForWidget(theme.ColorNameSelection, r.block)
		red, green, blue, _ := c.RGBA()
		if len(r.highlights) == len(r.pool) {
			r.pool = append(r.pool, canvas.NewRectangle(color.Transparent))
		}
		rect := r.pool[len(r.highlights)]
		rect.FillColor = color.NRGBA{R: uint8(red >> 8), G: uint8(green >> 8), B: uint8(blue >> 8), A: 85}
		rect.Move(f.position.Add(fyne.NewPos(x, 0)))
		rect.Resize(fyne.NewSize(width, f.size.Height))
		r.highlights = append(r.highlights, rect)
	}
}
