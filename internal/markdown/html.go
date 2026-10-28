package markdown

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Convert only a small, static HTML subset into the shared Markdown tree.
// Unsupported blocks remain literal source; no HTML renderer or network is used.
func expandHTML(root ast.Node, source []byte) {
	var candidates []ast.Node
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			// Inline anchors are split into separate RawHTML nodes by Goldmark.
			// Convert a complete static HTML paragraph together, preserving pairs.
			if paragraph, ok := node.(*ast.Paragraph); ok && htmlLinkParagraph(paragraph, source) {
				candidates = append(candidates, paragraph)
				return ast.WalkSkipChildren, nil
			}
			switch node.(type) {
			case *ast.HTMLBlock, *ast.RawHTML:
				candidates = append(candidates, node)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, node := range candidates {
		var raw string
		switch n := node.(type) {
		case *ast.Paragraph:
			raw = Code(source, n)
		case *ast.HTMLBlock:
			raw = Code(source, n)
			if n.HasClosure() {
				raw += string(n.ClosureLine.Value(source))
			}
		case *ast.RawHTML:
			var b strings.Builder
			for i := 0; i < n.Segments.Len(); i++ {
				segment := n.Segments.At(i)
				b.Write(segment.Value(source))
			}
			raw = b.String()
		}
		parsed, err := html.ParseFragment(strings.NewReader(raw), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
		if err != nil {
			continue
		}
		var replacement []ast.Node
		ok := true
		if _, inline := node.(*ast.RawHTML); inline {
			// Standalone inline images need no paired-tag state across Markdown nodes.
			if len(parsed) != 1 || parsed[0].Type != html.ElementNode || parsed[0].Data != "img" {
				continue
			}
			replacement, ok = htmlInline(parsed[0], 0)
		} else {
			replacement, ok = htmlBlocks(parsed, "", 0)
		}
		if !ok {
			continue
		}
		parent := node.Parent()
		for _, item := range replacement {
			parent.InsertBefore(parent, node, item)
		}
		parent.RemoveChild(parent, node)
	}
}

func htmlLinkParagraph(paragraph *ast.Paragraph, source []byte) bool {
	linked := false
	for child := paragraph.FirstChild(); child != nil; child = child.NextSibling() {
		switch node := child.(type) {
		case *ast.RawHTML:
			var raw strings.Builder
			for i := 0; i < node.Segments.Len(); i++ {
				segment := node.Segments.At(i)
				raw.Write(segment.Value(source))
			}
			tokenizer := html.NewTokenizer(strings.NewReader(raw.String()))
			if tokenizer.Next() == html.StartTagToken && tokenizer.Token().Data == "a" {
				linked = true
			}
		case *ast.Text:
		default:
			return false
		}
	}
	return linked
}

func htmlAttribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func htmlBlocks(nodes []*html.Node, align string, depth int) ([]ast.Node, bool) {
	if depth > 32 {
		return nil, false
	}
	var blocks []ast.Node
	paragraph := ast.NewParagraph()
	flush := func() {
		if paragraph.HasChildren() {
			paragraph.SetAttributeString("align", align)
			blocks = append(blocks, paragraph)
			paragraph = ast.NewParagraph()
		}
	}
	for _, node := range nodes {
		if node.Type == html.TextNode && strings.TrimSpace(node.Data) == "" && (!paragraph.HasChildren() || node.NextSibling == nil) {
			continue
		}
		if node.Type == html.ElementNode && (node.Data == "p" || node.Data == "div") {
			flush()
			alignment := htmlAttribute(node, "align")
			if alignment == "" {
				alignment = align
			}
			var children []*html.Node
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				children = append(children, child)
			}
			converted, ok := htmlBlocks(children, alignment, depth+1)
			if !ok {
				return nil, false
			}
			blocks = append(blocks, converted...)
			continue
		}
		converted, ok := htmlInline(node, depth+1)
		if !ok {
			return nil, false
		}
		for _, child := range converted {
			paragraph.AppendChild(paragraph, child)
		}
	}
	flush()
	return blocks, true
}

func htmlInline(node *html.Node, depth int) ([]ast.Node, bool) {
	if depth > 32 {
		return nil, false
	}
	if node.Type == html.TextNode {
		// HTML collapses layout whitespace. Keep explicit br elements as newlines.
		value := string(bytes.Join(bytes.Fields([]byte(node.Data)), []byte(" ")))
		if value == "" {
			return []ast.Node{ast.NewString([]byte(" "))}, true
		}
		if len(node.Data) > 0 && strings.ContainsAny(node.Data[:1], " \t\r\n") {
			value = " " + value
		}
		if len(node.Data) > 0 && strings.ContainsAny(node.Data[len(node.Data)-1:], " \t\r\n") {
			value += " "
		}
		return []ast.Node{ast.NewString([]byte(value))}, true
	}
	if node.Type != html.ElementNode {
		return nil, false
	}
	switch node.Data {
	case "a":
		link := ast.NewLink()
		link.Destination = []byte(htmlAttribute(node, "href"))
		link.Title = []byte(htmlAttribute(node, "title"))
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			converted, ok := htmlInline(child, depth+1)
			if !ok {
				return nil, false
			}
			for _, item := range converted {
				link.AppendChild(link, item)
			}
		}
		return []ast.Node{link}, true
	case "img":
		link := ast.NewLink()
		src := htmlAttribute(node, "src")
		// Site-root paths in README HTML refer to the document's local asset tree,
		// never to the machine's filesystem root. Protocol-relative URLs stay remote.
		if strings.HasPrefix(src, "/") && !strings.HasPrefix(src, "//") {
			src = strings.TrimPrefix(src, "/")
		}
		link.Destination = []byte(src)
		image := ast.NewImage(link)
		if width, err := strconv.ParseFloat(htmlAttribute(node, "width"), 32); err == nil && width > 0 && width <= 4096 {
			image.SetAttributeString("width", float32(width))
		}
		image.AppendChild(image, ast.NewString([]byte(htmlAttribute(node, "alt"))))
		return []ast.Node{image}, true
	case "br":
		return []ast.Node{ast.NewString([]byte("\n"))}, true
	case "strong", "b", "em", "i":
		level := 1
		if node.Data == "strong" || node.Data == "b" {
			level = 2
		}
		emphasis := ast.NewEmphasis(level)
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			converted, ok := htmlInline(child, depth+1)
			if !ok {
				return nil, false
			}
			for _, item := range converted {
				emphasis.AppendChild(emphasis, item)
			}
		}
		return []ast.Node{emphasis}, true
	}
	return nil, false
}

// ImageWidth is the optional HTML width in CSS pixels; zero uses natural sizing.
func ImageWidth(image *ast.Image) float32 {
	if width, ok := image.AttributeString("width"); ok {
		if value, ok := width.(float32); ok {
			return value
		}
	}
	return 0
}
