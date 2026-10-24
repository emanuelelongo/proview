package document

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"strconv"
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
