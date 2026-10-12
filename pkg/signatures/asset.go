package signatures

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"fmt"
	"golang.org/x/image/colornames"
	xdraw "golang.org/x/image/draw"
	"image"
	"image/draw"
	"image/png"
	"io"
	"math"
	"path/filepath"
	"strings"
	"sync"

	vector "github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"
	"github.com/tdewolff/canvas/renderers/rasterizer"
)

const MaxFileSize = 16 << 20

type Kind string

const (
	SVG   Kind = "svg"
	PNG   Kind = "png"
	Drawn Kind = "drawn"
)

// Asset is a self-contained signature. Documents own snapshots of assets.
type Asset struct {
	Kind      Kind     `json:"kind"`
	Data      []byte   `json:"data"`
	Drawing   *Drawing `json:"drawing,omitempty"`
	Thickness float64  `json:"thickness,omitempty"`
}

type Prepared struct {
	Asset   Asset
	PNG     []byte
	Preview image.Image
}

func (a Asset) Prepare() (Prepared, error) {
	if err := a.Validate(); err != nil {
		return Prepared{}, err
	}
	preview, err := a.Render(1024)
	if err != nil {
		return Prepared{}, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, preview); err != nil {
		return Prepared{}, err
	}
	return Prepared{Asset: a.Clone(), PNG: out.Bytes(), Preview: preview}, nil
}

func (a Asset) Clone() Asset {
	a.Data = bytes.Clone(a.Data)
	if a.Drawing != nil {
		d := a.Drawing.Clone()
		a.Drawing = &d
	}
	return a
}

func Import(name string, data []byte) (Asset, error) {
	if len(data) == 0 || len(data) > MaxFileSize {
		return Asset{}, errors.New("signature file is empty or larger than 16 MB")
	}
	a := Asset{Data: bytes.Clone(data)}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".svg":
		a.Kind = SVG
	case ".png":
		a.Kind = PNG
	default:
		return Asset{}, errors.New("choose an SVG or PNG signature")
	}
	if err := a.Validate(); err != nil {
		return Asset{}, err
	}
	picture, err := a.Render(180)
	if err != nil {
		return Asset{}, err
	}
	visible := false
	for y := picture.Bounds().Min.Y; y < picture.Bounds().Max.Y && !visible; y++ {
		for x := picture.Bounds().Min.X; x < picture.Bounds().Max.X; x++ {
			_, _, _, alpha := picture.At(x, y).RGBA()
			if alpha > 0 {
				visible = true
				break
			}
		}
	}
	if !visible {
		return Asset{}, errors.New("signature contains no visible marks")
	}
	return a, nil
}

func (a Asset) Validate() error {
	if math.IsNaN(a.Thickness) || math.IsInf(a.Thickness, 0) || a.Thickness < 0 || a.Thickness > 10 || a.Thickness != 0 && a.Kind != SVG {
		return errors.New("invalid signature thickness")
	}
	if len(a.Data) == 0 || len(a.Data) > MaxFileSize {
		return errors.New("invalid signature size")
	}
	if a.Kind == PNG {
		config, err := png.DecodeConfig(bytes.NewReader(a.Data))
		if err != nil {
			return fmt.Errorf("invalid PNG signature: %w", err)
		}
		if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16_000_000 {
			return errors.New("PNG signature exceeds 16 million pixels")
		}
		_, err = png.Decode(bytes.NewReader(a.Data))
		return err
	}
	if a.Kind != SVG && a.Kind != Drawn {
		return errors.New("unknown signature format")
	}
	if a.Kind == Drawn {
		if a.Drawing == nil {
			return errors.New("drawn signature has no original strokes")
		}
		generated, err := FromDrawing(*a.Drawing)
		if err != nil {
			return err
		}
		if !bytes.Equal(generated.Data, a.Data) {
			return errors.New("drawn signature does not match its original strokes")
		}
	}
	_, err := parseSVG(a.Data)
	return err
}

