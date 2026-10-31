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
	"sync/atomic"

	"emanuelelongo/proview/internal/gifplay"
	"emanuelelongo/proview/internal/pdfengine"
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

// Each Source opens its own MuPDF context, so the library-wide store limit is
// per source. Keep native resource caching bounded; Go-side raster caches are
// bounded separately in raster_cache.go.
func init() {
	fitz.MaxStore = 32 << 20
}

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
	preview                    image.Image
	thumbnail                  image.Image
	size                       image.Point
	previewUsed, thumbnailUsed uint64
}

type Source struct {
	animation         *gifplay.Animation
	annotationBase    *Source
	importedOverlays  []Overlay
	hasPDFAnnotations bool
	annotPages        map[int]bool
	noForms           bool
	formsOnce         sync.Once
	forms             []pdfengine.FormField
	formsByPage       map[int][]pdfengine.FormField
	formsErr          error
	formRendererMu    sync.Mutex
	formRenderer      *pdfengine.FormRenderer
	formRendererKey   uint64
	mu                sync.Mutex
	sizeMu            sync.RWMutex
	animationMu       sync.Mutex
	name              string
	location          string
	format            Format
	original          []byte
	pages             []pageData
	pdf               *fitz.Document
	textLayouts       textLayoutCache
	rasterClock       atomic.Uint64
	trimMu            sync.Mutex
	trimQueue         []func()
	trimRunning       bool
	rasterBytes       atomic.Int64
	animationBytes    atomic.Int64
	rasterRegistered  bool
}

func Load(name string, reader io.Reader) (*Source, error) {
	return LoadFrom(name, name, reader)
}

func LoadFrom(name, location string, reader io.Reader) (*Source, error) {
	if reader == nil {
		return nil, errors.New("document: nil reader")
	}
	if _, err := formatFromName(name); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("document: read %q: %w", name, err)
	}
	return loadSource(name, location, data, true)
}

// LoadBytes takes ownership of data: callers that already hold the file in
// memory avoid the second full copy io.ReadAll would make. data must not be
// modified afterwards.
func LoadBytes(name, location string, data []byte) (*Source, error) {
	return loadSource(name, location, data, true)
}

