package markdown

import (
	"bytes"
	"io"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"golang.org/x/net/html"
)

// Details retains disclosure state separately from its immutable Markdown body.
type Details struct {
	ast.BaseBlock
	Summary        string
	Open           bool
	summaryPending bool
	summaryHTML    string
}

var kindDetails = ast.NewNodeKind("Details")

func (*Details) Kind() ast.NodeKind { return kindDetails }
func (n *Details) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Summary": n.Summary}, nil)
}

type detailsParser struct{}

func (*detailsParser) Trigger() []byte                             { return []byte{'<'} }
func (*detailsParser) CanInterruptParagraph() bool                 { return true }
func (*detailsParser) CanAcceptIndentedLine() bool                 { return false }
func (*detailsParser) Close(ast.Node, text.Reader, parser.Context) {}
func (*detailsParser) Open(_ ast.Node, reader text.Reader, _ parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	value := strings.TrimSpace(string(line))
	tokenizer := html.NewTokenizer(strings.NewReader(value))
	if tokenizer.Next() != html.StartTagToken {
		return nil, parser.NoChildren
	}
	token := tokenizer.Token()
	if token.Data != "details" {
		return nil, parser.NoChildren
	}
	// Leave compact inline HTML to the static HTML converter.
	if strings.Contains(strings.ToLower(value), "</details>") {
		return nil, parser.NoChildren
	}
	node := &Details{Summary: "Details", summaryPending: true}
	for _, attr := range token.Attr {
		if attr.Key == "open" {
			node.Open = true
		}
	}
	remainder := strings.TrimSpace(value[len(tokenizer.Raw()):])
	if remainder != "" {
		if !strings.HasPrefix(strings.ToLower(remainder), "<summary") {
			return nil, parser.NoChildren
		}
		node.summaryHTML = remainder
		node.finishSummary()
	}
	reader.Advance(len(bytes.TrimRight(line, "\n")))
	return node, parser.HasChildren
}
func (n *Details) finishSummary() {
	if !strings.Contains(strings.ToLower(n.summaryHTML), "</summary>") {
		return
	}
	tokenizer := html.NewTokenizer(strings.NewReader(n.summaryHTML))
	var title strings.Builder
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			if tokenizer.Err() == io.EOF {
				break
			}
			break
		}
		if kind == html.TextToken {
			title.Write(tokenizer.Text())
		}
	}
	if label := strings.Join(strings.Fields(title.String()), " "); label != "" {
		n.Summary = label
	}
	n.summaryPending = false
}
func (*detailsParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	n := node.(*Details)
	line, _ := reader.PeekLine()
	value := strings.TrimSpace(string(line))
	// Closing tags inside fenced code or nested disclosures belong to that child.
	for _, block := range pc.OpenedBlocks() {
		if block.Node == node || !descendantOf(block.Node, node) {
			continue
		}
		switch block.Node.(type) {
		case *Details, *ast.FencedCodeBlock, *ast.CodeBlock:
			return parser.Continue | parser.HasChildren
		}
	}
	if strings.EqualFold(value, "</details>") {
		reader.Advance(len(bytes.TrimRight(line, "\n")))
		return parser.Close
	}
	if n.summaryPending {
		if n.summaryHTML != "" || strings.HasPrefix(strings.ToLower(value), "<summary") {
			n.summaryHTML += "\n" + value
			n.finishSummary()
			reader.Advance(len(bytes.TrimRight(line, "\n")))
			return parser.Continue | parser.HasChildren
		}
		if value != "" {
			n.summaryPending = false
		}
	}
	return parser.Continue | parser.HasChildren
}