func parseSVG(data []byte) (result *vector.Canvas, failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			failure = fmt.Errorf("invalid SVG geometry: %v", recovered)
		}
	}()
	allowed := map[string]bool{"svg": true, "g": true, "path": true, "line": true, "polyline": true, "polygon": true, "rect": true, "circle": true, "ellipse": true, "title": true, "desc": true}
	styles := map[string]bool{"fill": true, "stroke": true, "stroke-width": true, "stroke-linecap": true, "stroke-linejoin": true, "stroke-miterlimit": true, "fill-rule": true, "opacity": true, "fill-opacity": true, "stroke-opacity": true, "stroke-dasharray": true, "stroke-dashoffset": true, "color": true}
	attributes := map[string]bool{"viewBox": true, "width": true, "height": true, "preserveAspectRatio": true, "version": true, "xmlns": true, "id": true, "d": true, "points": true, "x": true, "y": true, "x1": true, "x2": true, "y1": true, "y2": true, "cx": true, "cy": true, "r": true, "rx": true, "ry": true, "transform": true, "style": true}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, roots := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid SVG: %w", err)
		}
		switch t := token.(type) {
		case xml.Directive:
			return nil, errors.New("SVG document declarations are not supported")
		case xml.StartElement:
			if t.Name.Local == "metadata" {
				if err := decoder.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			if t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg" {
				return nil, errors.New("unsupported SVG element namespace")
			}
			if depth == 0 {
				roots++
				if t.Name.Local != "svg" {
					return nil, errors.New("signature must have an SVG root")
				}
			}
			depth++
			if !allowed[t.Name.Local] {
				return nil, fmt.Errorf("unsupported SVG element <%s>; use paths and shapes", t.Name.Local)
			}
			for _, attr := range t.Attr {
				name := attr.Name.Local
				if attr.Name.Space == "xmlns" || name == "xmlns" {
					continue
				}
				if attr.Name.Space != "" && name != "href" {
					continue
				}
				if !attributes[name] && !styles[name] {
					return nil, fmt.Errorf("unsupported SVG attribute %s", name)
				}
				if strings.Contains(strings.ToLower(attr.Value), "url(") {
					return nil, errors.New("SVG references and gradients are not supported")
				}
				if name == "style" {
					for _, declaration := range strings.Split(attr.Value, ";") {
						if strings.TrimSpace(declaration) == "" {
							continue
						}
						key, _, ok := strings.Cut(declaration, ":")
						if !ok || !styles[strings.TrimSpace(key)] {
							return nil, fmt.Errorf("unsupported SVG style %s", strings.TrimSpace(key))
						}
					}
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	if roots != 1 || depth != 0 {
		return nil, errors.New("invalid SVG structure")
	}
	c, err := parseSupportedSVG(data)
	if err != nil {
		return nil, fmt.Errorf("invalid SVG geometry: %w", err)
	}
	w, h := c.Size()
	if w <= 0 || h <= 0 || math.IsNaN(w+h) || math.IsInf(w+h, 0) || math.Max(w/h, h/w) > 1000 {
		return nil, errors.New("invalid SVG dimensions or aspect ratio")
	}
	if c.Empty() {
		return nil, errors.New("SVG signature has no visible shapes")
	}
	return c, nil
}

func validatePaint(value string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "none" || value == "currentcolor" || value == "transparent" || value == "inherit" || value == "rebeccapurple" {
		return nil
	}
	if _, ok := colornames.Map[value]; ok {
		return nil
	}
	if strings.HasPrefix(value, "rgb(") || strings.HasPrefix(value, "rgba(") {
		return nil
	}
	if strings.HasPrefix(value, "#") && (len(value) == 4 || len(value) == 5 || len(value) == 7 || len(value) == 9) {
		for _, character := range value[1:] {
			if !strings.ContainsRune("0123456789abcdef", character) {
				return fmt.Errorf("invalid SVG color %q", value)
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported SVG color %q", value)
}

// Render preserves transparency and limits the longest side to pixels.
func (a Asset) Render(pixels int) (result image.Image, failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			failure = fmt.Errorf("cannot render signature: %v", recovered)
		}
	}()
	pixels = max(1, min(pixels, 2048))
	hash := sha256.New()
	_, _ = hash.Write(a.Data)
	_, _ = fmt.Fprintf(hash, "/%s/%d/%g", a.Kind, pixels, a.Thickness)
	var key [32]byte
	copy(key[:], hash.Sum(nil))
	if cached := signatureRenders.get(key); cached != nil {
		return cached, nil
	}
	if a.Kind == PNG {
		result, failure = png.Decode(bytes.NewReader(a.Data))
		if failure == nil && pixels < 1024 && max(result.Bounds().Dx(), result.Bounds().Dy()) > pixels {
			bounds := result.Bounds()
			scale := float64(pixels) / float64(max(bounds.Dx(), bounds.Dy()))
			preview := image.NewNRGBA(image.Rect(0, 0, max(1, int(float64(bounds.Dx())*scale)), max(1, int(float64(bounds.Dy())*scale))))
			xdraw.ApproxBiLinear.Scale(preview, preview.Bounds(), result, bounds, draw.Src, nil)
			result = preview
		}
		if failure == nil {
			signatureRenders.put(key, result)
		}
		return result, failure
	}
	c, err := a.styledCanvas()
	if err != nil {
		return nil, err
	}
	w, h := c.Size()
	resolution := vector.DPMM(math.Max(float64(max(1, pixels))/math.Max(w, h), 1/math.Min(w, h)))
	result = rasterizer.Draw(c, resolution, vector.LinearColorSpace{})
	signatureRenders.put(key, result)
	return result, nil
}

type renderItem struct {
	key    [32]byte
	image  image.Image
	pixels int
}

// Rasterize draws vector artwork at the requested output dimensions, without
// enlarging a thumbnail. It is used for original-resolution image exports.
func (a Asset) Rasterize(width, height int) (result image.Image, failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			failure = fmt.Errorf("cannot rasterize artwork: %v", recovered)
		}
	}()
	if width <= 0 || height <= 0 || int64(width)*int64(height) > 128_000_000 {
		return nil, errors.New("invalid artwork output dimensions")
	}
	if a.Kind == PNG {
		return nil, errors.New("artwork is not vector")
	}
	c, err := a.styledCanvas()
	if err != nil {
		return nil, err
	}
	w, h := c.Size()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	renderer := rasterizer.FromImage(picture, vector.DPMM(1), vector.LinearColorSpace{})
	c.RenderViewTo(renderer, vector.Identity.Scale(float64(width)/w, float64(height)/h))
	renderer.Close()
	return picture, nil
}

type renderCache struct {
	mu      sync.Mutex
	entries map[[32]byte]*list.Element
	order   *list.List
	pixels  int
}

var signatureRenders = renderCache{entries: make(map[[32]byte]*list.Element), order: list.New()}

func (c *renderCache) get(key [32]byte) image.Image {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element := c.entries[key]; element != nil {
		c.order.MoveToFront(element)
		return element.Value.(renderItem).image
	}
	return nil
}
func (c *renderCache) put(key [32]byte, picture image.Image) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pixels := picture.Bounds().Dx() * picture.Bounds().Dy()
	if pixels > 8_000_000 {
		return
	}
	if element := c.entries[key]; element != nil {
		c.order.MoveToFront(element)
		return
	}
	for c.pixels+pixels > 8_000_000 || c.order.Len() >= 64 {
		last := c.order.Back()
		item := last.Value.(renderItem)
		delete(c.entries, item.key)
		c.pixels -= item.pixels
		c.order.Remove(last)
	}
	c.entries[key] = c.order.PushFront(renderItem{key, picture, pixels})
	c.pixels += pixels
}

func (a Asset) PDF() ([]byte, error) {
	if a.Kind == PNG {
		return nil, errors.New("PNG signatures are raster images")
	}
	c, err := a.styledCanvas()
	if err != nil {
		return nil, err
	}
	w, h := c.Size()
	var out bytes.Buffer
	r := pdf.New(&out, w, h, nil)
	c.RenderTo(r)
	if err := r.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (a Asset) WithSettings(smooth, width float64) (Asset, error) {
	if a.Drawing == nil {
		return Asset{}, errors.New("only signatures drawn in Proview can be smoothed")
	}
	d := a.Drawing.Clone()
	d.Smooth, d.Width = smooth, width
	return FromDrawing(d)
}
