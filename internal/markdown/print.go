package markdown

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/url"
	"strings"
	"unicode"

	"codeberg.org/go-pdf/fpdf"
	"emanuelelongo/proview/internal/mermaid"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
)

const (
	printMargin     = 51.
	printLineHeight = 15.
	maxPrintPages   = 1000
)

// PrintedDocument is a paginated, searchable PDF, independent of the reading
// viewport, zoom, theme and asynchronous diagram widgets.
type PrintedDocument struct {
	Data          []byte
	PageCount     int
	Width, Height float64
}

// PrintPDF uses only already-loaded local resources. It never fetches images or
// invokes a browser. The AST, source and resource images remain immutable.
func (d *Document) PrintPDF(ctx context.Context) (PrintedDocument, error) {
	return d.layoutPDF(ctx, false)
}

// PreviewPDF lays out only the beginning of the document on a compact card.
// It stops at the first page boundary instead of shrinking the whole document.
func (d *Document) PreviewPDF(ctx context.Context) (PrintedDocument, error) {
	return d.layoutPDF(ctx, true)
}

var errPreviewFull = errors.New("Markdown preview is full")

func (d *Document) layoutPDF(ctx context.Context, preview bool) (PrintedDocument, error) {
	if err := ctx.Err(); err != nil {
		return PrintedDocument{}, err
	}
	d = d.resourceSnapshot()
	p := &markdownPrinter{ctx: ctx, doc: d, pdf: fpdf.New("P", "pt", "A4", ""), margin: printMargin, preview: preview, imageNames: make(map[image.Image]string), diagramRenderer: mermaid.NewRenderer()}
	if preview {
		p.pdf = fpdf.NewCustom(&fpdf.InitType{UnitStr: "pt", Size: fpdf.SizeType{Wd: 320, Ht: 240}})
		p.margin = 16
	}
	p.pageWidth, p.pageHeight = p.pdf.GetPageSize()
	p.bottom = p.pageHeight - p.margin
	p.pdf.SetMargins(p.margin, p.margin, p.margin)
	p.pdf.SetAutoPageBreak(false, p.margin)
	p.pdf.SetTitle(d.Name, true)
	p.pdf.SetCreator("Proview", true)
	for _, font := range []struct {
		family, style string
		data          []byte
	}{
		{"sans", "", goregular.TTF}, {"sans", "B", gobold.TTF}, {"sans", "I", goitalic.TTF}, {"sans", "BI", gobolditalic.TTF},
		{"mono", "", gomono.TTF}, {"mono", "B", gomonobold.TTF}, {"mono", "I", gomonoitalic.TTF}, {"mono", "BI", gomonobolditalic.TTF},
	} {
		p.pdf.AddUTF8FontFromBytes(font.family, font.style, font.data)
	}
	p.pdf.SetFooterFunc(func() {
		if preview {
			return
		}
		p.pdf.SetFont("sans", "", 8)
		p.pdf.SetTextColor(115, 115, 115)
		label := fmt.Sprint(p.pdf.PageNo())
		p.pdf.Text((p.pageWidth-p.pdf.GetStringWidth(label))/2, p.pageHeight-25, label)
	})
	if err := p.newPage(); err != nil {
		return PrintedDocument{}, err
	}
	if err := p.blocks(d.Root, p.margin, p.pageWidth-2*p.margin); err != nil && !(preview && errors.Is(err, errPreviewFull)) {
		return PrintedDocument{}, err
	}
	if err := ctx.Err(); err != nil {
		return PrintedDocument{}, err
	}
	var data bytes.Buffer
	if err := p.pdf.Output(&data); err != nil {
		return PrintedDocument{}, fmt.Errorf("Markdown print layout: %w", err)
	}
	if data.Len() > 64<<20 {
		return PrintedDocument{}, fmt.Errorf("Markdown print output exceeds 64 MiB")
	}
	return PrintedDocument{Data: data.Bytes(), PageCount: p.pdf.PageCount(), Width: p.pageWidth, Height: p.pageHeight}, nil
}

type printTextStyle struct {
	size                             float64
	mono, bold, italic, strike, code bool
	color                            [3]int
	link                             string
}

func printBodyStyle() printTextStyle { return printTextStyle{size: 10.5, color: [3]int{30, 30, 30}} }

