package document

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// NewImage uses a predictable allocation limit for user supplied dimensions.
func NewImage(name string, width, height int, background color.Color) (*Document, error) {
	if width < 1 || height < 1 || width > 16384 || height > 16384 || int64(width)*int64(height) > 32_000_000 {
		return nil, fmt.Errorf("document: dimensions must be 1–16384 pixels, up to 32 million pixels total")
	}
	if background == nil {
		background = color.White
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: background}, image.Point{}, draw.Src)
	return imageDocument(name, img)
}

func imageDocument(name string, img image.Image) (*Document, error) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return nil, err
	}
	source, err := LoadBytes(name, "", encoded.Bytes())
	if err != nil {
		return nil, err
	}
	doc, err := New(source)
	if err == nil {
		doc.MarkUnsaved()
	}
	return doc, err
}

// CropPage creates an independent editable PNG, including visible overlays.
// Coordinates refer to the rendered page (144 DPI for PDFs).
func (d *Document) CropPage(page int, rect image.Rectangle, name string) (*Document, error) {
	img, err := d.RenderPage(page)
	if err != nil {
		return nil, err
	}
	rect = rect.Intersect(img.Bounds())
	if rect.Empty() {
		return nil, ErrInvalidRect
	}
	cropped := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(cropped, cropped.Bounds(), img, rect.Min, draw.Src)
	return imageDocument(name, cropped)
}

// NewImageFromImage creates an independent, unsaved PNG document.
func NewImageFromImage(name string, img image.Image) (*Document, error) {
	if img == nil || img.Bounds().Empty() || int64(img.Bounds().Dx())*int64(img.Bounds().Dy()) > 32_000_000 {
		return nil, ErrInvalidRect
	}
	return imageDocument(name, img)
}
