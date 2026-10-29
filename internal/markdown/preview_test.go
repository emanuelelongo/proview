package markdown

import (
	"context"
	"strings"
	"testing"

	"github.com/gen2brain/go-fitz"
)

func TestPreviewPDFKeepsReadableOpeningAndStopsAtFirstCard(t *testing.T) {
	for _, body := range []string{
		strings.Repeat("An opening paragraph with ordinary text.\n\n", 200),
		"```go\n" + strings.Repeat("fmt.Println(123)\n", 200) + "```\n\n",
		"| Name | Value |\n| --- | --- |\n" + strings.Repeat("| Item | Description |\n", 200),
	} {
		doc, err := Load("long.md", "", strings.NewReader("# Opening heading\n\n"+body+"\n# Unseen tail\n\n```mermaid\ninvalid diagram\n```"))
		if err != nil {
			t.Fatal(err)
		}
		preview, err := doc.PreviewPDF(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if preview.PageCount != 1 || preview.Width != 320 || preview.Height != 240 {
			t.Fatalf("preview geometry = %d pages, %g x %g", preview.PageCount, preview.Width, preview.Height)
		}
		renderer, err := fitz.NewFromMemory(preview.Data)
		if err != nil {
			t.Fatal(err)
		}
		text, err := renderer.Text(0)
		renderer.Close()
		if err != nil || !strings.Contains(text, "Opening heading") || strings.Contains(text, "Unseen tail") {
			t.Fatalf("preview must retain the opening and omit the tail: %q, %v", text, err)
		}
	}
}

func TestPreviewPDFHonorsCancellation(t *testing.T) {
	doc, err := Load("empty.md", "", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := doc.PreviewPDF(ctx); err != context.Canceled {
		t.Fatalf("canceled preview: %v", err)
	}
}

func TestPreviewPDFHidesFrontMatterAndRendersAdmonition(t *testing.T) {
	doc, err := Load("metadata.md", "", strings.NewReader("---\nid: hidden-metadata\n---\n\n# Visible title\n\n:::warning\nKeep **all content**.\n:::\n"))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := doc.PreviewPDF(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := fitz.NewFromMemory(preview.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer renderer.Close()
	text, err := renderer.Text(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Visible title", "Warning", "all content"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing printed content %q: %q", want, text)
		}
	}
	if strings.Contains(text, "hidden-metadata") || strings.Contains(text, ":::") {
		t.Fatalf("source delimiters leaked into preview: %q", text)
	}
}

func TestPreviewPDFIncludesClosedDetailsContent(t *testing.T) {
	doc, err := Load("details.md", "", strings.NewReader("<details>\n<summary>More information</summary>\n\nHidden **content** for printing.\n\n</details>\n"))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := doc.PreviewPDF(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := fitz.NewFromMemory(preview.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer renderer.Close()
	text, err := renderer.Text(0)
	if err != nil || !strings.Contains(text, "More information") || !strings.Contains(text, "printing") {
		t.Fatalf("missing disclosure output: %q, %v", text, err)
	}
}

func TestGitHubAlertsPrintAndPreview(t *testing.T) {
	source := "> [!IMPORTANT]\n> Keep **all content** and [instructions](guide.md).\n>\n> - Save configuration\n\nAfterwards\n"
	doc, err := Load("github.md", "", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, preview := range []bool{false, true} {
		output, err := doc.layoutPDF(context.Background(), preview)
		if err != nil {
			t.Fatal(err)
		}
		renderer, err := fitz.NewFromMemory(output.Data)
		if err != nil {
			t.Fatal(err)
		}
		text, err := renderer.Text(0)
		renderer.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Important", "all content", "instructions", "Save configuration", "Afterwards"} {
			if !strings.Contains(text, want) {
				t.Fatalf("preview=%v: missing %q in %q", preview, want, text)
			}
		}
		if strings.Contains(text, "[!IMPORTANT]") {
			t.Fatalf("source marker leaked into PDF: %q", text)
		}
	}
}
