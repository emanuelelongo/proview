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
	"sync"
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
	Name           string
	Source         []byte
	Root           ast.Node
	Images         map[string]image.Image
	Animations     map[string]*gifplay.Animation
	Tokens         map[ast.Node][]chroma.Token
	Diagrams       *mermaid.Renderer
	directory      string
	closed         bool
	resourcesMu    sync.RWMutex
	restoreMu      sync.Mutex
	imageLimit     int64
	animationLimit int64
	imagesTrimmed  bool
	tokensMu       sync.Mutex
	tokenized      map[ast.Node]bool
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
	return LoadWithBudget(name, path, r, MaxDocumentImageBytes, MaxDocumentAnimationBytes)
}

// LoadWithBudget applies explicit decoded-image and animation allowances. A
// workspace with several open documents can divide one shared cap between them.
func LoadWithBudget(name, path string, r io.Reader, imageBytes, animationBytes int64) (*Document, error) {
	if imageBytes <= 0 {
		imageBytes = MaxDocumentImageBytes
	}
	if animationBytes <= 0 {
		animationBytes = MaxDocumentAnimationBytes
	}
	return load(name, path, r, imageBytes, animationBytes)
}

func load(name, path string, r io.Reader, imageLimit, animationLimit int64) (*Document, error) {
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
	md := goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithBlockParsers(
		util.Prioritized(&admonitionParser{}, 50),
		util.Prioritized(&detailsParser{}, 50),
		util.Prioritized(&githubAlertParser{BlockParser: parser.NewBlockquoteParser()}, 50),
	)))
	reader := text.NewReader(source)
	reader.Advance(frontMatterEnd(source))
	d := &Document{
		Name: name, Source: source, Root: md.Parser().Parse(reader), Images: map[string]image.Image{},
		Tokens: map[ast.Node][]chroma.Token{}, tokenized: make(map[ast.Node]bool), Diagrams: mermaid.NewRenderer(),
		imageLimit: imageLimit, animationLimit: animationLimit,
	}
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
			if !attemptedImages[dest] && imageBytes < imageLimit {
				attemptedImages[dest] = true
				if img, animation := d.loadImageResource(dest, imageLimit-imageBytes, animationLimit-animationBytes); img != nil {
					d.Images[dest] = img
					imageBytes += decodedImageBytes(img)
					if animation != nil {
						d.Animations[dest] = animation
						animationBytes += animation.Bytes
					}
				}
			}
		}
		return ast.WalkContinue, nil
	})
	return d, nil
}

// TokensFor lazily highlights a code block and caches only blocks actually
// displayed or printed. The cache is disposable; a trimmed document regenerates
// tokens from its immutable source on demand.
func (d *Document) TokensFor(n ast.Node) []chroma.Token {
	if d == nil || n == nil {
		return nil
	}
	d.tokensMu.Lock()
	defer d.tokensMu.Unlock()
	if tokens, ok := d.Tokens[n]; ok {
		return tokens
	}
	if d.tokenized[n] {
		return nil
	}
	if d.Tokens == nil {
		return nil
	}
	d.tokenized[n] = true
	block, ok := n.(*ast.FencedCodeBlock)
	if !ok {
		return nil
	}
	tokens := highlight(Code(d.Source, block), string(block.Language(d.Source)))
	if len(tokens) > 0 {
		d.Tokens[n] = tokens
	}
	return tokens
}

