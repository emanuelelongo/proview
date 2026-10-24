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
	doc                    *markdown.Document
	scroll                 *container.Scroll
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
	cropSnapshot           *canvas.Image
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
	v := &markdownView{doc: d, scale: 1, editor: g}
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
	v.content = container.NewVBox(v.blocks(v.doc.Root)...)
	// Keep a readable line length while allowing narrow windows to reflow.
	body := container.New(&markdownPageLayout{}, newMarkdownPageSurface(), v.content)
	themed := container.NewThemeOverride(body, &markdownTheme{scale: v.scale})
	if v.scroll == nil {
		v.scroll = container.NewVScroll(themed)
		v.scroll.OnScrolled = func(fyne.Position) {
			if !v.restoringReadingOffset {
				v.pendingReadingOffset = nil
			}
		}
	} else {
		v.scroll.Content = themed
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

type markdownPageLayout struct{}

func (*markdownPageLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	h := 2 * (markdownPageMargin + markdownPagePadding)
	if len(objects) > 1 {
		h += objects[1].MinSize().Height
	}
	return fyne.NewSize(180, h)
}
func (*markdownPageLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 2 {
		return
	}
	objects[0].Move(fyne.Position{})
	objects[0].Resize(size)
	width := max(1, min(markdownTextWidth, size.Width-2*(markdownPageMargin+markdownPagePadding)))
	content := objects[1]
	content.Resize(fyne.NewSize(width, content.MinSize().Height))
	content.Move(fyne.NewPos((size.Width-width)/2, markdownPageMargin+markdownPagePadding))
	content.Resize(fyne.NewSize(width, content.MinSize().Height))
}

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

// markdownTheme keeps code colours and text sizing in the normal Fyne theme path.
type markdownTheme struct {
	scale float32
}

func (t *markdownTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if strings.HasPrefix(string(name), "markdown-token-") {
		var token int
		_, _ = fmt.Sscanf(string(name), "markdown-token-%d", &token)
		palette := styles.Get("github")
		r, g, b, _ := fyne.CurrentApp().Settings().Theme().Color(theme.ColorNameBackground, variant).RGBA()
		if r+g+b < 3*32768 {
			palette = styles.Get("github-dark")
		}
		if palette == nil {
			palette = styles.Fallback
		}
		c := palette.Get(chroma.TokenType(token)).Colour
		if c.IsSet() {
			return color.NRGBA{R: c.Red(), G: c.Green(), B: c.Blue(), A: 255}
		}
	}
	if strings.HasPrefix(string(name), "markdown-token-") {
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
	// Prime those parents with standard style names before using local theme keys.
	saved := make(map[*widget.TextSegment]widget.RichTextStyle)
	for _, segment := range text.Segments {
		if s, ok := segment.(*widget.TextSegment); ok {
			saved[s] = s.Style
			s.Style.ColorName = theme.ColorNameForeground
			s.Style.SizeName = theme.SizeNameText
		}
	}
	themed := container.NewThemeOverride(text, &markdownTheme{scale: v.scale})
	for s, style := range saved {
		s.Style = style
	}
	text.Refresh()
	return themed
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
	segments := []widget.RichTextSegment{}
	for _, token := range tokens {
		st := widget.RichTextStyleCodeInline
		st.ColorName = fyne.ThemeColorName(fmt.Sprintf("markdown-token-%d", token.Type))
		segments = append(segments, &widget.TextSegment{Text: token.Value, Style: st})
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
	code.SetMinSize(fyne.NewSize(100, text.MinSize().Height+16))
	surface := newMarkdownCodeScrollSurface(v, code)
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
	if g.markdownView.crop != nil && g.markdownView.crop.active {
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

// A nested horizontal scroller consumes vertical wheel events in Fyne. Route
// each axis explicitly while leaving clicks and scrollbar dragging untouched.
type markdownCodeScrollSurface struct {
	widget.BaseWidget
	view *markdownView
	code *container.Scroll
}

func newMarkdownCodeScrollSurface(view *markdownView, code *container.Scroll) *markdownCodeScrollSurface {
	surface := &markdownCodeScrollSurface{view: view, code: code}
	surface.ExtendBaseWidget(surface)
	return surface
}
func (s *markdownCodeScrollSurface) CreateRenderer() fyne.WidgetRenderer {
	return &panOverlayRenderer{background: canvas.NewRectangle(color.Transparent)}
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
