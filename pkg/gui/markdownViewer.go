package gui

import (
	"context"
	"emanuelelongo/proview/internal/markdown"
	"emanuelelongo/proview/pkg/document"
	"errors"
	"fmt"
	"html"
	"image"
	"image/color"
	"math"
	"net/url"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"
)

type markdownView struct {
	thumbnailCapturing     bool
	thumbnailPending       bool
	themeReady             bool
	doc                    *markdown.Document
	scroll                 *container.Scroll
	page                   *markdownPage
	body                   *fyne.Container
	content                *fyne.Container
	anchors                map[string]fyne.CanvasObject
	scale                  float32
	editor                 *Gui
	diagramContext         context.Context
	cancelDiagrams         context.CancelFunc
	diagrams               []*markdownDiagram
	pendingReadingOffset   *fyne.Position
	restoringReadingOffset bool
	cropGeneration         uint64
	pendingDiagramUpdates  []func()
	crop                   *cropOverlay
	cropCapturing          bool
}

func (g *Gui) showingMarkdown() bool { return g.markdownView != nil }
func (g *Gui) clearMarkdown() {
	if g.markdownView != nil {
		g.markdownView.cancelCrop()
		if g.markdownView.cancelDiagrams != nil {
			g.markdownView.cancelDiagrams()
		}
		g.markdownView = nil
	}
	if g.markdownPanel != nil {
		g.markdownPanel.Hide()
		g.markdownPanel.RemoveAll()
	}
}
func (g *Gui) addMarkdown(d *markdown.Document, target fyne.URI) {
	// Match the existing PDF/non-PDF opening policy.
	if g.document != nil && g.document.Format() == document.FormatPDF {
		editor := g.newEditor()
		editor.addMarkdown(d, target)
		editor.window.Show()
		return
	}
	g.activateEditor()
	g.prepareDocumentSwitch()
	entry := &editorDocument{markdown: d, target: target}
	g.documents = append(g.documents, entry)
	g.activeDocument = entry
	g.document = nil
	g.showMarkdown(d)
	g.captureDocument()
}
func (g *Gui) showMarkdown(d *markdown.Document) {
	g.clearModel()
	g.clearMarkdown()
	g.document = nil
	g.saveTarget = nil
	g.imageScroll.Hide()
	g.pdfScroll.Hide()
	g.annotationControls.Hide()
	g.annotationPropertyScroll.Hide()
	scale := float32(1)
	if g.activeDocument != nil && g.activeDocument.markdown == d && g.activeDocument.markdownScale > 0 {
		scale = g.activeDocument.markdownScale
	}
	v := &markdownView{doc: d, scale: scale, editor: g}
	g.markdownView = v
	v.rebuild()
	g.markdownPanel.Add(newMarkdownViewport(v))
	g.markdownPanel.Show()
	g.updateHistoryActions()
}
func (v *markdownView) rebuild() {
	v.cancelCrop()
	if v.cancelDiagrams != nil {
		v.cancelDiagrams()
	}
	v.diagramContext, v.cancelDiagrams = context.WithCancel(context.Background())
	v.diagrams = nil
	offset := fyne.Position{}
	if v.scroll != nil {
		offset = v.scroll.Offset
	}
	v.anchors = map[string]fyne.CanvasObject{}
	v.themeReady = false
	v.content = container.NewVBox(v.blocks(v.doc.Root)...)
	// Keep a readable line length while allowing narrow windows to reflow.
	v.body = container.New(&markdownPageLayout{view: v}, newMarkdownPageSurface(), v.content)
	themed := container.NewThemeOverride(v.body, &markdownTheme{scale: v.scale})
	v.themeReady = true
	v.body.Refresh()
	v.page = newMarkdownPage(v, themed)
	if v.scroll == nil {
		v.scroll = container.NewVScroll(v.page)
		v.scroll.OnScrolled = func(fyne.Position) {
			if !v.restoringReadingOffset {
				v.pendingReadingOffset = nil
			}
		}
	} else {
		v.scroll.Content = v.page
		v.scroll.Refresh()
	}
	v.restoreReadingOffset(offset)
}
func (v *markdownView) zoom(delta int) {
	v.scale = float32(math.Max(.75, math.Min(2, float64(v.scale)+float64(delta)*.125)))
	v.rebuild()
}

