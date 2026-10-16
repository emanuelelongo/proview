package document

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"

	"emanuelelongo/proview/internal/pdfengine"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"golang.org/x/image/vector"
)

type TextPoint = pdfengine.Point
type TextQuad = pdfengine.Quad
type TextLayout = pdfengine.Layout
type RedactionID uint64

type Redaction struct {
	ID     RedactionID
	PageID uint64
	Quads  []TextQuad
}

func cloneRedactions(input []Redaction) []Redaction {
	if len(input) == 0 {
		return nil
	}
	result := make([]Redaction, len(input))
	for i, r := range input {
		r.Quads = append([]TextQuad(nil), r.Quads...)
		result[i] = r
	}
	return result
}

// TextGeometry owns a copy of the glyph layout. Source caches are synchronized
// and independent of editor mutations, so workers can use rendering snapshots.
func (d *Document) TextGeometry(page int) (TextLayout, error) {
	if d.Format() != FormatPDF {
		return TextLayout{}, errors.New("text selection requires a PDF")
	}
	source, index, err := d.pageSource(page)
	if err != nil {
		return TextLayout{}, err
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.textLayouts == nil {
		source.textLayouts = make(map[int]TextLayout)
	}
	layout, ok := source.textLayouts[index]
	if !ok {
		layout, err = pdfengine.TextLayout(source.original, index)
		if err != nil {
			return TextLayout{}, err
		}
		source.textLayouts[index] = layout
	}
	layout.Glyphs = append([]pdfengine.Glyph(nil), layout.Glyphs...)
	if turns := d.pages[page].turns; turns != 0 {
		for i := 0; i < turns; i++ {
			for j := range layout.Glyphs {
				layout.Glyphs[j].Quad = rotateTextQuad(layout.Glyphs[j].Quad, "right")
				p := layout.Glyphs[j].Origin
				layout.Glyphs[j].Origin = TextPoint{X: 1 - p.Y, Y: p.X}
			}
			layout.Width, layout.Height = layout.Height, layout.Width
		}
	}
	return layout, nil
}

func (d *Document) AddRedaction(page int, quads []TextQuad) (Redaction, error) {
	if d.Format() != FormatPDF {
		return Redaction{}, errors.New("redaction requires a PDF")
	}
	if d.activeEdit != nil {
		return Redaction{}, ErrEditInProgress
	}
	if err := d.validatePage(page); err != nil {
		return Redaction{}, err
	}
	if len(quads) == 0 {
		return Redaction{}, errors.New("select text or an area before adding a redaction")
	}
	for _, q := range quads {
		if !q.Valid() {
			return Redaction{}, errors.New("invalid redaction geometry")
		}
	}
	before := d.snapshot()
	d.nextRedactionID++
	r := Redaction{ID: d.nextRedactionID, PageID: d.pages[page].id, Quads: append([]TextQuad(nil), quads...)}
	d.redactions = append(d.redactions, r)
	d.record(before)
	return cloneRedactions([]Redaction{r})[0], nil
}

func (d *Document) RemoveRedaction(id RedactionID) error {
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	for i, r := range d.redactions {
		if r.ID == id {
			before := d.snapshot()
			d.redactions = append(d.redactions[:i], d.redactions[i+1:]...)
			d.record(before)
			return nil
		}
	}
	return errors.New("redaction not found")
}
func (d *Document) HasRedactions() bool     { return len(d.redactions) > 0 }
func (d *Document) Redactions() []Redaction { return cloneRedactions(d.redactions) }
func (d *Document) RedactionsForPage(page int) []Redaction {
	if page < 0 || page >= len(d.pages) {
		return nil
	}
	var result []Redaction
	for _, r := range d.redactions {
		if r.PageID == d.pages[page].id {
			result = append(result, r)
		}
	}
	return cloneRedactions(result)
}

// Keys remain stable when pages are reordered. The GUI retains them outside
// Undo to detect overwrites that could restore previously removed information.
func (d *Document) RedactionKeys() []string {
	var keys []string
	for _, r := range d.redactions {
		data, _ := json.Marshal(struct {
			Page  uint64
			Quads []TextQuad
		}{r.PageID, r.Quads})
		keys = append(keys, fmt.Sprintf("%x", sha256.Sum256(data)))
	}
	return keys
}

func (d *Document) redactExport(input []byte) ([]byte, error) {
	if !d.HasRedactions() {
		return input, nil
	}
	var regions []pdfengine.Region
	for page := range d.pages {
		for _, r := range d.RedactionsForPage(page) {
			for _, q := range r.Quads {
				regions = append(regions, pdfengine.Region{Page: page, Quad: q})
			}
		}
	}
	var output []byte
	var err error
	if len(d.signatures) == 0 {
		output, err = pdfengine.RedactAndSanitize(input, regions)
	} else {
		var sources [][]byte
		seen := make(map[*Source]bool)
		for _, page := range d.pages {
			if !seen[page.source] {
				seen[page.source] = true
				sources = append(sources, page.source.Original())
			}
		}
		output, err = pdfengine.RedactComposedPDF(input, regions, sources)
	}
	if err != nil {
		return nil, fmt.Errorf("could not redact PDF: %w", err)
	}
	if err := api.Validate(context.Background(), bytes.NewReader(output), model.NewDefaultConfiguration(), nil); err != nil {
		return nil, fmt.Errorf("invalid redacted PDF: %w", err)
	}
	return output, nil
}

// PaintRedactions creates a presentation mask; export uses native removal.
func PaintRedactions(size image.Point, quads []TextQuad) *image.NRGBA {
	result := image.NewNRGBA(image.Rect(0, 0, max(1, size.X), max(1, size.Y)))
	raster := vector.NewRasterizer(result.Bounds().Dx(), result.Bounds().Dy())
	for _, q := range quads {
		raster.MoveTo(float32(q.UL.X)*float32(size.X), float32(q.UL.Y)*float32(size.Y))
		for _, p := range []TextPoint{q.UR, q.LR, q.LL} {
			raster.LineTo(float32(p.X)*float32(size.X), float32(p.Y)*float32(size.Y))
		}
		raster.ClosePath()
	}
	raster.Draw(result, result.Bounds(), &image.Uniform{C: color.Black}, image.Point{})
	return result
}
func (d *Document) paintRedactions(page int, input image.Image) image.Image {
	var quads []TextQuad
	for _, r := range d.RedactionsForPage(page) {
		quads = append(quads, r.Quads...)
	}
	if len(quads) == 0 {
		return input
	}
	result := image.NewNRGBA(input.Bounds())
	draw.Draw(result, result.Bounds(), input, input.Bounds().Min, draw.Src)
	mask := PaintRedactions(input.Bounds().Size(), quads)
	draw.Draw(result, result.Bounds(), mask, image.Point{}, draw.Over)
	return result
}
