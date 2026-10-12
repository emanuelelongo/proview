# Raster signature vectorization prototype

Branch: `feature/raster-signature-vectorization`.

## User flow

Open the signature library and choose **New signature → Import from file → OK**, then select
an SVG, JPEG or PNG. SVG files are used directly; raster images open **Import
signature**, with the original and vector preview in equally sized panels.
The selection rectangle is always visible: draw, move or resize it to update the
preview automatically, without confirming or replacing the displayed original.
**Sensitivity** adjusts ink recognition with stronger filtering at the low end
and support for very faint ink at the high end. Its midpoint keeps the original
threshold. Contour smoothing always uses the maximum setting. The preview updates
in a cancellable background worker. Save becomes available only for the current
successful preview, outside an unfinished selection. Cancel and Save share a row.

Saving creates a normal SVG signature asset with a transparent background. It
supports the existing library/sync, recoloring, insertion and native vector PDF
export. The image source is immutable during acquisition and is not embedded in
the SVG. Closing the editor cancels outstanding work; stale results are ignored.
Previously imported PNG/SVG signatures keep their existing behavior.

## Implementation and scope

The first implementation uses Go only. It shares JPEG EXIF orientation handling
and the existing signature cubic Bézier fitter. No native helper, model or tracing
library is installed or packaged. Mature external tracing engines remain possible
comparison candidates if real scans reveal limitations.

Inputs are bounded to 16 MB and 16 million pixels. A selected region is scaled to
at most 1600 pixels on its long side; the source display is capped at 1200 pixels.
PNG alpha is composited onto white before recognition. A separable sliding maximum
estimates local light paper, then normalized contrast separates ink. Only isolated
single pixels are discarded so dots and short strokes are retained.

Boundary extraction preserves closed contour orientation and holes. Diagonally
touching marks remain separate. Smoothing uses a small circular contour filter
followed by the existing cubic fitter, in bounded chunks with cancellation checks.
Zero smoothing keeps polygon outlines. A 200,000-edge limit rejects excessively
noisy regions and asks for a tighter crop or lower sensitivity. Output is one black
SVG path with a tight viewport and padding, suitable for the existing strict SVG
parser. Color can be changed using existing signature controls after import.

This estimates visible ink contours. It cannot recover pen order/pressure or
missing detail in blur. It does not reconstruct mouse-editable drawing strokes,
correct perspective or remove printed text/ruled lines automatically. Strong
shadows, densely textured paper and very pale ink need further evaluation.

## Local evaluation

```sh
task run

task signature:probe -- -file /path/to/signature.jpeg \
  -crop 100,200,800,300 -intensity 50 -smooth 75 \
  -out build/signature-prototype/my-signature
```

The crop is `x,y,width,height` in the orientation-normalized original. Omitting
it traces the complete image. The command writes SVG, a transparent PNG preview,
a white-background preview, source crop and vector PDF, plus timing/size JSON.
Generated assets stay under ignored `build`; do not commit private signatures.

## Initial verification

Synthetic tests cover transparent paper, closed loops, disconnected dots,
one-pixel diagonals, shading/blue ink, cropped source isolation, cancellation,
invalid inputs, JPEG/PNG decoding and vector PDF generation. The GUI geometry
tests cover aspect preservation, live selection mapping, resizing the editor,
and retaining explicit confirmation for ordinary document cropping.

The local Desktop sample `sample-signatures.avif` was decoded to a temporary PNG
using macOS `sips` for evaluation. Raster signature acquisition accepts JPEG/PNG; SVG import bypasses tracing. Twelve manually selected example signatures produced valid SVG, PNG and PDF
outputs: SVG sizes 4,921–8,554 bytes, conversion up to 6 ms on the current machine
(excluding preview rendering and file writes). The input is a clean 740×740 image
of example signatures, not evidence of accuracy on photographed or scanned paper.
The first result was visually inspected at enlarged size for holes and smooth
outlines. Real scans remain the next quality check.