const (
	markdownPageMargin  float32 = 16
	markdownPagePadding float32 = 24
	markdownTextWidth   float32 = 900
)

type markdownPageLayout struct{ view *markdownView }

func (v *markdownView) viewportSize() fyne.Size {
	if v != nil {
		if v.scroll != nil && v.scroll.Size().Width > 0 {
			return v.scroll.Size()
		}
		if v.editor != nil {
			if v.editor.markdownPanel != nil && v.editor.markdownPanel.Size().Width > 0 {
				return v.editor.markdownPanel.Size()
			}
			if v.editor.window != nil && v.editor.window.Canvas().Size().Width > 0 {
				return v.editor.window.Canvas().Size()
			}
		}
	}
	return fyne.NewSize(markdownTextWidth+2*(markdownPageMargin+markdownPagePadding), 0)
}
func (v *markdownView) columnWidth(totalWidth float32) float32 {
	if totalWidth <= 0 {
		totalWidth = v.viewportSize().Width
	}
	return max(1, min(markdownTextWidth, totalWidth-2*(markdownPageMargin+markdownPagePadding)))
}
func (v *markdownView) themeState() (color.Color, color.Color, float32) {
	var target fyne.Widget
	if v != nil && v.body != nil && len(v.body.Objects) > 0 {
		target, _ = v.body.Objects[0].(fyne.Widget)
	}
	return theme.ColorForWidget(theme.ColorNameForeground, target),
		theme.ColorForWidget(theme.ColorNameBackground, target),
		theme.SizeForWidget(theme.SizeNameText, target)
}
func (v *markdownView) relayout() {
	if v.body != nil && v.body.Layout != nil {
		v.body.Layout.Layout(v.body.Objects, v.body.Size())
	}
	if v.content != nil && v.content.Layout != nil {
		v.content.Layout.Layout(v.content.Objects, v.content.Size())
	}
	for _, diagram := range v.diagrams {
		if diagram.box != nil && diagram.box.Layout != nil {
			diagram.box.Layout.Layout(diagram.box.Objects, diagram.box.Size())
		}
		diagram.request()
	}
}

func (l *markdownPageLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	h := 2 * (markdownPageMargin + markdownPagePadding)
	if len(objects) > 1 {
		if l.view != nil {
			width := l.view.columnWidth(0)
			if objects[1].Size().Width != width {
				objects[1].Resize(fyne.NewSize(width, objects[1].Size().Height))
			}
		}
		h += objects[1].MinSize().Height
	}
	return fyne.NewSize(180, h)
}
func (l *markdownPageLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 2 {
		return
	}
	objects[0].Move(fyne.Position{})
	objects[0].Resize(size)
	width := l.view.columnWidth(size.Width)
	content := objects[1]
	content.Resize(fyne.NewSize(width, content.MinSize().Height))
	content.Move(fyne.NewPos((size.Width-width)/2, markdownPageMargin+markdownPagePadding))
	if h := content.MinSize().Height; content.Size().Height != h {
		content.Resize(fyne.NewSize(width, h))
	}
}

// markdownPage shields the ThemeOverride from resetting global Fyne font caches
// on scroll or diagram layout refreshes when the theme itself has not changed.
type markdownPage struct {
	widget.BaseWidget
	view           *markdownView
	themed         *container.ThemeOverride
	lastFG, lastBG color.Color
	lastSize       float32
	refreshed      bool
}

func newMarkdownPage(view *markdownView, themed *container.ThemeOverride) *markdownPage {
	p := &markdownPage{view: view, themed: themed}
	p.lastFG, p.lastBG, p.lastSize = view.themeState()
	p.refreshed = true
	p.ExtendBaseWidget(p)
	return p
}
func (p *markdownPage) CreateRenderer() fyne.WidgetRenderer {
	return &markdownPageHolderRenderer{page: p}
}
func (p *markdownPage) Refresh() {
	fg, bg, sz := p.view.themeState()
	if !p.refreshed || p.lastFG != fg || p.lastBG != bg || p.lastSize != sz {
		p.refreshed = true
		p.lastFG, p.lastBG, p.lastSize = fg, bg, sz
	}
	p.view.relayout()
	p.BaseWidget.Refresh()
}

