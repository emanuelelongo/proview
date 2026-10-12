package document

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	img "emanuelelongo/proview/pkg/images"
	"github.com/gen2brain/go-fitz"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const (
	pdfPreviewDPI   = 144
	pdfThumbnailDPI = 72
)

type Format string

const (
	FormatPNG  Format = "png"
	FormatJPEG Format = "jpeg"
	FormatPDF  Format = "pdf"
	FormatGIF  Format = "gif"
	FormatTIFF Format = "tiff"
	FormatBMP  Format = "bmp"
	FormatWebP Format = "webp"
)

type pageData struct {
	preview   image.Image
	thumbnail image.Image
	size      image.Point
}

type Source struct {
	mu          sync.Mutex
	name        string
	location    string
	format      Format
	original    []byte
	pages       []pageData
	pdf         *fitz.Document
	textLayouts map[int]TextLayout
}

func Load(name string, reader io.Reader) (*Source, error) {
	return LoadFrom(name, name, reader)
}

func LoadFrom(name, location string, reader io.Reader) (*Source, error) {
	if reader == nil {
		return nil, errors.New("document: nil reader")
	}

	format, err := formatFromName(name)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("document: read %q: %w", name, err)
	}

	var pages []pageData
	var pdf *fitz.Document
	switch format {
	case FormatPNG, FormatJPEG, FormatGIF, FormatTIFF, FormatBMP, FormatWebP:
		pages, err = loadImage(data)
	case FormatPDF:
		pages, pdf, err = loadPDF(data)
	}
	if err != nil {
		return nil, fmt.Errorf("document: load %q: %w", name, err)
	}

	source := &Source{
		name:     name,
		location: location,
		format:   format,
		original: data,
		pages:    pages,
		pdf:      pdf,
	}
	if pdf != nil {
		runtime.SetFinalizer(source, (*Source).Close)
	}
	return source, nil
}

// Close releases the PDF renderer. Cached page previews remain available.
func (s *Source) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pdf != nil {
		_ = s.pdf.Close()
		s.pdf = nil
	}
}

func (s *Source) Name() string {
	return s.name
}

func (s *Source) Location() string {
	return s.location
}

func (s *Source) Format() Format {
	return s.format
}

func (s *Source) Original() []byte {
	return bytes.Clone(s.original)
}

func (s *Source) PageCount() int {
	return len(s.pages)
}

func (s *Source) Preview(page int) (image.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if page < 0 || page >= len(s.pages) {
		return nil, fmt.Errorf("document: page %d out of range", page)
	}
	if s.pages[page].preview == nil && s.format == FormatPDF {
		if err := s.openPDF(); err != nil {
			return nil, err
		}
		preview, err := s.pdf.ImageDPI(page, pdfPreviewDPI)
		if err != nil {
			return nil, fmt.Errorf("render PDF page %d: %w", page+1, err)
		}
		s.pages[page].preview = preview
		s.pages[page].size = preview.Bounds().Size()
	}
	return s.pages[page].preview, nil
}

func (s *Source) Thumbnail(page int) (image.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if page < 0 || page >= len(s.pages) {
		return nil, fmt.Errorf("document: page %d out of range", page)
	}
	if s.format != FormatPDF {
		return s.pages[page].preview, nil
	}
	if s.pages[page].thumbnail == nil {
		if err := s.openPDF(); err != nil {
			return nil, err
		}
		thumbnail, err := s.pdf.ImageDPI(page, pdfThumbnailDPI)
		if err != nil {
			return nil, fmt.Errorf("render PDF thumbnail %d: %w", page+1, err)
		}
		s.pages[page].thumbnail = thumbnail
	}
	return s.pages[page].thumbnail, nil
}

// openPDF must be called with s.mu held.
func (s *Source) openPDF() error {
	if s.pdf != nil {
		return nil
	}
	var err error
	s.pdf, err = fitz.NewFromMemory(s.original)
	return err
}

func (s *Source) PageSize(page int) (image.Point, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if page < 0 || page >= len(s.pages) {
		return image.Point{}, fmt.Errorf("document: page %d out of range", page)
	}
	return s.pages[page].size, nil
}

func formatFromName(name string) (Format, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return FormatPNG, nil
	case ".gif":
		return FormatGIF, nil
	case ".tif", ".tiff":
		return FormatTIFF, nil
	case ".bmp":
		return FormatBMP, nil
	case ".webp":
		return FormatWebP, nil
	case ".jpg", ".jpeg":
		return FormatJPEG, nil
	case ".pdf":
		return FormatPDF, nil
	default:
		return "", fmt.Errorf("document: unsupported format %q", filepath.Ext(name))
	}
}

func FormatFromName(name string) (Format, error) {
	return formatFromName(name)
}

func loadImage(data []byte) ([]pageData, error) {
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	// Normalize camera orientation once, before cropping, editing and OCR.
	decoded = img.JPEGOrientation(data).Apply(decoded)
	if format == "tiff" {
		decoded = img.TIFFOrientation(data).Apply(decoded)
	}
	return []pageData{{preview: decoded, size: decoded.Bounds().Size()}}, nil
}

func loadPDF(data []byte) ([]pageData, *fitz.Document, error) {
	doc, err := fitz.NewFromMemory(data)
	if err != nil {
		return nil, nil, fmt.Errorf("open PDF (the file may be encrypted or damaged): %w", err)
	}

	pageCount := doc.NumPage()
	if pageCount == 0 {
		_ = doc.Close()
		return nil, nil, errors.New("PDF has no renderable pages")
	}

	pages := make([]pageData, pageCount)
	for page := range pageCount {
		bounds, err := doc.Bound(page)
		if err != nil {
			_ = doc.Close()
			return nil, nil, fmt.Errorf("inspect PDF page %d: %w", page+1, err)
		}
		pages[page].size = image.Pt(
			int(math.Round(float64(bounds.Dx())*pdfPreviewDPI/72)),
			int(math.Round(float64(bounds.Dy())*pdfPreviewDPI/72)),
		)
	}
	return pages, doc, nil
}

// OpenExtensions lists the formats accepted by the document file picker.
func OpenExtensions() []string {
	return []string{".png", ".jpg", ".jpeg", ".gif", ".tif", ".tiff", ".bmp", ".webp", ".pdf"}
}

// CanExport reports whether Proview can write a format without changing its encoding.
func CanExport(format Format) bool {
	return format == FormatPNG || format == FormatJPEG || format == FormatPDF
}