type printSpan struct {
	text       string
	style      printTextStyle
	image      image.Image
	imageWidth float64
	alt        string
	check      *bool
}

type printLine []printSpan
type printMarker struct {
	text string
	x    float64
}

type markdownPrinter struct {
	ctx                              context.Context
	doc                              *Document
	pdf                              *fpdf.Fpdf
	pageWidth, pageHeight, bottom, y float64
	margin                           float64
	preview                          bool
	bars                             []float64
	marker                           *printMarker
	images, diagrams                 int
	imageNames                       map[image.Image]string
	imageBytes                       int
	diagramRenderer                  *mermaid.Renderer
}

func (p *markdownPrinter) newPage() error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	if p.preview && p.pdf.PageCount() > 0 {
		return errPreviewFull
	}
	if p.pdf.PageCount() >= maxPrintPages {
		return fmt.Errorf("Markdown exceeds the %d-page print limit", maxPrintPages)
	}
	p.pdf.AddPage()
	p.y = p.margin
	return p.pdf.Error()
}

func (p *markdownPrinter) room(height float64) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	if p.y+height > p.bottom {
		return p.newPage()
	}
	return p.pdf.Error()
}

func (p *markdownPrinter) gap(height float64) { p.y = math.Min(p.bottom, p.y+height) }

func (p *markdownPrinter) font(style printTextStyle) {
	family, face := "sans", ""
	if style.mono {
		family = "mono"
	}
	if style.bold {
		face += "B"
	}
	if style.italic {
		face += "I"
	}
	p.pdf.SetFont(family, face, style.size)
}

func (p *markdownPrinter) spanWidth(span printSpan) float64 {
	if span.check != nil {
		return span.style.size + 3
	}
	p.font(span.style)
	return p.pdf.GetStringWidth(printFontText(span.text))
}

// fpdf's embedded UTF-8 fonts use a BMP character map. Replace supplementary
// characters instead of handing the font subsetter an invalid glyph index.
func printFontText(text string) string {
	if strings.IndexFunc(text, func(r rune) bool { return r > 0xffff }) < 0 {
		return text
	}
	return strings.Map(func(r rune) rune {
		if r > 0xffff {
			return '\ufffd'
		}
		return r
	}, text)
}

func (p *markdownPrinter) lineWidth(line printLine) float64 {
	width := 0.
	for _, span := range line {
		width += p.spanWidth(span)
	}
	return width
}

// Wrapping measures the embedded font, including spaces in code. Long words
// break at rune boundaries rather than extending beyond the printable area.
func (p *markdownPrinter) wrap(spans []printSpan, width float64, preserve bool) ([]printLine, error) {
	width = math.Max(8, width)
	var lines []printLine
	var line printLine
	used := 0.
	flush := func() {
		if !preserve {
			for len(line) > 0 && strings.TrimSpace(line[len(line)-1].text) == "" && line[len(line)-1].check == nil {
				line = line[:len(line)-1]
			}
		}
		lines = append(lines, line)
		line, used = nil, 0
	}
	appendSpan := func(span printSpan) {
		used += p.spanWidth(span)
		if len(line) > 0 && span.check == nil && line[len(line)-1].check == nil && line[len(line)-1].style == span.style {
			line[len(line)-1].text += span.text
		} else {
			line = append(line, span)
		}
	}
	for _, span := range spans {
		if err := p.ctx.Err(); err != nil {
			return nil, err
		}
		if span.check != nil {
			if used+p.spanWidth(span) > width && len(line) > 0 {
				flush()
			}
			appendSpan(span)
			continue
		}
		var token strings.Builder
		space := false
		addToken := func() {
			if token.Len() == 0 {
				return
			}
			part := span
			part.text = token.String()
			token.Reset()
			if space && !preserve {
				if len(line) == 0 || strings.HasSuffix(line[len(line)-1].text, " ") {
					return
				}
				part.text = " "
			}
			w := p.spanWidth(part)
			if used+w <= width {
				appendSpan(part)
				return
			}
			if len(line) > 0 {
				flush()
			}
			if space && !preserve {
				return
			}
			if w <= width {
				appendSpan(part)
				return
			}
			for index, r := range part.text {
				if index%4096 == 0 && p.ctx.Err() != nil {
					return
				}
				piece := part
				piece.text = string(r)
				if used+p.spanWidth(piece) > width && len(line) > 0 {
					flush()
				}
				appendSpan(piece)
			}
		}
		for index, r := range span.text {
			if index%4096 == 0 {
				if err := p.ctx.Err(); err != nil {
					return nil, err
				}
			}
			if r == '\n' {
				addToken()
				flush()
				continue
			}
			isSpace := unicode.IsSpace(r)
			if token.Len() > 0 && isSpace != space {
				addToken()
			}
			space = isSpace
			token.WriteRune(r)
		}
		addToken()
		if err := p.ctx.Err(); err != nil {
			return nil, err
		}
	}
	if len(line) > 0 || len(lines) == 0 {
		lines = append(lines, line)
	}
	return lines, p.pdf.Error()
}