type markdownPageHolderRenderer struct{ page *markdownPage }

func (*markdownPageHolderRenderer) Destroy()                {}
func (r *markdownPageHolderRenderer) Layout(size fyne.Size) { r.page.themed.Resize(size) }
func (r *markdownPageHolderRenderer) MinSize() fyne.Size    { return r.page.themed.MinSize() }
func (r *markdownPageHolderRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.page.themed}
}
func (*markdownPageHolderRenderer) Refresh() {}

// The page and its surrounding workspace follow theme changes independently.
type markdownPageSurface struct{ widget.BaseWidget }

func newMarkdownPageSurface() *markdownPageSurface {
	surface := &markdownPageSurface{}
	surface.ExtendBaseWidget(surface)
	return surface
}
func (s *markdownPageSurface) CreateRenderer() fyne.WidgetRenderer {
	r := &markdownPageRenderer{surface: s, workspace: canvas.NewRectangle(color.Transparent), paper: canvas.NewRectangle(color.Transparent)}
	r.paper.StrokeWidth = 1
	r.Refresh()
	return r
}

type markdownPageRenderer struct {
	surface          *markdownPageSurface
	workspace, paper *canvas.Rectangle
}

func (*markdownPageRenderer) Destroy()           {}
func (*markdownPageRenderer) MinSize() fyne.Size { return fyne.Size{} }
func (r *markdownPageRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.workspace, r.paper}
}
func (r *markdownPageRenderer) Layout(size fyne.Size) {
	r.workspace.Resize(size)
	width := max(1, min(markdownTextWidth+2*markdownPagePadding, size.Width-2*markdownPageMargin))
	r.paper.Move(fyne.NewPos((size.Width-width)/2, markdownPageMargin))
	r.paper.Resize(fyne.NewSize(width, max(0, size.Height-2*markdownPageMargin)))
}
func (r *markdownPageRenderer) Refresh() {
	r.workspace.FillColor = theme.ColorForWidget(theme.ColorNameInputBackground, r.surface)
	r.paper.FillColor = theme.ColorForWidget(theme.ColorNameBackground, r.surface)
	r.paper.StrokeColor = theme.ColorForWidget(theme.ColorNameInputBorder, r.surface)
	r.workspace.Refresh()
	r.paper.Refresh()
}

var (
	markdownLightPalette = resolvedChromaStyle("github")
	markdownDarkPalette  = resolvedChromaStyle("github-dark")
)

func resolvedChromaStyle(name string) *chroma.Style {
	if s := styles.Get(name); s != nil {
		return s
	}
	return styles.Fallback
}

// markdownTheme keeps code colours and text sizing in the normal Fyne theme path.
type markdownTheme struct {
	scale float32
}

func (t *markdownTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if suffix, ok := strings.CutPrefix(string(name), "markdown-token-"); ok {
		if token, err := strconv.Atoi(suffix); err == nil {
			palette := markdownLightPalette
			r, g, b, _ := fyne.CurrentApp().Settings().Theme().Color(theme.ColorNameBackground, variant).RGBA()
			if r+g+b < 3*32768 {
				palette = markdownDarkPalette
			}
			c := palette.Get(chroma.TokenType(token)).Colour
			if c.IsSet() {
				return color.NRGBA{R: c.Red(), G: c.Green(), B: c.Blue(), A: 255}
			}
		}
		name = theme.ColorNameForeground
	}
	return fyne.CurrentApp().Settings().Theme().Color(name, variant)
}
func (t *markdownTheme) Font(s fyne.TextStyle) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Font(s)
}
func (t *markdownTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Icon(n)
}
func (t *markdownTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case "markdown-h1":
		return 30 * t.scale
	case "markdown-h2":
		return 25 * t.scale
	case "markdown-h3":
		return 21 * t.scale
	case "markdown-h4":
		return 18 * t.scale
	case theme.SizeNameText:
		return fyne.CurrentApp().Settings().Theme().Size(n) * t.scale
	}
	return fyne.CurrentApp().Settings().Theme().Size(n)
}

