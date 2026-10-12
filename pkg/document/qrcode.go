package document

import (
	"context"
	"image"
	"math"

	"emanuelelongo/proview/internal/qrscan"
	"emanuelelongo/proview/internal/textselect"
	img "emanuelelongo/proview/pkg/images"
)

// SelectableQRCodes recognizes the visible source on a rendering snapshot.
// Recognition is independent of OCR and never modifies document history.
func (d *Document) SelectableQRCodes(ctx context.Context, page int) (textselect.Layout, error) {
	if err := ctx.Err(); err != nil {
		return textselect.Layout{}, err
	}
	if err := d.validatePage(page); err != nil {
		return textselect.Layout{}, err
	}
	var raster image.Image
	var err error
	if d.Format() == FormatPDF {
		size, e := d.PageSize(page)
		if e != nil {
			return textselect.Layout{}, e
		}
		dpi := math.Min(300, 144*float64(ocrMaxDimension)/float64(max(size.X, size.Y)))
		if d.HasRedactions() {
			// Real sanitation is mandatory: merely painting a band could still
			// expose hidden QR data through error correction or shared images.
			safe := d.RenderingSnapshot()
			safe.signatures = nil
			raster, err = safe.SafeRenderPage(page, dpi)
		} else {
			raster, err = d.basePageDPI(page, dpi)
			if err == nil {
				raster = (img.Transform{Turns: d.pages[page].turns}).Apply(raster)
			}
		}
	} else {
		raster, err = d.RenderBasePage(page)
	}
	if err != nil {
		return textselect.Layout{}, err
	}
	if err := ctx.Err(); err != nil {
		return textselect.Layout{}, err
	}
	return qrscan.Scan(ctx, ocrRaster(raster))
}