func (p *markdownPrinter) decorate(top, height float64) {
	p.pdf.SetDrawColor(160, 160, 160)
	p.pdf.SetLineWidth(2)
	for _, x := range p.bars {
		p.pdf.Line(x, top, x, top+height)
	}
}

func (p *markdownPrinter) drawLine(line printLine, x, width, top, height float64, align east.Alignment) {
	baseline := top + height*.78
	if p.marker != nil {
		p.font(printBodyStyle())
		p.pdf.SetTextColor(30, 30, 30)
		p.pdf.Text(p.marker.x, baseline, p.marker.text)
		p.marker = nil
	}
	switch align {
	case east.AlignCenter:
		x += math.Max(0, (width-p.lineWidth(line))/2)
	case east.AlignRight:
		x += math.Max(0, width-p.lineWidth(line))
	}
	for _, span := range line {
		w := p.spanWidth(span)
		p.font(span.style)
		p.pdf.SetTextColor(span.style.color[0], span.style.color[1], span.style.color[2])
		if span.check != nil {
			size := span.style.size * .75
			p.pdf.SetDrawColor(50, 50, 50)
			p.pdf.SetLineWidth(.7)
			p.pdf.Rect(x, baseline-size, size, size, "D")
			if *span.check {
				p.pdf.Line(x+size*.15, baseline-size*.5, x+size*.4, baseline-size*.2)
				p.pdf.Line(x+size*.4, baseline-size*.2, x+size*.85, baseline-size*.8)
			}
		} else {
			if span.style.code {
				p.pdf.SetFillColor(242, 242, 242)
				p.pdf.Rect(x, baseline-span.style.size*.85, w, span.style.size*1.1, "F")
			}
			p.pdf.Text(x, baseline, printFontText(span.text))
			if span.style.strike {
				p.pdf.SetDrawColor(span.style.color[0], span.style.color[1], span.style.color[2])
				p.pdf.SetLineWidth(.5)
				p.pdf.Line(x, baseline-span.style.size*.3, x+w, baseline-span.style.size*.3)
			}
		}
		if span.style.link != "" {
			p.pdf.LinkString(x, top, w, height, span.style.link)
			p.pdf.SetDrawColor(40, 80, 150)
			p.pdf.SetLineWidth(.4)
			p.pdf.Line(x, baseline+1, x+w, baseline+1)
		}
		x += w
	}
}

func (p *markdownPrinter) flowLines(lines []printLine, x, width, height float64) error {
	return p.flowLinesAligned(lines, x, width, height, east.AlignLeft)
}
func (p *markdownPrinter) flowLinesAligned(lines []printLine, x, width, height float64, align east.Alignment) error {
	for start := 0; start < len(lines); {
		if err := p.room(height * float64(min(2, len(lines)-start))); err != nil {
			return err
		}
		count := min(len(lines)-start, int((p.bottom-p.y)/height))
		if count <= 0 {
			return fmt.Errorf("invalid Markdown line height")
		}
		if len(lines)-start-count == 1 && count > 2 {
			count--
		}
		for _, line := range lines[start : start+count] {
			p.decorate(p.y, height)
			p.drawLine(line, x, width, p.y, height, align)
			p.y += height
		}
		start += count
	}
	return p.pdf.Error()
}

