// Package qrscan decodes QR payloads and their selectable source geometry.
package qrscan

import (
	"context"
	"image"
	"math"

	"emanuelelongo/proview/internal/textselect"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/multi/qrcode/detector"
	qrdecoder "github.com/makiuchi-d/gozxing/qrcode/decoder"
	qrdetector "github.com/makiuchi-d/gozxing/qrcode/detector"
)

// Scan expects a bounded, opaque raster. Coordinates are normalized so that
// zooming the viewer does not require recognition again.
func Scan(ctx context.Context, src image.Image) (textselect.Layout, error) {
	if err := ctx.Err(); err != nil {
		return textselect.Layout{}, err
	}
	bitmap, err := gozxing.NewBinaryBitmapFromImage(src)
	if err != nil {
		return textselect.Layout{}, err
	}
	matrix, err := bitmap.GetBlackMatrix()
	if err != nil {
		return textselect.Layout{}, err
	}
	if err := ctx.Err(); err != nil {
		return textselect.Layout{}, err
	}
	layout := textselect.Layout{Width: float64(src.Bounds().Dx()), Height: float64(src.Bounds().Dy())}
	hints := map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_TRY_HARDER: true}
	results, err := detector.NewMultiDetector(matrix).DetectMulti(hints)
	if err != nil {
		if _, absent := err.(gozxing.ReaderException); absent {
			return layout, ctx.Err()
		}
		return layout, err
	}
	decoder := qrdecoder.NewDecoder()
	for _, result := range results {
		if err := ctx.Err(); err != nil {
			return textselect.Layout{}, err
		}
		decoded, err := decoder.Decode(result.GetBits(), hints)
		if err != nil || decoded.GetText() == "" {
			continue
		}
		points := result.GetPoints()
		if len(points) < 3 {
			continue
		}
		var alignment *qrdetector.AlignmentPattern
		if len(points) > 3 {
			alignment = qrdetector.NewAlignmentPattern(points[3].GetX(), points[3].GetY(), 1)
		}
		dim := float64(result.GetBits().GetWidth())
		transform := qrdetector.Detector_createTransform(points[1], points[2], points[0], alignment, int(dim))
		corners := []float64{0, 0, dim, 0, dim, dim, 0, dim}
		transform.TransformPoints(corners)
		var quad textselect.Quad
		valid := true
		for i := range quad {
			x, y := corners[2*i]/layout.Width, corners[2*i+1]/layout.Height
			if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
				valid = false
			}
			quad[i] = textselect.Point{X: x, Y: y}
		}
		if valid {
			layout.Words = append(layout.Words, textselect.Word{Text: decoded.GetText(), Quad: quad, Confidence: 100, Line: len(layout.Words)})
		}
	}
	return layout, ctx.Err()
}