func decoded(s []byte) string { return html.UnescapeString(string(util.UnescapePunctuations(s))) }
func (v *markdownView) inline(parent ast.Node, style widget.RichTextStyle) []widget.RichTextSegment {
	var result []widget.RichTextSegment
	add := func(s string, st widget.RichTextStyle) {
		result = append(result, &widget.TextSegment{Text: s, Style: st})
	}
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.Text:
			s := string(n.Value(v.doc.Source))
			if !n.IsRaw() {
				s = decoded(n.Value(v.doc.Source))
			}
			if n.HardLineBreak() {
				s += "\n"
			} else if n.SoftLineBreak() {
				s += " "
			}
			add(s, style)
		case *ast.String:
			add(string(n.Value), style)
		case *ast.Emphasis:
			st := style
			if n.Level == 2 {
				st.TextStyle.Bold = true
			} else {
				st.TextStyle.Italic = true
			}
			result = append(result, v.inline(n, st)...)
		case *ast.CodeSpan:
			st := style
			st.TextStyle.Monospace = true
			var b strings.Builder
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if text, ok := c.(*ast.Text); ok {
					b.Write(text.Value(v.doc.Source))
				}
			}
			add(strings.ReplaceAll(b.String(), "\n", " "), st)
		case *ast.Link:
			label := v.inlineText(n)
			dest := string(n.Destination)
			result = append(result, &widget.HyperlinkSegment{Text: label, OnTapped: func() { v.openLink(dest) }})
		case *ast.AutoLink:
			dest := string(n.URL(v.doc.Source))
			if n.AutoLinkType == ast.AutoLinkEmail {
				dest = "mailto:" + dest
			}
			result = append(result, &widget.HyperlinkSegment{Text: string(n.Label(v.doc.Source)), OnTapped: func() { v.openLink(dest) }})
		case *ast.Image:
			if img := v.doc.Images[string(n.Destination)]; img != nil {
				result = append(result, &markdownImageSegment{image: img})
			} else {
				add("[Image: "+v.inlineText(n)+"]", style)
			}
		case *east.TaskCheckBox:
			if n.IsChecked {
				add("☑ ", style)
			} else {
				add("☐ ", style)
			}
		case *east.Strikethrough:
			// Combining strokes preserve wrapping in Fyne's native text layout.
			var struck strings.Builder
			for _, r := range v.inlineText(n) {
				struck.WriteRune(r)
				struck.WriteRune(0x0336)
			}
			add(struck.String(), style)
		case *ast.RawHTML:
			var b strings.Builder
			for i := 0; i < n.Segments.Len(); i++ {
				s := n.Segments.At(i)
				b.Write(s.Value(v.doc.Source))
			}
			add(b.String(), widget.RichTextStyleCodeInline)
		default:
			result = append(result, v.inline(n, style)...)
		}
	}
	return result
}
func (v *markdownView) inlineText(n ast.Node) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			switch c := c.(type) {
			case *ast.Text:
				if c.IsRaw() {
					b.Write(c.Value(v.doc.Source))
				} else {
					b.WriteString(decoded(c.Value(v.doc.Source)))
				}
				if c.SoftLineBreak() || c.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(c.Value)
			case *ast.AutoLink:
				b.Write(c.Label(v.doc.Source))
			case *ast.RawHTML:
				for i := 0; i < c.Segments.Len(); i++ {
					segment := c.Segments.At(i)
					b.Write(segment.Value(v.doc.Source))
				}
			case *east.TaskCheckBox:
				if c.IsChecked {
					b.WriteString("☑ ")
				} else {
					b.WriteString("☐ ")
				}
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}
func (v *markdownView) styleText(text *widget.RichText) fyne.CanvasObject {
	// Fyne initialises segment parents during Refresh, after measuring them.
	// Prime those parents with empty text before applying the page theme scope.
	type primedSegment struct {
		text  string
		style widget.RichTextStyle
	}
	saved := make(map[*widget.TextSegment]primedSegment, len(text.Segments))
	for _, segment := range text.Segments {
		if s, ok := segment.(*widget.TextSegment); ok {
			saved[s] = primedSegment{text: s.Text, style: s.Style}
			s.Text = ""
			s.Style.ColorName = theme.ColorNameForeground
			s.Style.SizeName = theme.SizeNameText
		}
	}
	text.Refresh()
	text.Resize(fyne.NewSize(v.columnWidth(0), 0))
	for s, item := range saved {
		s.Text = item.text
		s.Style = item.style
	}
	return newMarkdownText(v, text)
}
func (v *markdownView) prose(n ast.Node, style widget.RichTextStyle) fyne.CanvasObject {
	text := widget.NewRichText(v.inline(n, style)...)
	text.Wrapping = fyne.TextWrapWord
	return v.copyable(v.styleText(text), v.inlineText(n))
}
func (v *markdownView) blocks(parent ast.Node) []fyne.CanvasObject {
	var blocks []fyne.CanvasObject
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		var object fyne.CanvasObject
		switch n := child.(type) {
		case *ast.Paragraph, *ast.TextBlock:
			object = v.prose(n, widget.RichTextStyleInline)
		case *ast.Heading:
			style := widget.RichTextStyleStrong
			style.SizeName = fyne.ThemeSizeName(fmt.Sprintf("markdown-h%d", min(n.Level, 4)))
			object = v.prose(n, style)
			if id, ok := n.AttributeString("id"); ok {
				v.anchors[string(id.([]byte))] = object
			}
		case *ast.FencedCodeBlock:
			object = v.code(n, string(n.Language(v.doc.Source)))
		case *ast.CodeBlock:
			object = v.code(n, "")
		case *ast.HTMLBlock:
			source := markdown.Code(v.doc.Source, n)
			if n.HasClosure() {
				source += string(n.ClosureLine.Value(v.doc.Source))
			}
			object = v.plainCode(source, "HTML source")
		case *ast.List:
			rows := []fyne.CanvasObject{}
			index := n.Start
			for item := n.FirstChild(); item != nil; item = item.NextSibling() {
				marker := "•"
				if n.IsOrdered() {
					marker = fmt.Sprintf("%d.", index)
					index++
				}
				label := widget.NewLabel(marker)
				if item.FirstChild() != nil && item.FirstChild().FirstChild() != nil {
					if _, ok := item.FirstChild().FirstChild().(*east.TaskCheckBox); ok {
						label.SetText("")
					}
				}
				rows = append(rows, container.NewBorder(nil, nil, label, nil, container.NewVBox(v.blocks(item)...)))
			}
			object = container.NewVBox(rows...)
		case *ast.Blockquote:
			bar := canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
			bar.SetMinSize(fyne.NewSize(3, 0))
			object = container.NewBorder(nil, nil, bar, nil, container.NewPadded(container.NewVBox(v.blocks(n)...)))
		case *ast.ThematicBreak:
			object = widget.NewSeparator()
		case *east.Table:
			object = v.table(n)
		default:
			object = container.NewVBox(v.blocks(n)...)
		}
		if object != nil {
			blocks = append(blocks, object)
		}
	}
	return blocks
}
func (v *markdownView) code(n ast.Node, language string) fyne.CanvasObject {
	source := markdown.Code(v.doc.Source, n)
	if strings.EqualFold(language, "mermaid") {
		return v.mermaidBlock(source)
	}
	tokens := v.doc.Tokens[n]
	if len(tokens) == 0 {
		return v.plainCode(source, language)
	}
	var segments []widget.RichTextSegment
	var lastSeg *widget.TextSegment
	var lastColors [2]chroma.Colour
	var lastHasColor bool
	for _, token := range tokens {
		if token.Value == "" {
			continue
		}
		pureSpace := strings.Trim(token.Value, " \t") == ""
		colors := [2]chroma.Colour{
			markdownLightPalette.Get(token.Type).Colour,
			markdownDarkPalette.Get(token.Type).Colour,
		}
		colorName := theme.ColorNameForeground
		if colors[0].IsSet() || colors[1].IsSet() {
			colorName = fyne.ThemeColorName("markdown-token-" + strconv.Itoa(int(token.Type)))
		}
		singleLine := !strings.Contains(strings.TrimSuffix(token.Value, "\n"), "\n")
		if lastSeg != nil && !strings.Contains(lastSeg.Text, "\n") && singleLine {
			if pureSpace {
				lastSeg.Text += token.Value
				continue
			}
			if !lastHasColor {
				lastSeg.Text += token.Value
				lastSeg.Style.ColorName = colorName
				lastColors = colors
				lastHasColor = true
				continue
			}
			if colors == lastColors {
				lastSeg.Text += token.Value
				continue
			}
		}
		st := widget.RichTextStyleCodeInline
		st.ColorName = colorName
		seg := &widget.TextSegment{Text: token.Value, Style: st}
		segments = append(segments, seg)
		lastSeg = seg
		lastColors = colors
		lastHasColor = !pureSpace
	}
	text := widget.NewRichText(segments...)
	text.Wrapping = fyne.TextWrapOff
	return v.codePanel(v.styleText(text), source, language)
}
func (v *markdownView) plainCode(source, language string) fyne.CanvasObject {
	text := widget.NewRichText(&widget.TextSegment{Text: source, Style: widget.RichTextStyleCodeInline})
	return v.codePanel(v.styleText(text), source, language)
}
func (v *markdownView) codePanel(text fyne.CanvasObject, source, language string) fyne.CanvasObject {
	copy := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() { v.editor.app.Clipboard().SetContent(source) })
	copy.Importance = widget.LowImportance
	header := container.NewBorder(nil, nil, widget.NewLabel(language), copy)
	// Horizontal overflow stays within the code block, never the whole page.
	code := container.NewHScroll(container.NewPadded(text))
	surface := newMarkdownCodeScrollSurface(v, code, text)
	return widget.NewCard("", "", container.NewBorder(header, nil, nil, nil, container.NewStack(code, surface)))
}
func (v *markdownView) table(n *east.Table) fyne.CanvasObject {
	rows := []fyne.CanvasObject{}
	for row := n.FirstChild(); row != nil; row = row.NextSibling() {
		cells := []fyne.CanvasObject{}
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			st := widget.RichTextStyleInline
			if _, ok := row.(*east.TableHeader); ok {
				st.TextStyle.Bold = true
			}
			c := cell.(*east.TableCell)
			switch c.Alignment {
			case east.AlignCenter:
				st.Alignment = fyne.TextAlignCenter
			case east.AlignRight:
				st.Alignment = fyne.TextAlignTrailing
			}
			cells = append(cells, container.NewPadded(v.prose(cell, st)))
		}
		rows = append(rows, container.NewGridWithColumns(len(n.Alignments), cells...), widget.NewSeparator())
	}
	return container.NewVBox(rows...)
}