func (p *markdownPrinter) inline(parent ast.Node, style printTextStyle) []printSpan {
	var spans []printSpan
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.Text:
			text := string(n.Value(p.doc.Source))
			if !n.IsRaw() {
				text = html.UnescapeString(string(util.UnescapePunctuations(n.Value(p.doc.Source))))
			}
			if n.HardLineBreak() {
				text += "\n"
			} else if n.SoftLineBreak() {
				text += " "
			}
			spans = append(spans, printSpan{text: text, style: style})
		case *ast.String:
			spans = append(spans, printSpan{text: string(n.Value), style: style})
		case *ast.Emphasis:
			st := style
			if n.Level == 2 {
				st.bold = true
			} else {
				st.italic = true
			}
			spans = append(spans, p.inline(n, st)...)
		case *ast.CodeSpan:
			st := style
			st.mono = true
			st.code = true
			spans = append(spans, printSpan{text: strings.ReplaceAll(string(n.Text(p.doc.Source)), "\n", " "), style: st})
		case *ast.Link:
			st := style
			if printLink(string(n.Destination)) {
				st.link = string(n.Destination)
				st.color = [3]int{35, 75, 145}
			}
			spans = append(spans, p.inline(n, st)...)
		case *ast.AutoLink:
			st := style
			destination := string(n.URL(p.doc.Source))
			if n.AutoLinkType == ast.AutoLinkEmail {
				destination = "mailto:" + destination
			}
			if printLink(destination) {
				st.link = destination
				st.color = [3]int{35, 75, 145}
			}
			spans = append(spans, printSpan{text: string(n.Label(p.doc.Source)), style: st})
		case *ast.Image:
			alt := string(n.Text(p.doc.Source))
			if picture := p.doc.Image(string(n.Destination)); picture != nil {
				spans = append(spans, printSpan{style: style, image: picture, imageWidth: float64(ImageWidth(n)) * .75, alt: alt})
			} else {
				spans = append(spans, printSpan{text: "[Image: " + alt + "]", style: style})
			}
		case *east.Strikethrough:
			st := style
			st.strike = true
			spans = append(spans, p.inline(n, st)...)
		case *east.TaskCheckBox:
			checked := n.IsChecked
			spans = append(spans, printSpan{style: style, check: &checked})
		case *ast.RawHTML:
			var text strings.Builder
			for i := 0; i < n.Segments.Len(); i++ {
				segment := n.Segments.At(i)
				text.Write(segment.Value(p.doc.Source))
			}
			st := style
			st.mono = true
			spans = append(spans, printSpan{text: text.String(), style: st})
		default:
			spans = append(spans, p.inline(n, style)...)
		}
	}
	return spans
}

func printLink(destination string) bool {
	u, err := url.Parse(destination)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto")
}

func (p *markdownPrinter) prose(n ast.Node, style printTextStyle, x, width, height float64) error {
	alignment := east.AlignLeft
	if align, ok := n.AttributeString("align"); ok {
		switch align {
		case "center":
			alignment = east.AlignCenter
		case "right":
			alignment = east.AlignRight
		}
	}
	return p.flowSpansAligned(p.inline(n, style), x, width, height, alignment)
}

func (p *markdownPrinter) flowSpans(spans []printSpan, x, width, height float64) error {
	return p.flowSpansAligned(spans, x, width, height, east.AlignLeft)
}
func (p *markdownPrinter) flowSpansAligned(spans []printSpan, x, width, height float64, align east.Alignment) error {
	var text []printSpan
	flush := func() error {
		if len(text) == 0 {
			return nil
		}
		lines, err := p.wrap(text, width, false)
		text = nil
		if err != nil {
			return err
		}
		return p.flowLinesAligned(lines, x, width, height, align)
	}
	for _, span := range spans {
		if span.image == nil {
			text = append(text, span)
			continue
		}
		if err := flush(); err != nil {
			return err
		}
		naturalWidth := float64(span.image.Bounds().Dx()) * .75
		if span.imageWidth > 0 {
			naturalWidth = span.imageWidth
		}
		if err := p.pictureWidth(span.image, x, width, naturalWidth); err != nil {
			return err
		}
	}
	return flush()
}

func (p *markdownPrinter) picture(picture image.Image, x, width float64) error {
	return p.pictureWidth(picture, x, width, float64(picture.Bounds().Dx())*.75)
}

