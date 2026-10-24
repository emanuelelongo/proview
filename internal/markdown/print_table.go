package markdown

import (
	"fmt"
	"math"
	"strings"

	east "github.com/yuin/goldmark/extension/ast"
)

type printTableCell struct {
	lines []printLine
	align east.Alignment
}

type printTableRow struct {
	cells []printTableCell
	lines int
}

const tableLineHeight = 13.

func (p *markdownPrinter) table(table *east.Table, x, width float64) error {
	columns := len(table.Alignments)
	if columns == 0 {
		return nil
	}
	// Extremely wide tables remain readable as labeled records instead of
	// shrinking text to an illegible size or clipping columns off the paper.
	if columns > 8 || width/float64(columns) < 42 {
		return p.tableRecords(table, x, width)
	}
	weights := make([]float64, columns)
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		column := 0
		for cell := row.FirstChild(); cell != nil && column < columns; cell = cell.NextSibling() {
			spans := p.inline(cell, printBodyStyle())
			measure := 0.
			for _, span := range spans {
				if span.image == nil {
					measure += p.spanWidth(span)
				}
			}
			weights[column] = math.Max(weights[column], math.Min(180, math.Max(30, measure)))
			column++
		}
		if err := p.ctx.Err(); err != nil {
			return err
		}
	}
	weight := 0.
	for i := range weights {
		weights[i] = math.Max(weights[i], 30)
		weight += weights[i]
	}
	widths := make([]float64, columns)
	minimum := math.Min(50, width/float64(columns)*.7)
	for i := range widths {
		widths[i] = minimum + (width-minimum*float64(columns))*weights[i]/weight
	}
	var rows []printTableRow
	var pictures []printSpan
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		_, header := row.(*east.TableHeader)
		prepared := printTableRow{lines: 1}
		column := 0
		for cell := row.FirstChild(); cell != nil && column < columns; cell = cell.NextSibling() {
			style := printBodyStyle()
			style.size = 9.5
			style.bold = header
			spans := p.inline(cell, style)
			for i, span := range spans {
				if span.image != nil {
					pictures = append(pictures, span)
					spans[i] = printSpan{text: "[Image: " + span.alt + "]", style: style}
				}
			}
			lines, err := p.wrap(spans, widths[column]-12, false)
			if err != nil {
				return err
			}
			align := east.AlignLeft
			if c, ok := cell.(*east.TableCell); ok {
				align = c.Alignment
			}
			prepared.cells = append(prepared.cells, printTableCell{lines: lines, align: align})
			prepared.lines = max(prepared.lines, len(lines))
			column++
		}
		rows = append(rows, prepared)
	}
	if len(rows) == 0 {
		return nil
	}
	header := rows[0]
	headerHeight := 8 + float64(header.lines)*tableLineHeight
	fullHeight := p.bottom - printMargin
	if headerHeight > fullHeight/4 {
		return p.tableRecords(table, x, width)
	}
	if err := p.room(headerHeight + tableLineHeight + 8); err != nil {
		return err
	}
	p.drawTableRow(header, widths, x, 0, header.lines, true)
	repeatHeader := func() error {
		if err := p.newPage(); err != nil {
			return err
		}
		p.drawTableRow(header, widths, x, 0, header.lines, true)
		return p.pdf.Error()
	}
	for _, row := range rows[1:] {
		h := 8 + float64(row.lines)*tableLineHeight
		if h <= fullHeight-headerHeight && p.y+h > p.bottom {
			if err := repeatHeader(); err != nil {
				return err
			}
		}
		for start := 0; start < row.lines; {
			available := int((p.bottom - p.y - 8) / tableLineHeight)
			if available < 1 {
				if err := repeatHeader(); err != nil {
					return err
				}
				available = int((p.bottom - p.y - 8) / tableLineHeight)
			}
			count := min(row.lines-start, available)
			p.drawTableRow(row, widths, x, start, count, false)
			start += count
			if start < row.lines {
				if err := repeatHeader(); err != nil {
					return err
				}
			}
			if err := p.ctx.Err(); err != nil {
				return err
			}
		}
	}
	p.gap(7)
	// Inline table images are printed below the table at a useful width rather
	// than reduced to a tiny cell. Their alt text identifies the source cell.
	for _, picture := range pictures {
		if err := p.picture(picture.image, x, width); err != nil {
			return err
		}
	}
	return p.pdf.Error()
}

func (p *markdownPrinter) drawTableRow(row printTableRow, widths []float64, x float64, start, count int, header bool) {
	height := 8 + float64(count)*tableLineHeight
	p.decorate(p.y, height)
	for column, width := range widths {
		p.pdf.SetDrawColor(200, 200, 200)
		p.pdf.SetLineWidth(.4)
		if header {
			p.pdf.SetFillColor(237, 239, 242)
		} else {
			p.pdf.SetFillColor(255, 255, 255)
		}
		p.pdf.Rect(x, p.y, width, height, "DF")
		if column < len(row.cells) {
			cell := row.cells[column]
			for i := start; i < min(start+count, len(cell.lines)); i++ {
				p.drawLine(cell.lines[i], x+6, width-12, p.y+4+float64(i-start)*tableLineHeight, tableLineHeight, cell.align)
			}
		}
		x += width
	}
	p.y += height
}

func (p *markdownPrinter) tableRecords(table *east.Table, x, width float64) error {
	header := table.FirstChild()
	if header == nil {
		return nil
	}
	if header.NextSibling() == nil {
		style := printBodyStyle()
		style.bold = true
		for cell := header.FirstChild(); cell != nil; cell = cell.NextSibling() {
			if err := p.prose(cell, style, x, width, printLineHeight); err != nil {
				return err
			}
			p.gap(4)
		}
		return nil
	}
	var labels []string
	for cell := header.FirstChild(); cell != nil; cell = cell.NextSibling() {
		var label strings.Builder
		for _, span := range p.inline(cell, printBodyStyle()) {
			label.WriteString(span.text)
		}
		labels = append(labels, label.String())
	}
	for row := header.NextSibling(); row != nil; row = row.NextSibling() {
		column := 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			label := fmt.Sprintf("Column %d", column+1)
			if column < len(labels) && labels[column] != "" {
				label = labels[column]
			}
			style := printBodyStyle()
			style.bold = true
			spans := []printSpan{{text: label + ": ", style: style}}
			spans = append(spans, p.inline(cell, printBodyStyle())...)
			if err := p.flowSpans(spans, x, width, printLineHeight); err != nil {
				return err
			}
			p.gap(4)
			column++
		}
		p.gap(8)
	}
	return nil
}
