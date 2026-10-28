// Package markdown loads read-only Markdown without a browser or network access.
package markdown

import (
	"bufio"
	"bytes"
	"emanuelelongo/proview/internal/mermaid"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const MaxSourceBytes = 2 << 20

type Document struct {
	Name      string
	Source    []byte
	Root      ast.Node
	Images    map[string]image.Image
	Tokens    map[ast.Node][]chroma.Token
	Diagrams  *mermaid.Renderer
	directory string
}

func IsFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// Load prepares parsing, highlighting and bounded local images off the UI thread.
func Load(name, path string, r io.Reader) (*Document, error) {
	source, err := io.ReadAll(io.LimitReader(r, MaxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(source) > MaxSourceBytes {
		return nil, errors.New("Markdown exceeds the 2 MiB limit")
	}
	source = bytes.TrimPrefix(source, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(source) || bytes.ContainsRune(source, 0) {
		return nil, errors.New("Markdown must contain UTF-8 text")
	}
	source = bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))
	md := goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithBlockParsers(util.Prioritized(&admonitionParser{}, 50))))
	reader := text.NewReader(source)
	reader.Advance(frontMatterEnd(source))
	d := &Document{Name: name, Source: source, Root: md.Parser().Parse(reader), Images: map[string]image.Image{}, Tokens: map[ast.Node][]chroma.Token{}, Diagrams: mermaid.NewRenderer()}
	expandHTML(d.Root, source)
	if path != "" {
		d.directory, _ = filepath.EvalSymlinks(filepath.Dir(path))
	}
	imageBytes := int64(0)
	attemptedImages := make(map[string]bool)
	_ = ast.Walk(d.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Image:
			dest := string(n.Destination)
			if !attemptedImages[dest] && imageBytes < 64<<20 {
				attemptedImages[dest] = true
				if img := d.loadImage(dest, (64<<20)-imageBytes); img != nil {
					d.Images[dest] = img
					imageBytes += int64(img.Bounds().Dx()) * int64(img.Bounds().Dy()) * 8
				}
			}
		case *ast.FencedCodeBlock:
			if tokens := highlight(Code(source, n), string(n.Language(source))); len(tokens) > 0 {
				d.Tokens[n] = tokens
			}
		}
		return ast.WalkContinue, nil
	})
	return d, nil
}

func Code(source []byte, n ast.Node) string {
	var b strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		line := n.Lines().At(i)
		b.Write(line.Value(source))
	}
	return b.String()
}

func highlight(code, language string) []chroma.Token {
	if len(code) > 128<<10 {
		return nil
	}
	lexer := lexers.Get(language)
	if lexer == nil {
		return nil
	}
	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return nil
	}
	tokens := make([]chroma.Token, 0)
	for t := iterator(); t != chroma.EOF; t = iterator() {
		tokens = append(tokens, t)
		if len(tokens) > 32768 {
			return nil
		}
	}
	return tokens
}

// LocalPath accepts relative paths confined to the canonical document directory.
// Remote resources, absolute paths and escaping symlinks are never followed.
func (d *Document) LocalPath(destination string) (string, error) {
	u, err := url.Parse(destination)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" || d.directory == "" || filepath.IsAbs(u.Path) || strings.Contains(u.Path, "\\") {
		return "", errors.New("not a local document resource")
	}
	path, err := filepath.EvalSymlinks(filepath.Join(d.directory, filepath.FromSlash(u.Path)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(d.directory, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("resource is outside the document directory")
	}
	return path, nil
}

// Budget conservatively allows eight bytes per pixel, including 16-bit PNGs.
func (d *Document) loadImage(destination string, budget int64) image.Image {
	path, err := d.LocalPath(destination)
	if err != nil {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil
	}
	reader := bufio.NewReader(io.LimitReader(f, 16<<20))
	config, _, err := image.DecodeConfig(reader)
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 8_000_000 || int64(config.Width)*int64(config.Height) > budget/8 {
		return nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil
	}
	reader.Reset(io.LimitReader(f, 16<<20))
	img, _, err := image.Decode(reader)
	if err != nil {
		return nil
	}
	return img
}

func (d *Document) Info() string {
	return fmt.Sprintf("Markdown · Read only · %.2f MB", float64(len(d.Source))/1_000_000)
}
