package markdown

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Admonition is a Docusaurus container or GitHub alert with ordinary Markdown children.
type Admonition struct {
	ast.BaseBlock
	Label    string
	Category string
}

var kindAdmonition = ast.NewNodeKind("Admonition")

func (*Admonition) Kind() ast.NodeKind { return kindAdmonition }
func (n *Admonition) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Label": n.Label}, nil)
}

type admonitionParser struct{}

func (*admonitionParser) Trigger() []byte                             { return []byte{':'} }
func (*admonitionParser) CanInterruptParagraph() bool                 { return true }
func (*admonitionParser) CanAcceptIndentedLine() bool                 { return false }
func (*admonitionParser) Close(ast.Node, text.Reader, parser.Context) {}
func (*admonitionParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	value := strings.TrimSpace(string(line))
	if !strings.HasPrefix(value, ":::") {
		return nil, parser.NoChildren
	}
	info := strings.TrimSpace(value[3:])
	kind, title, _ := strings.Cut(info, " ")
	switch kind {
	case "note", "tip", "info", "warning", "danger", "caution":
	default:
		return nil, parser.NoChildren
	}
	label := strings.TrimSpace(title)
	if label == "" {
		label = strings.ToUpper(kind[:1]) + kind[1:]
	}
	reader.Advance(len(bytes.TrimRight(line, "\n")))
	return &Admonition{Label: label, Category: kind}, parser.HasChildren
}
func (*admonitionParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	line, _ := reader.PeekLine()
	if strings.TrimSpace(string(line)) == ":::" {
		// A closing marker belongs to the innermost container and remains literal
		// inside fenced code, even when its language is unspecified.
		for _, block := range pc.OpenedBlocks() {
			if block.Node == node {
				continue
			}
			if _, ok := block.Node.(*Admonition); ok && descendantOf(block.Node, node) {
				return parser.Continue | parser.HasChildren
			}
			switch block.Node.(type) {
			case *ast.FencedCodeBlock, *ast.CodeBlock:
				return parser.Continue | parser.HasChildren
			}
		}
		reader.Advance(len(bytes.TrimRight(line, "\n")))
		return parser.Close
	}
	return parser.Continue | parser.HasChildren
}
func descendantOf(child, parent ast.Node) bool {
	for n := child.Parent(); n != nil; n = n.Parent() {
		if n == parent {
			return true
		}
	}
	return false
}

// GitHub alerts retain Goldmark's blockquote continuation and source offsets.
// Only a complete marker on the opening line replaces an ordinary quote.
type githubAlertParser struct {
	parser.BlockParser
}

func (p *githubAlertParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	value := strings.TrimSpace(string(line))
	if !strings.HasPrefix(value, ">") {
		return nil, parser.NoChildren
	}
	marker := strings.TrimSpace(value[1:])
	var category, label string
	switch marker {
	case "[!NOTE]":
		category, label = "note", "Note"
	case "[!TIP]":
		category, label = "tip", "Tip"
	case "[!IMPORTANT]":
		category, label = "important", "Important"
	case "[!WARNING]":
		category, label = "warning", "Warning"
	case "[!CAUTION]":
		category, label = "caution", "Caution"
	default:
		return nil, parser.NoChildren
	}
	if node, _ := p.BlockParser.Open(parent, reader, pc); node == nil {
		return nil, parser.NoChildren
	}
	remaining, _ := reader.PeekLine()
	reader.Advance(len(bytes.TrimRight(remaining, "\n")))
	return &Admonition{Label: label, Category: category}, parser.HasChildren
}

// Front matter stays in Source for copying, but is excluded from the rendered
// tree. Preserve offsets by starting the reader after a complete metadata block.
func frontMatterEnd(source []byte) int {
	if !bytes.HasPrefix(source, []byte("---\n")) {
		return 0
	}
	offset := 4
	for offset < len(source) {
		end := bytes.IndexByte(source[offset:], '\n')
		if end < 0 {
			end = len(source) - offset
		} else {
			end++
		}
		line := strings.TrimSpace(string(source[offset : offset+end]))
		offset += end
		if line == "---" || line == "..." {
			return offset
		}
	}
	return 0
}