func (p *markdownPrinter) pictureWidth(picture image.Image, x, width, naturalWidth float64) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	b := picture.Bounds()
	if b.Empty() {
		return nil
	}
	w := math.Min(width, naturalWidth)
	h := w * float64(b.Dy()) / float64(b.Dx())
	if h > p.bottom-p.margin-12 {
		scale := (p.bottom - p.margin - 12) / h
		w *= scale
		h *= scale
	}
	if err := p.room(h + 8); err != nil {
		return err
	}
	if p.marker != nil {
		p.drawLine(nil, x, width, p.y, printLineHeight, east.AlignLeft)
	}
	name, known := p.imageNames[picture]
	options := fpdf.ImageOptions{ImageType: "PNG"}
	if !known {
		var data bytes.Buffer
		if err := png.Encode(&data, picture); err != nil {
			return err
		}
		p.imageBytes += data.Len()
		if p.imageBytes > 48<<20 {
			return fmt.Errorf("Markdown print images exceed 48 MiB")
		}
		p.images++
		name = fmt.Sprintf("markdown-image-%d", p.images)
		p.pdf.RegisterImageOptionsReader(name, options, &data)
		p.imageNames[picture] = name
	}
	p.decorate(p.y, h)
	p.pdf.ImageOptions(name, x+(width-w)/2, p.y, w, h, false, options, 0, "")
	p.y += h + 8
	return p.pdf.Error()
}

func (p *markdownPrinter) blocks(parent ast.Node, x, width float64) error {
	if width < 24 {
		return fmt.Errorf("Markdown nesting exceeds printable width")
	}
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		switch n := child.(type) {
		case *ast.Paragraph, *ast.TextBlock:
			if err := p.prose(n, printBodyStyle(), x, width, printLineHeight); err != nil {
				return err
			}
			p.gap(5)
		case *ast.Heading:
			st := printBodyStyle()
			st.bold = true
			st.size = []float64{24, 20, 17, 14, 12, 11}[min(6, max(1, n.Level))-1]
			h := st.size * 1.3
			if err := p.room(h + 2*printLineHeight + 10); err != nil {
				return err
			}
			p.gap(9)
			if err := p.prose(n, st, x, width, h); err != nil {
				return err
			}
			p.gap(6)
		case *ast.FencedCodeBlock:
			if err := p.code(n, string(n.Language(p.doc.Source)), x, width); err != nil {
				return err
			}
		case *ast.CodeBlock:
			if err := p.code(n, "", x, width); err != nil {
				return err
			}
		case *ast.HTMLBlock:
			source := Code(p.doc.Source, n)
			if n.HasClosure() {
				source += string(n.ClosureLine.Value(p.doc.Source))
			}
			if err := p.codeText(source, nil, x, width); err != nil {
				return err
			}
		case *ast.List:
			index := n.Start
			for item := n.FirstChild(); item != nil; item = item.NextSibling() {
				marker := "•"
				if n.IsOrdered() {
					marker = fmt.Sprintf("%d.", index)
					index++
				}
				if first := item.FirstChild(); first != nil && first.FirstChild() != nil {
					if _, ok := first.FirstChild().(*east.TaskCheckBox); ok {
						marker = ""
					}
				}
				previous := p.marker
				p.marker = &printMarker{text: marker, x: x}
				indent := math.Max(19, p.spanWidth(printSpan{text: marker, style: printBodyStyle()})+6)
				if err := p.blocks(item, x+indent, width-indent); err != nil {
					return err
				}
				p.marker = previous
			}
			p.gap(3)
		case *Details:
			title := ast.NewParagraph()
			title.AppendChild(title, ast.NewString([]byte(n.Summary)))
			style := printBodyStyle()
			style.bold = true
			if err := p.prose(title, style, x, width, printLineHeight); err != nil {
				return err
			}
			if err := p.blocks(n, x+14, width-14); err != nil {
				return err
			}
		case *Admonition:
			st := printBodyStyle()
			st.bold = true
			title := ast.NewParagraph()
			title.AppendChild(title, ast.NewString([]byte(n.Label)))
			if err := p.prose(title, st, x+14, width-14, printLineHeight); err != nil {
				return err
			}
			p.bars = append(p.bars, x+3)
			if err := p.blocks(n, x+14, width-14); err != nil {
				return err
			}
			p.bars = p.bars[:len(p.bars)-1]
		case *ast.Blockquote:
			p.bars = append(p.bars, x+3)
			if err := p.blocks(n, x+14, width-14); err != nil {
				return err
			}
			p.bars = p.bars[:len(p.bars)-1]
		case *ast.ThematicBreak:
			if err := p.room(16); err != nil {
				return err
			}
			p.pdf.SetDrawColor(180, 180, 180)
			p.pdf.SetLineWidth(.5)
			p.pdf.Line(x, p.y+7, x+width, p.y+7)
			p.y += 16
		case *east.Table:
			if err := p.table(n, x, width); err != nil {
				return err
			}
		default:
			if err := p.blocks(n, x, width); err != nil {
				return err
			}
		}
	}
	return p.pdf.Error()
}