func loadSource(name, location string, data []byte, importAnnotations bool) (*Source, error) {
	format, err := formatFromName(name)
	if err != nil {
		return nil, err
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
	if format == FormatGIF {
		// Keep oversized animations usable as static images.
		source.ensureAnimation()
		source.animationMu.Lock()
		animation := source.animation
		source.animationMu.Unlock()
		if animation != nil {
			source.pages[0].preview = animation.NewPlayer().Image()
		}
	}
	if format != FormatPDF {
		for i := range source.pages {
			source.rasterBytes.Add(rasterBytes(source.pages[i].preview))
		}
	}
	if pdf != nil {
		if importAnnotations {
			scan, e := readPDFAnnotations(data)
			if e != nil {
				pdf.Close()
				return nil, fmt.Errorf("document: read PDF annotations: %w", e)
			}
			source.hasPDFAnnotations = scan.hasPDFAnnotations
			source.annotPages = scan.annotPages
			source.noForms = scan.noForms
			source.importedOverlays = scan.overlays
			if len(scan.overlays) > 0 {
				source.annotationBase, e = loadSource(name, location, scan.base, false)
				if e != nil {
					pdf.Close()
					return nil, e
				}
				// Retained unsupported annotations still need their standard appearances.
				source.annotationBase.hasPDFAnnotations = scan.hasPDFAnnotations
				source.annotationBase.annotPages = scan.retainedPages
				source.annotationBase.noForms = scan.noForms
				// Pages render from the base; the outer source keeps only its
				// bytes and reopens lazily if ever asked to render, so it neither
				// holds a second MuPDF context nor halves the shared raster budget.
				_ = pdf.Close()
				source.pdf = nil
				runtime.SetFinalizer(source, (*Source).Close)
				return source, nil
			}
		}
		runtime.SetFinalizer(source, (*Source).Close)
	}
	source.registerRasterSource()
	return source, nil
}

// Close releases the PDF renderer, disposable rasters and glyph caches. Original
// bytes remain available, so a closed source can still be reopened if needed.
func (s *Source) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.annotationBase != nil {
		s.annotationBase.Close()
	}
	if s.pdf != nil {
		_ = s.pdf.Close()
		s.pdf = nil
	}
	s.formRendererMu.Lock()
	if s.formRenderer != nil {
		s.formRenderer.Close()
		s.formRenderer = nil
		s.formRendererKey = 0
	}
	s.formRendererMu.Unlock()
	for i := range s.pages {
		s.pages[i].preview = nil
		s.pages[i].thumbnail = nil
	}
	s.rasterBytes.Store(0)
	s.textLayouts = textLayoutCache{}
	s.unregisterRasterSourceLocked()
	s.animationMu.Lock()
	s.animation = nil
	s.animationBytes.Store(0)
	s.animationMu.Unlock()
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

// originalBytes exposes the immutable source buffer to internal readers that
// never mutate it. Avoids a full-file copy for export and page collection.
func (s *Source) originalBytes() []byte {
	return s.original
}

func (s *Source) PageCount() int {
	return len(s.pages)
}

func (s *Source) hasPageAnnotations(page int) bool {
	return s.hasPDFAnnotations || s.annotPages[page]
}

func (s *Source) Preview(page int) (image.Image, error) {
	if s.format == FormatPDF {
		_, _ = s.formFields()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if page < 0 || page >= len(s.pages) {
		return nil, fmt.Errorf("document: page %d out of range", page)
	}
	if s.pages[page].preview == nil {
		var preview image.Image
		var err error
		if s.format == FormatPDF {
			if err = s.openPDF(); err != nil {
				return nil, err
			}
			if len(s.formsByPage[page]) > 0 || s.hasPageAnnotations(page) {
				preview, err = s.renderFormPage(s.original, page, pdfPreviewDPI, 0)
			} else {
				preview, err = s.pdf.ImageDPI(page, pdfPreviewDPI)
			}
			if err != nil {
				return nil, fmt.Errorf("render PDF page %d: %w", page+1, err)
			}
			s.rasterBytes.Add(rasterBytes(preview))
		} else {
			// Inactive image documents release their decoded raster; reopen it
			// lazily from the immutable encoded bytes.
			var pages []pageData
			pages, err = loadImage(s.original)
			if err != nil {
				return nil, fmt.Errorf("render image page %d: %w", page+1, err)
			}
			preview = pages[0].preview
			// Match the first frame shown by the animation player, which
			// composites frames; the static decoder can differ for GIFs.
			if s.format == FormatGIF {
				if anim := s.ensureAnimation(); anim != nil {
					if frame := anim.NewPlayer().Image(); frame != nil {
						preview = frame
					}
				}
			}
			s.rasterBytes.Add(rasterBytes(preview))
		}
		s.pages[page].preview = preview
		s.sizeMu.Lock()
		s.pages[page].size = preview.Bounds().Size()
		s.sizeMu.Unlock()
	}
	preview := s.pages[page].preview
	s.touchRaster(page, false)
	return preview, nil
}

func (s *Source) Thumbnail(page int) (image.Image, error) {
	if s.format != FormatPDF {
		return s.Preview(page)
	}
	_, _ = s.formFields()
	s.mu.Lock()
	defer s.mu.Unlock()
	if page < 0 || page >= len(s.pages) {
		return nil, fmt.Errorf("document: page %d out of range", page)
	}
	if s.pages[page].thumbnail == nil {
		if err := s.openPDF(); err != nil {
			return nil, err
		}
		var thumbnail image.Image
		var err error
		if len(s.formsByPage[page]) > 0 || s.hasPageAnnotations(page) {
			thumbnail, err = s.renderFormPage(s.original, page, pdfThumbnailDPI, 0)
		} else {
			thumbnail, err = s.pdf.ImageDPI(page, pdfThumbnailDPI)
		}
		if err != nil {
			return nil, fmt.Errorf("render PDF thumbnail %d: %w", page+1, err)
		}
		s.pages[page].thumbnail = thumbnail
		s.rasterBytes.Add(rasterBytes(thumbnail))
	}
	thumbnail := s.pages[page].thumbnail
	s.touchRaster(page, true)
	return thumbnail, nil
}

func (s *Source) renderFormPage(data []byte, page int, dpi float64, key uint64) (image.Image, error) {
	if page < 0 || page >= len(s.pages) {
		return nil, fmt.Errorf("document: page %d out of range", page+1)
	}
	s.formRendererMu.Lock()
	defer s.formRendererMu.Unlock()
	if s.formRenderer == nil || s.formRendererKey != key {
		if s.formRenderer != nil {
			s.formRenderer.Close()
		}
		var err error
		s.formRenderer, err = pdfengine.NewFormRenderer(data)
		if err != nil {
			s.formRenderer = nil
			s.formRendererKey = 0
			return nil, err
		}
		s.formRendererKey = key
	}
	preview, err := s.formRenderer.RenderPage(page, dpi)
	if err != nil {
		return nil, err
	}
	flattenOnWhite(preview)
	return preview, nil
}

// openPDF must be called with s.mu held.
func (s *Source) openPDF() error {
	if s.pdf != nil {
		return nil
	}
	var err error
	s.pdf, err = fitz.NewFromMemory(s.original)
	if err == nil {
		// A source reopened after Close participates in the shared budget again.
		s.registerRasterSourceLocked()
	}
	return err
}

func (s *Source) PageSize(page int) (image.Point, error) {
	s.sizeMu.RLock()
	defer s.sizeMu.RUnlock()
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
