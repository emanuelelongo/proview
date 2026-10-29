package markdown

import (
	"github.com/yuin/goldmark/ast"
	"strings"
	"testing"
)

func TestDetailsRetainMarkdownAndNesting(t *testing.T) {
	source := "<details open>\n<summary>More <strong>information</strong> &amp; examples</summary>\n\nA **bold** paragraph.\n\n<details>\n<summary>Nested</summary>\n\n- Child\n\n```html\n</details>\n```\n\n</details>\n\nAfter nested.\n\n</details>\n\n# Afterwards\n"
	doc, err := Load("details.md", "", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	outer, ok := doc.Root.FirstChild().(*Details)
	if !ok || !outer.Open || outer.Summary != "More information & examples" {
		t.Fatalf("wrong disclosure: %#v", doc.Root.FirstChild())
	}
	counts := map[string]int{}
	ast.Walk(outer, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			counts[n.Kind().String()]++
		}
		return ast.WalkContinue, nil
	})
	if counts["Details"] != 2 || counts["FencedCodeBlock"] != 1 || counts["Emphasis"] != 1 || counts["List"] != 1 {
		t.Fatalf("lost content: %v", counts)
	}
	if _, ok := outer.NextSibling().(*ast.Heading); !ok {
		t.Fatal("disclosure swallowed following content")
	}
	if string(doc.Source) != source {
		t.Fatal("source changed")
	}
}
func TestDetailsCompactAndDefaultSummary(t *testing.T) {
	for _, source := range []string{"<details open><summary>Compact</summary><p>Body <b>bold</b></p></details>\n", "<details>\n\nBody\n\n</details>\n"} {
		doc, err := Load("details.md", "", strings.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		n, ok := doc.Root.FirstChild().(*Details)
		if !ok || !n.HasChildren() || n.Summary == "" {
			t.Fatalf("missing details in %q", source)
		}
	}
}
