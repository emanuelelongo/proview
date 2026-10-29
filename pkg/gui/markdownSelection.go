package gui

import (
	"image"
	"image/color"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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

type markdownTextLink struct {
	start, end int
	onTapped   func()
}

// Retain the original formatting while adding a clickable text range.
type markdownLinkSegment struct {
	*widget.TextSegment
	onTapped func()
}

type markdownSelectableText struct {
	widget.RichText
	view              *markdownView
	index             int
	text              string
	renderer          *markdownSelectionRenderer
	creatingRenderer  bool
	segmentTheme      *container.ThemeOverride
	mousePosition     fyne.Position
	hasImages         bool
	links             []markdownTextLink
	pagePosition      fyne.Position
	positioned        bool
	doubleTappedAt    time.Time
	doubleTapPosition fyne.Position
	tripleTapped      bool
	horizontal        *container.Scroll
	horizontalX       float32
}

func (v *markdownView) selectable(segments ...widget.RichTextSegment) *markdownSelectableText {
	b := &markdownSelectableText{view: v, index: len(v.textBlocks)}
	// Native HyperlinkSegments do not participate in RichText line wrapping.
	// Use ordinary styled text and retain the clickable byte ranges instead.
	segments = append([]widget.RichTextSegment(nil), segments...)
	offset := 0
	for i, segment := range segments {
		if link, ok := segment.(*markdownLinkSegment); ok {
			segments[i] = link.TextSegment
			b.links = append(b.links, markdownTextLink{start: offset, end: offset + len(link.Text), onTapped: link.onTapped})
		}
		if image, ok := segment.(*markdownImageSegment); ok {
			image.text = &b.RichText
			image.block = b
			b.hasImages = true
		}
		if link, ok := segment.(*widget.HyperlinkSegment); ok {
			style := widget.RichTextStyleInline
			style.Alignment = link.Alignment
			style.ColorName = theme.ColorNameHyperlink
			segments[i] = &widget.TextSegment{Text: link.Text, Style: style}
			b.links = append(b.links, markdownTextLink{start: offset, end: offset + len(link.Text), onTapped: link.OnTapped})
		}
		offset += len(segment.Textual())
	}
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

// Idle renderer eviction also removes Fyne's widget theme override. Restore the
// embedded RichText scope only when it is missing, before regenerating glyphs.
func (b *markdownSelectableText) ensureReadingTheme() {
	if b.segmentTheme == nil {
		return
	}
	actual := theme.CurrentForWidget(&b.RichText)
	expected := b.view.readingTheme()
	variant := fyne.CurrentApp().Settings().ThemeVariant()
	if actual.Color(theme.ColorNameForeground, variant) != expected.Color(theme.ColorNameForeground, variant) ||
		actual.Color(theme.ColorNameBackground, variant) != expected.Color(theme.ColorNameBackground, variant) ||
		actual.Size(theme.SizeNameText) != expected.Size(theme.SizeNameText) {
		b.segmentTheme.Refresh()
	}
}

func (b *markdownSelectableText) Refresh() {
	b.ensureReadingTheme()
	b.RichText.Refresh()
}

func (b *markdownSelectableText) CreateRenderer() fyne.WidgetRenderer {
	// Restoring the embedded theme refreshes RichText. During construction,
	// that refresh may ask Fyne for this renderer before it has been cached.
	if b.creatingRenderer {
		return b.renderer
	}
	b.renderer = &markdownSelectionRenderer{WidgetRenderer: b.RichText.CreateRenderer(), block: b}
	b.renderer.shareFonts()
	b.creatingRenderer = true
	defer func() { b.creatingRenderer = false }()
	b.ensureReadingTheme()
	return b.renderer
}

func (b *markdownSelectableText) Resize(size fyne.Size) {
	if b.Size() == size {
		return
	}
	b.ensureReadingTheme()
	b.BaseWidget.Resize(size)
	b.RichText.Refresh()
}

func (b *markdownSelectableText) Cursor() desktop.Cursor {
	if b.linkAt(b.mousePosition) != nil || b.imageAt(b.mousePosition) != nil {
		return desktop.PointerCursor
	}
	return desktop.TextCursor
}
func (b *markdownSelectableText) MouseIn(e *desktop.MouseEvent)    { b.mousePosition = e.Position }
func (b *markdownSelectableText) MouseMoved(e *desktop.MouseEvent) { b.mousePosition = e.Position }
func (b *markdownSelectableText) MouseOut()                        { b.mousePosition = fyne.NewPos(-1, -1) }
func (b *markdownSelectableText) MouseDown(e *desktop.MouseEvent) {
	if e.Button != desktop.MouseButtonPrimary {
		return
	}
	if b.view.editor != nil && b.view.editor.window != nil {
		b.view.editor.window.Canvas().Focus(nil)
	}
	b.tripleTapped = false
	if !b.doubleTappedAt.IsZero() && time.Since(b.doubleTappedAt) <= fyne.CurrentApp().Driver().DoubleTapDelay() &&
		math.Abs(float64(e.Position.X-b.doubleTapPosition.X)) <= 6 && math.Abs(float64(e.Position.Y-b.doubleTapPosition.Y)) <= 6 {
		b.doubleTappedAt = time.Time{}
		b.tripleTapped = true
		b.selectRenderedLine(e.Position)
		return
	}
	b.doubleTappedAt = time.Time{}
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
	var pageOrigin fyne.Position
	if v.scroll != nil {
		pageOrigin = v.editor.app.Driver().AbsolutePositionForObject(v.scroll.Scroll).Subtract(v.scroll.Offset)
	}
	for _, candidate := range v.textBlocks {
		origin := pageOrigin.Add(candidate.pagePosition)
		if !candidate.positioned {
			origin = candidate.absolutePosition()
		}
		pos := e.AbsolutePosition.Subtract(origin)
		dx := max(0, max(-pos.X, pos.X-candidate.Size().Width))
		dy := max(0, max(-pos.Y, pos.Y-candidate.Size().Height))
		d := dx*dx + dy*dy
		if d < distance {
			best, distance = candidate, d
		}
	}
	origin := best.absolutePosition()
	v.selection.end = best.point(e.AbsolutePosition.Subtract(origin))
	v.refreshSelection()
}

func (b *markdownSelectableText) absolutePosition() fyne.Position {
	v := b.view
	if b.positioned && v.scroll != nil {
		origin := v.editor.app.Driver().AbsolutePositionForObject(v.scroll.Scroll)
		return origin.Add(b.pagePosition).Subtract(v.scroll.Offset)
	}
	return v.editor.app.Driver().AbsolutePositionForObject(b)
}
func (b *markdownSelectableText) DragEnd() {
	if b.view.selection != nil {
		b.view.selection.dragging = false
	}
}
func (b *markdownSelectableText) Tapped(e *fyne.PointEvent) {
	b.view.dismissOutline()
	if b.tripleTapped {
		return
	}
	if picture := b.imageAt(e.Position); picture != nil {
		b.view.showImagePreview(picture)
		return
	}
	if action := b.linkAt(e.Position); action != nil {
		action()
	}
}

// Double clicks select a Unicode word across adjacent formatting segments.
func (b *markdownSelectableText) DoubleTapped(e *fyne.PointEvent) {
	b.view.dismissOutline()
	if b.imageAt(e.Position) != nil || b.text == "" {
		return
	}
	b.doubleTappedAt, b.doubleTapPosition = time.Now(), e.Position
	offset := b.point(e.Position).offset
	// Hit testing returns the nearest caret; keep the trailing half of a glyph
	// in its own word rather than selecting the following space or punctuation.
	for _, f := range b.fragments() {
		if offset > f.start && offset <= f.start+len(f.text) && e.Position.Y >= f.position.Y && e.Position.Y < f.position.Y+f.size.Height && e.Position.X < f.position.X+f.width(f.text[:offset-f.start]) {
			_, size := utf8.DecodeLastRuneInString(b.text[:offset])
			offset -= size
			break
		}
	}
	if offset == len(b.text) {
		_, size := utf8.DecodeLastRuneInString(b.text)
		offset -= size
	}
	kind := func(r rune) int {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) || r == '_' {
			return 0
		}
		if unicode.IsSpace(r) {
			return 1
		}
		return 2
	}
	r, size := utf8.DecodeRuneInString(b.text[offset:])
	start, end, class := offset, offset+size, kind(r)
	for start > 0 {
		r, size = utf8.DecodeLastRuneInString(b.text[:start])
		if kind(r) != class {
			break
		}
		start -= size
	}
	for end < len(b.text) {
		r, size = utf8.DecodeRuneInString(b.text[end:])
		if kind(r) != class {
			break
		}
		end += size
	}
	b.selectRange(start, end)
}

func (b *markdownSelectableText) selectRange(start, end int) {
	b.view.selection = &markdownTextSelection{anchor: markdownTextPoint{block: b.index, offset: start}, end: markdownTextPoint{block: b.index, offset: end}}
	b.view.refreshSelection()
}

func (b *markdownSelectableText) selectRenderedLine(position fyne.Position) {
	point := b.point(position)
	fragments := b.fragments()
	for _, f := range fragments {
		if point.offset < f.start || point.offset > f.start+len(f.text) {
			continue
		}
		if position.Y < f.position.Y || position.Y >= f.position.Y+f.size.Height {
			continue
		}
		start, end := f.start, f.start+len(f.text)
		center := f.position.Y + f.size.Height/2
		for _, other := range fragments {
			if center >= other.position.Y && center < other.position.Y+other.size.Height {
				start, end = min(start, other.start), max(end, other.start+len(other.text))
			}
		}
		b.selectRange(start, end)
		return
	}
}

func (b *markdownSelectableText) imageAt(position fyne.Position) image.Image {
	if b.renderer == nil || !b.hasImages {
		return nil
	}
	for _, object := range b.renderer.WidgetRenderer.Objects() {
		if picture, ok := object.(*canvas.Image); ok && picture.Image != nil {
			origin, size := picture.Position(), picture.Size()
			bounds := picture.Image.Bounds().Size()
			aspect := float32(bounds.X) / float32(bounds.Y)
			fitted := fyne.NewSize(min(size.Width, size.Height*aspect), min(size.Height, size.Width/aspect))
			origin = origin.Add(fyne.NewPos((size.Width-fitted.Width)/2, (size.Height-fitted.Height)/2))
			size = fitted
			if position.X >= origin.X && position.X < origin.X+size.Width && position.Y >= origin.Y && position.Y < origin.Y+size.Height {
				return picture.Image
			}
		}
	}
	return nil
}

func (b *markdownSelectableText) linkAt(position fyne.Position) func() {
	if len(b.links) == 0 {
		return nil
	}
	for _, fragment := range b.fragments() {
		if position.X < fragment.position.X || position.X >= fragment.position.X+fragment.size.Width || position.Y < fragment.position.Y || position.Y >= fragment.position.Y+fragment.size.Height {
			continue
		}
		point := b.point(position)
		// Hit testing uses caret positions; the trailing half of the last glyph
		// still belongs to the link even if the nearest caret is its end.
		if point.offset == fragment.start+len(fragment.text) {
			point.offset--
		}
		for _, link := range b.links {
			if point.offset >= link.start && point.offset < link.end {
				return link.onTapped
			}
		}
	}
	return nil
}
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
	var changed fyne.CanvasObject
	for _, b := range v.textBlocks {
		start, end := b.selectionRange()
		searchStart, searchEnd := b.rangeFor(b.view.searchMatch)
		if b.renderer != nil && (b.renderer.selectionStart != start || b.renderer.selectionEnd != end || b.renderer.searchStart != searchStart || b.renderer.searchEnd != searchEnd) {
			b.renderer.updateSelection()
			changed = b
		}
	}
	if changed != nil {
		// Highlight rectangles are updated in place. Queue one frame instead
		// of a canvas lookup, thread check and refresh for every text block.
		if v.viewport != nil {
			changed = v.viewport
		}
		canvas.Refresh(changed)
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
	if b.renderer.fragmentsReady {
		return b.renderer.fragments
	}
	fragments := b.renderer.fragments[:0]
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
	b.renderer.fragments, b.renderer.fragmentsReady = fragments, true
	return fragments
}
func (b *markdownSelectableText) point(pos fyne.Position) markdownTextPoint {
	fragments := b.fragments()
	point := markdownTextPoint{block: b.index}
	distance := float32(math.MaxFloat32)
	nearest := -1
	for i, f := range fragments {
		dx := max(0, max(f.position.X-pos.X, pos.X-f.position.X-f.size.Width))
		dy := max(0, max(f.position.Y-pos.Y, pos.Y-f.position.Y-f.size.Height))
		d := dx*dx + dy*dy
		if d >= distance {
			continue
		}
		distance = d
		nearest = i
	}
	if nearest < 0 {
		return point
	}
	f := fragments[nearest]
	x := pos.X - f.position.X
	boundaries := make([]int, 0, len(f.text)+1)
	for i := range f.text {
		boundaries = append(boundaries, i)
	}
	boundaries = append(boundaries, len(f.text))
	// Shape prefixes only for the nearest fragment, including in long code blocks.
	index := sort.Search(len(boundaries)-1, func(i int) bool {
		left := f.width(f.text[:boundaries[i]])
		right := f.width(f.text[:boundaries[i+1]])
		return x < (left+right)/2
	})
	point.offset = f.start + boundaries[index]
	return point
}

type markdownSelectionRenderer struct {
	fyne.WidgetRenderer
	block                        *markdownSelectableText
	highlights, objects          []fyne.CanvasObject
	pool                         []*canvas.Rectangle
	selectionStart, selectionEnd int
	searchStart, searchEnd       int
	fragments                    []markdownTextFragment
	fragmentsReady               bool
	minimum                      fyne.Size
	minimumReady                 bool
	layoutSize                   fyne.Size
	laidOut                      bool
}

func (r *markdownSelectionRenderer) Layout(size fyne.Size) {
	// Newly visible lines/highlights make Fyne request another parent layout.
	// Their minimum sizes do not change the immutable text at the same width.
	if r.laidOut && r.layoutSize == size {
		return
	}
	r.block.ensureReadingTheme()
	r.fragmentsReady, r.minimumReady = false, false
	r.WidgetRenderer.Layout(size)
	r.shareFonts()
	r.layoutSize, r.laidOut = size, true
	r.updateSelection()
}
func (r *markdownSelectionRenderer) Refresh() {
	r.block.ensureReadingTheme()
	r.laidOut = false
	r.fragmentsReady, r.minimumReady = false, false
	r.WidgetRenderer.Refresh()
	r.shareFonts()
	r.updateSelection()
}
func (r *markdownSelectionRenderer) MinSize() fyne.Size {
	if !r.minimumReady {
		r.minimum = r.WidgetRenderer.MinSize()
		r.minimumReady = true
	}
	return r.minimum
}
func (r *markdownSelectionRenderer) Objects() []fyne.CanvasObject {
	objects := r.WidgetRenderer.Objects()
	b := r.block
	if b.positioned && b.view.scroll != nil {
		// Cull individual lines too: a single highlighted code block or prose
		// paragraph can span many screens. Retain complete hit-test geometry.
		scroll := b.view.scroll
		top := scroll.Offset.Y - scroll.Size().Height - b.pagePosition.Y
		bottom := scroll.Offset.Y + 2*scroll.Size().Height - b.pagePosition.Y
		r.objects = r.objects[:0]
		appendVisible := func(items []fyne.CanvasObject) {
			for _, object := range items {
				if object.Position().Y+object.Size().Height >= top && object.Position().Y <= bottom {
					r.objects = append(r.objects, object)
				}
			}
		}
		appendVisible(objects)
		appendVisible(r.highlights)
		return r.objects
	}
	if len(r.highlights) == 0 {
		return objects
	}
	r.objects = append(r.objects[:0], objects...)
	r.objects = append(r.objects, r.highlights...)
	return r.objects
}
func (block *markdownSelectableText) selectionRange() (start, end int) {
	return block.rangeFor(block.view.selection)
}
func (block *markdownSelectableText) rangeFor(selection *markdownTextSelection) (start, end int) {
	if selection == nil {
		return 0, 0
	}
	a, b := selection.anchor, selection.end
	if a.block > b.block || a.block == b.block && a.offset > b.offset {
		a, b = b, a
	}
	ok := a != b
	index := block.index
	if !ok || index < a.block || index > b.block {
		return 0, 0
	}
	start, end = 0, len(block.text)
	if index == a.block {
		start = a.offset
	}
	if index == b.block {
		end = b.offset
	}
	return start, end
}
func (r *markdownSelectionRenderer) updateSelection() {
	r.highlights = r.highlights[:0]
	start, end := r.block.selectionRange()
	r.selectionStart, r.selectionEnd = start, end
	r.searchStart, r.searchEnd = r.block.rangeFor(r.block.view.searchMatch)
	r.appendHighlight(r.searchStart, r.searchEnd, color.NRGBA{R: 255, G: 170, B: 0, A: 110})
	c := theme.ColorForWidget(theme.ColorNameSelection, r.block)
	red, green, blue, _ := c.RGBA()
	r.appendHighlight(start, end, color.NRGBA{R: uint8(red >> 8), G: uint8(green >> 8), B: uint8(blue >> 8), A: 85})
}
func (r *markdownSelectionRenderer) appendHighlight(start, end int, fill color.Color) {
	if start == end {
		return
	}
	for _, f := range r.block.fragments() {
		lo, hi := max(start, f.start)-f.start, min(end, f.start+len(f.text))-f.start
		if lo >= hi {
			continue
		}
		x := f.width(f.text[:lo])
		width := f.width(f.text[:hi]) - x
		// A translucent foreground works with RichText's inline-code backgrounds.
		if len(r.highlights) == len(r.pool) {
			r.pool = append(r.pool, canvas.NewRectangle(color.Transparent))
		}
		rect := r.pool[len(r.highlights)]
		rect.FillColor = fill
		rect.Move(f.position.Add(fyne.NewPos(x, 0)))
		rect.Resize(fyne.NewSize(width, f.size.Height))
		r.highlights = append(r.highlights, rect)
	}
}
