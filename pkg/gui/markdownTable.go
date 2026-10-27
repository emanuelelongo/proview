package gui

import (
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	east "github.com/yuin/goldmark/extension/ast"
)

func (v *markdownView) table(n *east.Table) fyne.CanvasObject {
	l := &markdownTableLayout{view: v, columns: len(n.Alignments)}
	var objects []fyne.CanvasObject
	for row := n.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []*markdownText
		_, header := row.(*east.TableHeader)
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			style := widget.RichTextStyleInline
			style.TextStyle.Bold = header
			switch cell.(*east.TableCell).Alignment {
			case east.AlignCenter:
				style.Alignment = fyne.TextAlignCenter
			case east.AlignRight:
				style.Alignment = fyne.TextAlignTrailing
			}
			text := v.prose(cell, style).(*markdownText)
			cells = append(cells, text)
			objects = append(objects, text)
		}
		l.rows = append(l.rows, cells)
		objects = append(objects, widget.NewSeparator())
	}
	table := container.New(l, objects...)
	scroll := container.NewHScroll(table)
	scroll.OnScrolled = func(fyne.Position) { v.updateTextViewport() }
	surface := newMarkdownHorizontalScrollSurface(v, scroll)
	return container.New(&markdownTableViewportLayout{table: l, scroll: scroll}, scroll, surface)
}

// Rows share column measurements. Short tables keep their natural width; longer
// cells wrap between words, with horizontal scrolling only when those words or
// the number of columns cannot fit. Width and height preparation is cached so
// scrolling never reshapes the whole table.
type markdownTableLayout struct {
	view                       *markdownView
	columns                    int
	rows                       [][]*markdownText
	available, preparedWidth   float32
	measuredTheme              fyne.Theme
	textSize, padding, inset   float32
	minimum, preferred, widths []float32
	heights                    []float32
	minimumSize                fyne.Size
}

func (l *markdownTableLayout) prepare() {
	if !l.view.themeReady || l.columns == 0 || len(l.rows) == 0 || len(l.rows[0]) == 0 {
		return
	}
	text := l.rows[0][0].text
	currentTheme := fyne.CurrentApp().Settings().Theme()
	textSize := theme.SizeForWidget(theme.SizeNameText, text)
	padding := theme.SizeForWidget(theme.SizeNamePadding, text)
	inset := theme.SizeForWidget(theme.SizeNameInnerPadding, text)
	if l.measuredTheme != currentTheme || l.textSize != textSize || l.padding != padding || l.inset != inset {
		l.measuredTheme, l.textSize, l.padding, l.inset = currentTheme, textSize, padding, inset
		l.minimum, l.preferred = make([]float32, l.columns), make([]float32, l.columns)
		for _, row := range l.rows {
			for col, cell := range row {
				minimum, preferred := markdownCellWidths(cell.text)
				l.minimum[col] = max(l.minimum[col], minimum+2*padding)
				l.preferred[col] = max(l.preferred[col], preferred+2*padding)
			}
		}
		l.preparedWidth = -1
	}
	available := l.available
	if available <= 0 {
		available = l.view.columnWidth(0)
	}
	if l.preparedWidth == available {
		return
	}
	l.preparedWidth = available
	minimum, preferred := float32(0), float32(0)
	for col := range l.columns {
		minimum += l.minimum[col]
		preferred += l.preferred[col]
	}
	width := max(minimum, min(preferred, available))
	l.widths = make([]float32, l.columns)
	for col := range l.widths {
		l.widths[col] = l.minimum[col]
		if preferred > minimum {
			l.widths[col] += (width - minimum) * (l.preferred[col] - l.minimum[col]) / (preferred - minimum)
		}
	}
	l.heights = make([]float32, len(l.rows))
	height := float32(0)
	separator := theme.SizeForWidget(theme.SizeNameSeparatorThickness, text)
	for i, row := range l.rows {
		for col, cell := range row {
			cell.Resize(fyne.NewSize(max(1, l.widths[col]-2*padding), cell.Size().Height))
			l.heights[i] = max(l.heights[i], cell.MinSize().Height+2*padding)
		}
		height += l.heights[i] + separator
	}
	l.minimumSize = fyne.NewSize(width, height)
}

func (l *markdownTableLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	l.prepare()
	return l.minimumSize
}

func (l *markdownTableLayout) Layout(objects []fyne.CanvasObject, _ fyne.Size) {
	l.prepare()
	if len(l.heights) == 0 {
		return
	}
	y, index := float32(0), 0
	for row, cells := range l.rows {
		x := float32(0)
		for col, cell := range cells {
			cell.Move(fyne.NewPos(x+l.padding, y+l.padding))
			cell.Resize(fyne.NewSize(max(1, l.widths[col]-2*l.padding), cell.MinSize().Height))
			x += l.widths[col]
			index++
		}
		y += l.heights[row]
		separator := objects[index]
		separator.Move(fyne.NewPos(0, y))
		separator.Resize(fyne.NewSize(l.minimumSize.Width, separator.MinSize().Height))
		y += separator.Size().Height
		index++
	}
}

type markdownTableViewportLayout struct {
	table  *markdownTableLayout
	scroll *container.Scroll
}

func (l *markdownTableViewportLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	l.table.prepare()
	height := l.table.minimumSize.Height
	if l.table.minimumSize.Width > l.table.preparedWidth {
		height += theme.ScrollBarSize()
	}
	return fyne.NewSize(100, height)
}

func (l *markdownTableViewportLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	l.table.available = size.Width
	minimum := l.MinSize(objects)
	l.scroll.SetMinSize(minimum)
	for _, object := range objects {
		object.Move(fyne.Position{})
		object.Resize(fyne.NewSize(size.Width, minimum.Height))
	}
}

// Measure styled runs without temporarily changing RichText's wrapping mode.
// A word can span adjacent emphasis/code segments, so keep accumulating its
// width until whitespace rather than treating each style boundary as a break.
func markdownCellWidths(text *widget.RichText) (minimum, preferred float32) {
	line, word := float32(0), float32(0)
	for _, segment := range text.Segments {
		style := widget.RichTextStyleInline
		if s, ok := segment.(*widget.TextSegment); ok {
			style = s.Style
		}
		if image, ok := segment.(*markdownImageSegment); ok {
			width := image.Visual().MinSize().Width
			minimum, preferred = max(minimum, word, width), max(preferred, line, width)
			line, word = 0, 0
			continue
		}
		sizeName := style.SizeName
		if sizeName == "" {
			sizeName = theme.SizeNameText
		}
		fontSize := theme.SizeForWidget(sizeName, text)
		font := text.Theme().Font(style.TextStyle)
		measure := func(value string) float32 {
			size, _ := fyne.CurrentApp().Driver().RenderedTextSize(value, fontSize, style.TextStyle, font)
			return size.Width
		}
		value, start := segment.Textual(), 0
		for i, r := range value {
			if !unicode.IsSpace(r) {
				continue
			}
			width := measure(value[start:i])
			word += width
			line += width
			minimum = max(minimum, word)
			word = 0
			if r == '\n' {
				preferred = max(preferred, line)
				line = 0
			} else {
				line += measure(string(r))
			}
			start = i + len(string(r))
		}
		width := measure(value[start:])
		word += width
		line += width
	}
	padding := 2 * theme.SizeForWidget(theme.SizeNameInnerPadding, text)
	// Leave a rounding point so a naturally sized cell does not wrap its last word.
	minimum = max(minimum, word) + padding + 1
	preferred = max(preferred, line) + padding + 1
	return minimum, preferred
}