// Secondary click copies a whole block without exposing editable content.
type markdownCopyBlock struct {
	widget.BaseWidget
	object fyne.CanvasObject
	text   string
	view   *markdownView
}

func (v *markdownView) copyable(o fyne.CanvasObject, text string) fyne.CanvasObject {
	b := &markdownCopyBlock{object: o, text: text, view: v}
	b.ExtendBaseWidget(b)
	return b
}
func (b *markdownCopyBlock) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.object)
}
func (b *markdownCopyBlock) TappedSecondary(e *fyne.PointEvent) {
	menu := fyne.NewMenu("", fyne.NewMenuItem("Copy text", func() { b.view.editor.app.Clipboard().SetContent(b.text) }))
	widget.ShowPopUpMenuAtPosition(menu, b.view.editor.window.Canvas(), e.AbsolutePosition)
}
func (v *markdownView) openLink(destination string) {
	u, err := url.Parse(destination)
	if err != nil {
		return
	}
	if u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto" {
		if err := v.editor.app.OpenURL(u); err != nil {
			v.editor.showOperationError("open_url", err)
		}
		return
	}
	if u.Scheme != "" || u.Host != "" {
		return
	}
	if u.Path == "" && u.Fragment != "" {
		v.pendingReadingOffset = nil
		if o := v.anchors[u.Fragment]; o != nil {
			position := v.editor.app.Driver().AbsolutePositionForObject(o)
			origin := v.editor.app.Driver().AbsolutePositionForObject(v.content)
			v.scroll.ScrollToOffset(fyne.NewPos(0, position.Y-origin.Y+markdownPageMargin+markdownPagePadding))
		}
		return
	}
	path, err := v.doc.LocalPath(destination)
	if err != nil {
		return
	}
	v.editor.openStartupFiles([]string{path})
}