func (p *markdownPrinter) code(n ast.Node, language string, x, width float64) error {
	source := Code(p.doc.Source, n)
	if strings.EqualFold(language, "mermaid") {
		p.diagrams++
		var err error
		if p.diagrams <= 16 {
			result, e := p.diagramRenderer.Render(p.ctx, source, mermaid.Options{Width: int(width * 4 / 3), FontScale: 1, PixelScale: 3, Background: color.NRGBA{R: 255, G: 255, B: 255, A: 255}})
			err = e
			if err == nil {
				return p.pictureWidth(result.Image, x, width, result.Width*.75)
			}
		} else {
			err = mermaid.ErrLimit
		}
		if p.ctx.Err() != nil {
			return p.ctx.Err()
		}
		st := printBodyStyle()
		st.italic = true
		st.size = 9
		st.color = [3]int{100, 100, 100}
		lines, e := p.wrap([]printSpan{{text: mermaid.Message(err), style: st}}, width, false)
		if e != nil {
			return e
		}
		if e = p.flowLines(lines, x, width, 13); e != nil {
			return e
		}
	}
	return p.codeText(source, p.doc.TokensFor(n), x, width)
}

func (p *markdownPrinter) codeText(source string, tokens []chroma.Token, x, width float64) error {
	st := printBodyStyle()
	st.mono = true
	st.size = 9
	var spans []printSpan
	column := 0
	expand := func(s string) string {
		var out strings.Builder
		for _, r := range s {
			switch r {
			case '\t':
				count := 4 - column%4
				out.WriteString(strings.Repeat(" ", count))
				column += count
			case '\n':
				out.WriteRune(r)
				column = 0
			default:
				out.WriteRune(r)
				column++
			}
		}
		return out.String()
	}
	if len(tokens) == 0 {
		spans = []printSpan{{text: expand(strings.TrimSuffix(source, "\n")), style: st}}
	} else {
		palette := styles.Get("github")
		if palette == nil {
			palette = styles.Fallback
		}
		for _, token := range tokens {
			style := st
			entry := palette.Get(token.Type)
			if entry.Colour.IsSet() {
				style.color = [3]int{int(entry.Colour.Red()), int(entry.Colour.Green()), int(entry.Colour.Blue())}
			}
			style.bold = entry.Bold == chroma.Yes
			style.italic = entry.Italic == chroma.Yes
			spans = append(spans, printSpan{text: expand(token.Value), style: style})
		}
		if len(spans) > 0 {
			spans[len(spans)-1].text = strings.TrimSuffix(spans[len(spans)-1].text, "\n")
		}
	}
	lines, err := p.wrap(spans, width-16, true)
	if err != nil {
		return err
	}
	const lineHeight = 12.5
	total := float64(len(lines))*lineHeight + 16
	if total <= p.bottom-p.margin {
		if err := p.room(total); err != nil {
			return err
		}
	}
	for start := 0; start < len(lines); {
		if err := p.room(lineHeight + 16); err != nil {
			return err
		}
		count := min(len(lines)-start, int((p.bottom-p.y-16)/lineHeight))
		if count < 1 {
			return fmt.Errorf("code block exceeds printable width")
		}
		h := float64(count)*lineHeight + 16
		p.decorate(p.y, h)
		p.pdf.SetFillColor(246, 246, 246)
		p.pdf.SetDrawColor(215, 215, 215)
		p.pdf.SetLineWidth(.4)
		p.pdf.Rect(x, p.y, width, h, "DF")
		for i, line := range lines[start : start+count] {
			p.drawLine(line, x+8, width-16, p.y+8+float64(i)*lineHeight, lineHeight, east.AlignLeft)
		}
		p.y += h
		start += count
	}
	p.gap(7)
	return p.pdf.Error()
}
