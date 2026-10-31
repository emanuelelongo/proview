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
	if !ok || Code(doc.Source, code) != "var x = 1\n" || len(doc.TokensFor(code)) == 0 {
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

func TestGitHubAlertsRetainMarkdownAndSourceOffsets(t *testing.T) {
	for _, kind := range []string{"NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION"} {
		t.Run(kind, func(t *testing.T) {
			source := "> [!" + kind + "]\n> Keep **styled text** and [a link](guide.md).\n>\n> - First\n> - Second\n>\n> ```go\n> var x = 1\n> ```\n\n# Afterwards\n"
			doc, err := Load("github.md", "", strings.NewReader(source))
			if err != nil {
				t.Fatal(err)
			}
			callout, ok := doc.Root.FirstChild().(*Admonition)
			if !ok || callout.Category != strings.ToLower(kind) || strings.ToUpper(callout.Label) != kind {
				t.Fatalf("missing %s alert: %T", kind, doc.Root.FirstChild())
			}
			counts := map[string]int{}
			_ = ast.Walk(callout, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
				if entering {
					counts[node.Kind().String()]++
					if code, ok := node.(*ast.FencedCodeBlock); ok {
						if Code(doc.Source, code) != "var x = 1\n" || len(doc.TokensFor(code)) == 0 {
							t.Fatal("code offsets or highlighting lost")
						}
					}
				}
				return ast.WalkContinue, nil
			})
			if counts["List"] != 1 || counts["Link"] != 1 || counts["Emphasis"] != 1 || counts["FencedCodeBlock"] != 1 {
				t.Fatalf("lost Markdown children: %v", counts)
			}
			if string(doc.Source) != source || strings.Contains(string(callout.Text(doc.Source)), "[!") {
				t.Fatal("source changed or marker leaked into alert body")
			}
			if _, ok := callout.NextSibling().(*ast.Heading); !ok {
				t.Fatal("following heading swallowed by alert")
			}
		})
	}
}

func TestGitHubAlertsLeaveOrdinaryQuotesAndCodeUnchanged(t *testing.T) {
	for _, source := range []string{
		"> Ordinary quote\n> [!TIP]\n",
		"> [!UNKNOWN]\n> Content\n",
		"> [!TIP] Custom title\n> Content\n",
		"> [!tip]\n> Content\n",
		"> \\[!TIP]\n> Content\n",
		"```markdown\n> [!TIP]\n> Content\n```\n",
		"    > [!TIP]\n    > Content\n",
	} {
		doc, err := Load("literal.md", "", strings.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		_ = ast.Walk(doc.Root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			if _, ok := node.(*Admonition); entering && ok {
				t.Fatalf("ordinary content became an alert: %q", source)
			}
			return ast.WalkContinue, nil
		})
	}
}

func TestGitHubAlertsKeepQuoteBoundaries(t *testing.T) {
	for _, source := range []string{
		"> [!TIP]\n> Body\n\nAfterwards\n",
		"> [!TIP]\n> Body\n>\n> Second paragraph\n\nAfterwards\n",
		"> [!TIP]\n> Body\ncontinued prose\n\nAfterwards\n",
		"> [!TIP]\n\nAfterwards\n",
		"   >[!TIP]  \n   > Body\n\nAfterwards\n",
	} {
		doc, err := Load("boundaries.md", "", strings.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		callout, ok := doc.Root.FirstChild().(*Admonition)
		if !ok || callout.NextSibling() == nil || string(callout.NextSibling().Text(doc.Source)) != "Afterwards" {
			t.Fatalf("lost quote boundary: %q", source)
		}
	}
}
