package markdown

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuin/goldmark/ast"
)

func TestHTMLImagesAndParagraphsShareMarkdownTree(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(root, "assets", "logo.png"))
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 16, 8))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	file.Close()
	source := "<p align=\"center\">\n <img alt=\"Local logo\" src=\"/assets/logo.png?raw=true\" width=\"480\">\n</p>\n\n<p align=\"center\">Welcome to <strong>the project</strong>.</p>\n\nInline <img src=\"assets/logo.png\" alt=\"Inline logo\"> image.\n"
	doc, err := Load("README.md", filepath.Join(root, "README.md"), strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	if string(doc.Source) != source || len(doc.Images) != 2 || doc.Images["assets/logo.png?raw=true"] == nil {
		t.Fatal("HTML image not loaded or source changed")
	}
	images := 0
	_ = ast.Walk(doc.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			switch node := n.(type) {
			case *ast.HTMLBlock, *ast.RawHTML:
				t.Fatal("supported HTML left as source")
			case *ast.Image:
				images++
				if images == 1 && ImageWidth(node) != 480 {
					t.Fatal("HTML width lost")
				}
			}
		}
		return ast.WalkContinue, nil
	})
	if images != 2 {
		t.Fatalf("images = %d", images)
	}
	if align, _ := doc.Root.FirstChild().AttributeString("align"); align != "center" {
		t.Fatal("paragraph alignment lost")
	}
}

func TestHTMLUnsupportedMarkupRemainsLiteralAndImagesStayLocal(t *testing.T) {
	for _, source := range []string{"<script>alert('no')</script>\n", "<details>Hidden content</details>\n", "<p><iframe src=\"remote\"></iframe>Keep this text</p>\n"} {
		doc, err := Load("read.md", "", strings.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := doc.Root.FirstChild().(*ast.HTMLBlock); !ok {
			t.Fatal("unsupported HTML was partially interpreted")
		}
	}
	source := "<p><img src=\"https://example.com/logo.png\" alt=\"Remote\"><img src=\"/../secret.png\" alt=\"Outside\"><img src=\"//example.com/logo.png\"></p>\n"
	doc, err := Load("read.md", filepath.Join(t.TempDir(), "read.md"), strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Images) != 0 {
		t.Fatal("external HTML image loaded")
	}
}