func loadMarkdownReader(uri fyne.URIReadCloser) (*markdown.Document, error) {
	path := ""
	if uri.URI().Scheme() == "file" {
		path = uri.URI().Path()
	}
	d, err := markdown.Load(uri.URI().Name(), path, uri)
	return d, errors.Join(err, uri.Close())
}

type markdownImageSegment struct{ image image.Image }

func (*markdownImageSegment) Inline() bool                        { return false }
func (*markdownImageSegment) Textual() string                     { return "" }
func (*markdownImageSegment) Select(fyne.Position, fyne.Position) {}
func (*markdownImageSegment) SelectedText() string                { return "" }
func (*markdownImageSegment) Unselect()                           {}
func (s *markdownImageSegment) Visual() fyne.CanvasObject {
	o := canvas.NewImageFromImage(s.image)
	o.FillMode = canvas.ImageFillContain
	size := s.image.Bounds().Size()
	w := min(float32(300), float32(size.X), 400*float32(size.X)/float32(size.Y))
	o.SetMinSize(fyne.NewSize(w, w*float32(size.Y)/float32(size.X)))
	return o
}
func (s *markdownImageSegment) Update(o fyne.CanvasObject) {
	o.(*canvas.Image).Image = s.image
	o.Refresh()
}

func (g *Gui) handleMarkdownKey(key fyne.KeyName) bool {
	if g.markdownView.cropCapturing || g.markdownView.crop != nil && g.markdownView.crop.active {
		return false
	}
	if g.closed || g.working || g.window == nil || g.modifiers() != 0 || g.window.Canvas().Focused() != nil || g.window.Canvas().Overlays().Top() != nil {
		return false
	}
	scroll := g.markdownView.scroll
	g.markdownView.pendingReadingOffset = nil
	offset := scroll.Offset
	switch key {
	case fyne.KeyUp:
		offset.Y -= 40
	case fyne.KeyDown:
		offset.Y += 40
	case fyne.KeyPageUp:
		offset.Y -= max(40, scroll.Size().Height-40)
	case fyne.KeyPageDown:
		offset.Y += max(40, scroll.Size().Height-40)
	case fyne.KeyHome:
		offset.Y = 0
	case fyne.KeyEnd:
		offset.Y = scroll.Content.MinSize().Height
	default:
		return false
	}
	scroll.ScrollToOffset(offset)
	return true
}

