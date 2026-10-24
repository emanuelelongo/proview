package document

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"io"
	"math"
	"strconv"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// PrintPDF runs on a rendering snapshot. It includes current edits without
// changing history, the save target or the original source. Pages are zero-based;
// an empty selection prints all pages. PDF artwork remains vector content.
func (d *Document) PrintPDF(ctx context.Context, pages []int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		pages = make([]int, d.PageCount())
		for i := range pages {
			pages[i] = i
		}
	}
	seen := make(map[int]bool, len(pages))
	for _, page := range pages {
		if d.validatePage(page) != nil || seen[page] {
			return nil, fmt.Errorf("document: invalid print page selection")
		}
		seen[page] = true
	}
	if d.Format() != FormatPDF {
		p, err := d.RenderPage(0)
		if err != nil {
			return nil, err
		}
		// Paper is white even when the source contains transparency.
		opaque := image.NewRGBA(image.Rect(0, 0, p.Bounds().Dx(), p.Bounds().Dy()))
		draw.Draw(opaque, opaque.Bounds(), image.White, image.Point{}, draw.Src)
		draw.Draw(opaque, opaque.Bounds(), p, p.Bounds().Min, draw.Over)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return imagePDF(opaque)
	}
	data, err := d.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	all := len(pages) == d.PageCount()
	for i, page := range pages {
		all = all && i == page
	}
	if all {
		return data, nil
	}
	selection := make([]string, len(pages))
	for i, page := range pages {
		selection[i] = strconv.Itoa(page + 1)
	}
	var out bytes.Buffer
	if err := collectPDFPages(ctx, data, &out, selection); err != nil {
		return nil, err
	}
	return out.Bytes(), ctx.Err()
}

// CombinePrintPDFs joins already-prepared documents without rasterizing their
// pages or returning an original source when preparation/merging fails.
func CombinePrintPDFs(ctx context.Context, parts [][]byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("document: no selected PDF content")
	}
	readers := make([]io.ReadSeeker, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			return nil, fmt.Errorf("document: empty selected PDF content")
		}
		readers = append(readers, bytes.NewReader(part))
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	var output bytes.Buffer
	if err := api.MergeRaw(ctx, readers, &output, false, nil); err != nil {
		return nil, err
	}
	if err := api.Validate(ctx, bytes.NewReader(output.Bytes()), nil, nil); err != nil {
		return nil, err
	}
	return output.Bytes(), ctx.Err()
}

// PDFPageLayout specifies a common physical page size for a newly created PDF.
// Width and height fix the orientation; individual pages cannot swap them.
type PDFPageLayout struct{ WidthMM, HeightMM float64 }

func (layout PDFPageLayout) Validate() error {
	for _, size := range []float64{layout.WidthMM, layout.HeightMM} {
		if math.IsNaN(size) || math.IsInf(size, 0) || size < 10 || size > 2000 {
			return fmt.Errorf("page dimensions must be between 10 and 2000 mm")
		}
	}
	return nil
}

func (layout PDFPageLayout) Points() (width, height float64) {
	return layout.WidthMM * 72 / 25.4, layout.HeightMM * 72 / 25.4
}

// NormalizePrintPDF fits prepared pages onto identical sheets, retaining image
// pixels and native PDF artwork. Only the new copy is changed. Existing page
// orientation is respected; no extra content rotation or cropping is applied.
func NormalizePrintPDF(ctx context.Context, data []byte, layout PDFPageLayout) ([]byte, error) {
	if err := layout.Validate(); err != nil {
		return nil, err
	}
	w, h := layout.Points()
	paper := types.Dim{Width: w, Height: h}
	resize := &model.Resize{PageDim: &paper, UserDim: true, Unit: types.POINTS, EnforceOrient: true, DisableContentRotation: true}
	var output bytes.Buffer
	if err := api.Resize(ctx, bytes.NewReader(data), &output, nil, resize, nil); err != nil {
		return nil, err
	}
	if err := api.Validate(ctx, bytes.NewReader(output.Bytes()), nil, nil); err != nil {
		return nil, err
	}
	return output.Bytes(), ctx.Err()
}
