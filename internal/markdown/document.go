// Package markdown loads read-only Markdown without a browser or network access.
package markdown

import (
	"bufio"
	"bytes"
	"emanuelelongo/proview/internal/gifplay"
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
	Name       string
	Source     []byte
	Root       ast.Node
	Images     map[string]image.Image
	Animations map[string]*gifplay.Animation
	Tokens     map[ast.Node][]chroma.Token
	Diagrams   *mermaid.Renderer
	directory  string
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
	d.Animations = make(map[string]*gifplay.Animation)
	if path != "" {
		d.directory, _ = filepath.EvalSymlinks(filepath.Dir(path))
	}
	imageBytes := int64(0)
	animationBytes := int64(0)
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
				if img, animation := d.loadImageResource(dest, (64<<20)-imageBytes, gifplay.MaxBytes-animationBytes); img != nil {
					d.Images[dest] = img
					imageBytes += decodedImageBytes(img)
					if animation != nil {
						d.Animations[dest] = animation
						animationBytes += animation.Bytes
					}
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

// Charge retained buffers at their actual representation; the pre-decode check
// still reserves eight bytes per pixel for a possibly 16-bit image.
func decodedImageBytes(img image.Image) int64 {
	switch img := img.(type) {
	case *image.NRGBA:
		return int64(len(img.Pix))
	case *image.RGBA:
		return int64(len(img.Pix))
	case *image.NRGBA64:
		return int64(len(img.Pix))
	case *image.RGBA64:
		return int64(len(img.Pix))
	case *image.Gray:
		return int64(len(img.Pix))
	case *image.Gray16:
		return int64(len(img.Pix))
	case *image.Paletted:
		return int64(len(img.Pix)) + 8192
	case *image.YCbCr:
		return int64(len(img.Y) + len(img.Cb) + len(img.Cr))
	default:
		return int64(img.Bounds().Dx()) * int64(img.Bounds().Dy()) * 8
	}
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
	img, _ := d.loadImageResource(destination, budget, 0)
	return img
}

func (d *Document) loadImageResource(destination string, budget, animationBudget int64) (image.Image, *gifplay.Animation) {
	path, err := d.LocalPath(destination)
	if err != nil {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil, nil
	}
	reader := bufio.NewReader(io.LimitReader(f, 16<<20))
	config, format, err := image.DecodeConfig(reader)
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 8_000_000 || int64(config.Width)*int64(config.Height) > budget/8 {
		return nil, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, nil
	}
	if format == "gif" && animationBudget > 0 {
		data, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
		if err == nil && len(data) <= 16<<20 {
			if animation, err := gifplay.Decode(data, animationBudget); err == nil && animation.Animated() {
				return animation.NewPlayer().Image(), animation
			}
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, nil
		}
	}
	reader.Reset(io.LimitReader(f, 16<<20))
	img, _, err := image.Decode(reader)
	if err != nil || decodedImageBytes(img) > budget {
		return nil, nil
	}
	return img, nil
}

func (d *Document) Info() string {
	return fmt.Sprintf("Markdown · Read only · %.2f MB", float64(len(d.Source))/1_000_000)
}
