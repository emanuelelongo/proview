package markdown

import (
	"strings"
	"testing"

	"github.com/yuin/goldmark/ast"
)

func TestFrontMatterHiddenWithoutChangingSourceOrCodeOffsets(t *testing.T) {
	source := "---\nid: internal-id\ntitle: Metadata title\nsidebar_label: Label\nslug: /long/path\n---\n\n# Visible title\n\n```go\nvar x = 1\n```\n"
	doc, err := Load("metadata.md", "", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	if string(doc.Source) != source {
		t.Fatal("source changed")
	}
	heading, ok := doc.Root.FirstChild().(*ast.Heading)
	if !ok || string(heading.Text(doc.Source)) != "Visible title" {
		t.Fatal("metadata became visible content")
	}
	code, ok := heading.NextSibling().(*ast.FencedCodeBlock)
	if !ok || Code(doc.Source, code) != "var x = 1\n" || len(doc.Tokens[code]) == 0 {
		t.Fatal("source offsets or highlighting lost")
	}
	for _, source := range []string{"---\nunclosed metadata\n", "Before\n\n---\nbody\n---\n"} {
		d, err := Load("ordinary.md", "", strings.NewReader(source))
		if err != nil || d.Root.FirstChild() == nil {
			t.Fatal("ordinary content removed")
		}
	}
}

func TestAdmonitionsRetainMarkdownAndFollowingContent(t *testing.T) {
	source := ":::warning\nThe migration needs **downtime** and [a guide](guide.md).\n\n- First\n- Second\n\n```text\n:::\n```\n\n:::note Nested title\nNested content\n:::\n:::\n\n# Afterwards\n"
	doc, err := Load("admonition.md", "", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	callout, ok := doc.Root.FirstChild().(*Admonition)
	if !ok || callout.Label != "Warning" {
		doc.Root.Dump(doc.Source, 0)
		t.Fatal("warning container missing")
	}
	counts := map[string]int{}
	_ = ast.Walk(callout, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			counts[node.Kind().String()]++
		}
		return ast.WalkContinue, nil
	})
	if counts["Admonition"] != 2 || counts["List"] != 1 || counts["FencedCodeBlock"] != 1 || counts["Link"] != 1 || counts["Emphasis"] != 1 {
		t.Fatalf("lost Markdown children: %v", counts)
	}
	if _, ok := callout.NextSibling().(*ast.Heading); !ok {
		doc.Root.Dump(doc.Source, 0)
		t.Fatal("following heading swallowed by callout")
	}
}