func (d *Document) trimTokens() {
	d.tokensMu.Lock()
	d.Tokens = make(map[ast.Node][]chroma.Token)
	d.tokenized = make(map[ast.Node]bool)
	d.tokensMu.Unlock()
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

// Close releases decoded images and animations. It is safe to call more than
// once. Rebuilding the view afterwards requires loading the document again.
func (d *Document) Close() {
	if d == nil {
		return
	}
	d.resourcesMu.Lock()
	defer d.resourcesMu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	d.Images = nil
	d.Animations = nil
	d.tokensMu.Lock()
	d.Tokens = nil
	d.tokenized = nil
	d.tokensMu.Unlock()
	d.imagesTrimmed = false
	d.Diagrams.Clear()
}

// Image returns the decoded local image for a Markdown destination.
func (d *Document) Image(destination string) image.Image {
	if d == nil {
		return nil
	}
	d.resourcesMu.RLock()
	defer d.resourcesMu.RUnlock()
	return d.Images[destination]
}

// Animation returns the decoded animation for a Markdown destination.
func (d *Document) Animation(destination string) *gifplay.Animation {
	if d == nil {
		return nil
	}
	d.resourcesMu.RLock()
	defer d.resourcesMu.RUnlock()
	return d.Animations[destination]
}

// ImageCount reports the number of decoded local images retained by the document.
func (d *Document) ImageCount() int {
	if d == nil {
		return 0
	}
	d.resourcesMu.RLock()
	defer d.resourcesMu.RUnlock()
	return len(d.Images)
}

// AnimationCount reports the number of decoded animations retained by the document.
func (d *Document) AnimationCount() int {
	if d == nil {
		return 0
	}
	d.resourcesMu.RLock()
	defer d.resourcesMu.RUnlock()
	return len(d.Animations)
}

// ImagesTrimmed reports whether decoded resources must be restored before the
// document is shown again.
func (d *Document) ImagesTrimmed() bool {
	if d == nil {
		return false
	}
	d.resourcesMu.RLock()
	defer d.resourcesMu.RUnlock()
	return d.imagesTrimmed
}

// TrimImages releases decoded local images and animations while retaining the
// parsed document and original Markdown source.
func (d *Document) TrimImages() {
	if d == nil {
		return
	}
	d.resourcesMu.Lock()
	defer d.resourcesMu.Unlock()
	if d.closed {
		return
	}
	// Diagram-only documents have nothing to decode again on activation.
	if len(d.Images) > 0 || len(d.Animations) > 0 {
		d.imagesTrimmed = true
	}
	d.Images = nil
	d.Animations = nil
	d.trimTokens()
	d.Diagrams.Clear()
}

// resourceSnapshot returns d, or a transient copy with decoded still images
// when d is trimmed. Output paths use it so an inactive document prints its
// pictures without restoring them into the retained document.
func (d *Document) resourceSnapshot() *Document {
	if d == nil {
		return nil
	}
	d.resourcesMu.RLock()
	trimmed := d.imagesTrimmed && !d.closed
	d.resourcesMu.RUnlock()
	if !trimmed {
		return d
	}
	snapshot := &Document{
		Name:           d.Name,
		Source:         d.Source,
		Root:           d.Root,
		Tokens:         make(map[ast.Node][]chroma.Token),
		tokenized:      make(map[ast.Node]bool),
		directory:      d.directory,
		imageLimit:     d.imageLimit,
		animationLimit: 0,
	}
	snapshot.Images, _ = snapshot.decodeImages()
	return snapshot
}

// RestoreImages decodes bounded local image resources after TrimImages.
func (d *Document) RestoreImages() {
	if d == nil {
		return
	}
	d.restoreMu.Lock()
	defer d.restoreMu.Unlock()
	d.resourcesMu.RLock()
	trimmed, closed := d.imagesTrimmed, d.closed
	d.resourcesMu.RUnlock()
	if !trimmed || closed {
		return
	}
	images, animations := d.decodeImages()
	d.resourcesMu.Lock()
	defer d.resourcesMu.Unlock()
	if d.closed {
		return
	}
	d.Images, d.Animations = images, animations
	d.imagesTrimmed = false
}

func (d *Document) decodeImages() (map[string]image.Image, map[string]*gifplay.Animation) {
	images := make(map[string]image.Image)
	animations := make(map[string]*gifplay.Animation)
	imageBytes, animationBytes := int64(0), int64(0)
	attempted := make(map[string]bool)
	_ = ast.Walk(d.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		imageNode, ok := n.(*ast.Image)
		if !ok {
			return ast.WalkContinue, nil
		}
		destination := string(imageNode.Destination)
		if attempted[destination] || imageBytes >= d.imageLimit {
			return ast.WalkContinue, nil
		}
		attempted[destination] = true
		picture, animation := d.loadImageResource(destination, d.imageLimit-imageBytes, d.animationLimit-animationBytes)
		if picture != nil {
			images[destination] = picture
			imageBytes += decodedImageBytes(picture)
			if animation != nil {
				animations[destination] = animation
				animationBytes += animation.Bytes
			}
		}
		return ast.WalkContinue, nil
	})
	return images, animations
}

// ImageBytes reports retained decoded image bytes for diagnostics.
func (d *Document) ImageBytes() int64 {
	if d == nil {
		return 0
	}
	d.resourcesMu.RLock()
	defer d.resourcesMu.RUnlock()
	var total int64
	for _, img := range d.Images {
		total += decodedImageBytes(img)
	}
	return total
}

// AnimationBytes reports retained GIF animation bytes for diagnostics.
func (d *Document) AnimationBytes() int64 {
	if d == nil {
		return 0
	}
	d.resourcesMu.RLock()
	defer d.resourcesMu.RUnlock()
	var total int64
	for _, animation := range d.Animations {
		if animation != nil {
			total += animation.Bytes
		}
	}
	return total
}

// HasDecodedImages reports whether decoded image or animation resources are retained.
func (d *Document) HasDecodedImages() bool {
	if d == nil {
		return false
	}
	d.resourcesMu.RLock()
	defer d.resourcesMu.RUnlock()
	return len(d.Images) > 0 || len(d.Animations) > 0
}

// HasReleasableResources reports decoded images, animations or cached diagram
// rasters that TrimImages would release.
func (d *Document) HasReleasableResources() bool {
	return d.HasDecodedImages() || d != nil && (d.Diagrams.CachedPixels() > 0 || d.hasCachedTokens())
}

func (d *Document) hasCachedTokens() bool {
	d.tokensMu.Lock()
	defer d.tokensMu.Unlock()
	return len(d.Tokens) > 0
}

func (d *Document) Info() string {
	return fmt.Sprintf("Markdown · Read only · %.2f MB", float64(len(d.Source))/1_000_000)
}