// markdownText caches RichText measurements for a given wrap width and avoids
// redundant Harfbuzz shaping passes when layout or scrollbars refresh.
type markdownText struct {
	widget.BaseWidget
	view           *markdownView
	text           *widget.RichText
	cachedMin      fyne.Size
	minWidth       float32
	hasMin         bool
	lastFG, lastBG color.Color
	lastSize       float32
	refreshed      bool
}

func newMarkdownText(view *markdownView, text *widget.RichText) *markdownText {
	t := &markdownText{view: view, text: text}
	t.ExtendBaseWidget(t)
	return t
}
func (t *markdownText) themeState() (color.Color, color.Color, float32) {
	return theme.ColorForWidget(theme.ColorNameForeground, t.text),
		theme.ColorForWidget(theme.ColorNameBackground, t.text),
		theme.SizeForWidget(theme.SizeNameText, t.text)
}
func (t *markdownText) ensureRefreshed() {
	if t.view != nil && !t.view.themeReady {
		return
	}
	fg, bg, sz := t.themeState()
	if t.refreshed && t.lastFG == fg && t.lastBG == bg && t.lastSize == sz {
		return
	}
	t.refreshed = true
	t.lastFG, t.lastBG, t.lastSize = fg, bg, sz
	t.hasMin = false
	t.text.Refresh()
}
func (t *markdownText) CreateRenderer() fyne.WidgetRenderer {
	return &markdownTextRenderer{text: t}
}
func (t *markdownText) MinSize() fyne.Size {
	if t.view != nil && !t.view.themeReady {
		return fyne.Size{}
	}
	t.ensureRefreshed()
	width := t.text.Size().Width
	if t.hasMin && (t.text.Wrapping == fyne.TextWrapOff || t.minWidth == width) {
		return t.cachedMin
	}
	t.cachedMin = t.text.MinSize()
	t.minWidth = width
	t.hasMin = true
	return t.cachedMin
}
func (t *markdownText) Resize(size fyne.Size) {
	if size == t.Size() {
		return
	}
	if size.Width > 0 && t.text.Wrapping != fyne.TextWrapOff && size.Width != t.text.Size().Width {
		t.hasMin = false
	}
	t.BaseWidget.Resize(size)
}
func (t *markdownText) Refresh() {
	if t.view != nil && !t.view.themeReady {
		return
	}
	fg, bg, sz := t.themeState()
	if t.refreshed && t.lastFG == fg && t.lastBG == bg && t.lastSize == sz {
		return
	}
	t.ensureRefreshed()
	t.BaseWidget.Refresh()
}

