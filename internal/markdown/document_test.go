package markdown

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

func TestLoadParsesGFMAndHighlightsWithoutLosingCode(t *testing.T) {
	source, err := os.ReadFile("testdata/reading.md")
	if err != nil {
		t.Fatal(err)
	}
	source = bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("newline=%q", newline), func(t *testing.T) {
			input := bytes.ReplaceAll(source, []byte("\n"), []byte(newline))
			d, err := Load("reading.md", "", bytes.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(d.Source, source) {
				t.Fatal("source changed")
			}
			counts := map[string]int{}
			_ = ast.Walk(d.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
				if entering {
					switch n := n.(type) {
					case *east.Table:
						counts["table"]++
					case *east.TaskCheckBox:
						counts["task"]++
					case *ast.FencedCodeBlock:
						if tokens := d.Tokens[n]; len(tokens) > 0 {
							counts["highlight"]++
							var b strings.Builder
							for _, token := range tokens {
								b.WriteString(token.Value)
							}
							if b.String() != Code(d.Source, n) {
								t.Fatal("highlighting changed code")
							}
						}
						if string(n.Language(d.Source)) == "mermaid" && len(d.Tokens[n]) != 0 {
							t.Fatal("unexpected Mermaid interpretation")
						}
					}
				}
				return ast.WalkContinue, nil
			})
			if counts["table"] != 1 || counts["task"] != 3 || counts["highlight"] != 2 {
				t.Fatalf("missing content: %v", counts)
			}
		})
	}
}
func TestLoadRejectsNonTextAndOversize(t *testing.T) {
	for _, source := range []string{"\xff", "a\x00b", strings.Repeat("x", MaxSourceBytes+1)} {
		if _, err := Load("bad.md", "", strings.NewReader(source)); err == nil {
			t.Fatal("accepted invalid input")
		}
	}
	d, err := Load("bom.md", "", strings.NewReader("\xef\xbb\xbf# Title\r\n"))
	if err != nil || string(d.Source) != "# Title\n" {
		t.Fatal("BOM/CRLF handling", err)
	}
}
func TestLocalImagesStayInsideDocumentDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "local image.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 8, 4))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	outside := filepath.Join(root, "outside.png")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.png")); err != nil {
		t.Fatal(err)
	}
	d, err := Load("read.md", filepath.Join(dir, "read.md"), strings.NewReader("![Local](local%20image.png)\n![Remote](https://example.com/a.png)"))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Images) != 1 || d.Images["local%20image.png"] == nil {
		t.Fatal("local image missing")
	}
	for _, dest := range []string{"../outside.png", "escape.png", outside, "https://example.com/a.png", "//example.com/a.png", "file:///etc/passwd"} {
		if _, err := d.LocalPath(dest); err == nil {
			t.Fatalf("accepted %q", dest)
		}
	}
}
