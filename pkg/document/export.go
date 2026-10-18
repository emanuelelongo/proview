package document

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const DefaultJPEGQuality = 92

type ExportOptions struct {
	Format         Format
	JPEGQuality    int
	JPEGBackground color.Color
}

func (d *Document) Export(options ExportOptions) ([]byte, error) {
	format := options.Format
	if format == "" {
		format = d.Format()
	}

	switch format {
	case FormatPNG, FormatJPEG:
		if d.Format() == FormatPDF {
			return nil, fmt.Errorf("document: cannot export a PDF as %s", format)
		}
		return d.exportImage(format, options)
	case FormatPDF:
		if d.Format() != FormatPDF {
			return nil, fmt.Errorf("document: cannot export an image as PDF")
		}
		return d.exportPDF()
	default:
		return nil, fmt.Errorf("document: unsupported export format %q", format)
	}
}

func (d *Document) exportImage(format Format, options ExportOptions) ([]byte, error) {
	rendered, err := d.RenderPage(0)
	if err != nil {
		return nil, fmt.Errorf("document: render image for export: %w", err)
	}

	var output bytes.Buffer
	switch format {
	case FormatPNG:
		if err := png.Encode(&output, rendered); err != nil {
			return nil, fmt.Errorf("document: encode PNG: %w", err)
		}
	case FormatJPEG:
		background := options.JPEGBackground
		if background == nil {
			background = color.White
		}
		opaque := image.NewRGBA(image.Rect(0, 0, rendered.Bounds().Dx(), rendered.Bounds().Dy()))
		draw.Draw(opaque, opaque.Bounds(), &image.Uniform{C: background}, image.Point{}, draw.Src)
		draw.Draw(opaque, opaque.Bounds(), rendered, rendered.Bounds().Min, draw.Over)
		quality := options.JPEGQuality
		if quality < 1 || quality > 100 {
			quality = DefaultJPEGQuality
		}
		if err := jpeg.Encode(&output, opaque, &jpeg.Options{Quality: quality}); err != nil {
			return nil, fmt.Errorf("document: encode JPEG: %w", err)
		}
	}
	return output.Bytes(), nil
}

func (d *Document) exportPDFFlattened() ([]byte, error) {
	original, err := d.basePDF()
	if err != nil {
		return nil, err
	}
	if len(d.signatures) == 0 {
		return d.redactExport(original)
	}
	ctx := context.Background()
	boundaries, err := api.Boxes(ctx, bytes.NewReader(original), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("document: inspect PDF page boxes: %w", err)
	}
	if len(boundaries) != d.PageCount() {
		return nil, fmt.Errorf("document: PDF page box count %d does not match page count %d", len(boundaries), d.PageCount())
	}

	watermarks := make(map[int][]*model.Watermark)
	for _, signature := range d.signatures {
		box := boundaries[signature.Page].CropBox()
		if box == nil || box.Width() <= 0 || box.Height() <= 0 {
			return nil, fmt.Errorf("document: PDF page %d has an invalid visible area", signature.Page+1)
		}
		pageWidth, pageHeight := box.Width(), box.Height()
		if boundaries[signature.Page].Rot%180 != 0 {
			pageWidth, pageHeight = pageHeight, pageWidth
		}

		previewBounds := signature.preview.Bounds()
		if previewBounds.Empty() {
			return nil, fmt.Errorf("document: signature %d has invalid dimensions", signature.ID)
		}
		targetWidth := signature.Rect.Width * pageWidth
		scale := targetWidth / float64(previewBounds.Dx())
		x := signature.Rect.X * pageWidth
		y := (1 - signature.Rect.Y - signature.Rect.Height) * pageHeight
		description := fmt.Sprintf("pos:bl, off:%.8f %.8f, scale:%.10f abs, rot:0", x, y, scale)
		var watermark *model.Watermark
		var err error
		if signature.IsLens() {
			picture, e := d.RenderLens(signature.ID, 300)
			if e != nil {
				return nil, e
			}
			var pngData bytes.Buffer
			if e = png.Encode(&pngData, picture); e != nil {
				return nil, e
			}
			scale = targetWidth / float64(picture.Bounds().Dx())
			description = fmt.Sprintf("pos:bl, off:%.8f %.8f, scale:%.10f abs, rot:0", x, y, scale)
			watermark, err = api.ImageWatermarkForReader(ctx, bytes.NewReader(pngData.Bytes()), description, true, false, types.POINTS)
		} else if signature.IsVector() {
			vectorPDF, vectorErr := signature.ArtworkAsset(targetWidth / (signature.Rect.Height * pageHeight))
			if vectorErr != nil {
				return nil, fmt.Errorf("document: convert SVG signature: %w", vectorErr)
			}
			boxes, vectorErr := api.Boxes(ctx, bytes.NewReader(vectorPDF), nil, nil)
			if vectorErr != nil || len(boxes) != 1 {
				return nil, fmt.Errorf("document: inspect vector signature: %v", vectorErr)
			}
			scale = targetWidth / boxes[0].CropBox().Width()
			description = fmt.Sprintf("pos:bl, off:%.8f %.8f, scale:%.10f abs, rot:0", x, y, scale)
			watermark, err = api.PDFWatermarkForReadSeeker(ctx, bytes.NewReader(vectorPDF), 1, description, true, false, types.POINTS)
		} else {
			var signaturePNG bytes.Buffer
			if err := png.Encode(&signaturePNG, signature.Preview()); err != nil {
				return nil, fmt.Errorf("document: encode signature %d: %w", signature.ID, err)
			}
			watermark, err = api.ImageWatermarkForReader(ctx, bytes.NewReader(signaturePNG.Bytes()), description, true, false, types.POINTS)
		}
		if err != nil {
			return nil, fmt.Errorf("document: prepare signature %d for PDF page %d: %w", signature.ID, signature.Page+1, err)
		}
		pageNumber := signature.Page + 1
		watermarks[pageNumber] = append(watermarks[pageNumber], watermark)
	}

	var output bytes.Buffer
	configuration := model.NewDefaultConfiguration()
	if err := api.AddWatermarksSliceMap(ctx, bytes.NewReader(original), &output, watermarks, configuration); err != nil {
		return nil, fmt.Errorf("document: apply PDF signatures (the PDF may be encrypted or not editable): %w", err)
	}
	if err := api.Validate(ctx, bytes.NewReader(output.Bytes()), model.NewDefaultConfiguration(), nil); err != nil {
		return nil, fmt.Errorf("document: validate exported PDF: %w", err)
	}
	return d.redactExport(output.Bytes())
}