type markdownTextRenderer struct{ text *markdownText }

func (*markdownTextRenderer) Destroy() {}
func (r *markdownTextRenderer) Layout(size fyne.Size) {
	if size.Width <= 0 || (r.text.view != nil && !r.text.view.themeReady) {
		return
	}
	r.text.ensureRefreshed()
	if r.text.text.Wrapping == fyne.TextWrapOff {
		if r.text.text.Size().IsZero() {
			r.text.text.Resize(size)
		}
		return
	}
	if r.text.text.Size().Width != size.Width {
		r.text.hasMin = false
		r.text.text.Resize(size)
	}
}
func (r *markdownTextRenderer) MinSize() fyne.Size { return r.text.text.MinSize() }
func (r *markdownTextRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.text.text}
}
func (*markdownTextRenderer) Refresh() {}

// A nested horizontal scroller consumes vertical wheel events in Fyne. Route
// each axis explicitly while leaving clicks and scrollbar dragging untouched.
type markdownCodeScrollSurface struct {
	widget.BaseWidget
	view *markdownView
	code *container.Scroll
	text fyne.CanvasObject
}

func newMarkdownCodeScrollSurface(view *markdownView, code *container.Scroll, text fyne.CanvasObject) *markdownCodeScrollSurface {
	surface := &markdownCodeScrollSurface{view: view, code: code, text: text}
	surface.ExtendBaseWidget(surface)
	return surface
}
func (s *markdownCodeScrollSurface) CreateRenderer() fyne.WidgetRenderer {
	return &panOverlayRenderer{background: canvas.NewRectangle(color.Transparent)}
}
func (s *markdownCodeScrollSurface) MinSize() fyne.Size {
	h := float32(16)
	if s.text != nil {
		h += s.text.MinSize().Height
	}
	if s.code != nil {
		s.code.SetMinSize(fyne.NewSize(100, h))
	}
	return fyne.NewSize(100, h)
}
func (s *markdownCodeScrollSurface) Scrolled(event *fyne.ScrollEvent) {
	if event.Scrolled.DX != 0 {
		horizontal := *event
		horizontal.Scrolled.DY = 0
		s.code.Scrolled(&horizontal)
	}
	if event.Scrolled.DY != 0 && s.view.scroll != nil {
		s.view.pendingReadingOffset = nil
		vertical := *event
		vertical.Scrolled.DX = 0
		s.view.scroll.Scrolled(&vertical)
	}
}
