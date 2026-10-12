# Automatic text selection prototype

Automatic text selection uses Tesseract 5 on macOS, Windows and Linux. Recognition
runs locally in a separate subprocess, automatically when an image or PDF page
becomes visible. No Apple APIs or online recognition service are used.

## Run

Normal Task builds and release packages now bundle the OCR runtime and models,
with recognition enabled automatically. Use `task run -- <file>`; no installed
Tesseract is required. See [packaging](ocr-packaging.md) for build prerequisites,
embedded assets and the private runtime cache.

For a separate system-engine evaluation, install Tesseract 5, then run
`task ocr:probe`. It prepares SHA-256-verified English/Italian `tessdata_fast`
models under `build/ocr/tessdata`, pinned to commit
`87416418657359cb625c412a48b6e1d6d41c29bd`.

Direct unbundled Go builds still require `PROVIEW_OCR=1` to enable recognition,
`TESSDATA_PREFIX` pointing to these models and Tesseract on PATH. The optional
`PROVIEW_TESSERACT_PATH` overrides the engine in development builds. Set
`PROVIEW_OCR=0` to disable recognition in any build. Restart after changing the
engine configuration. The Redact tool remains independent of text selection.

## Interaction

In Select, drag starting on a recognized word to select words and lines on that
page. Double-click selects a word. Only selected words are highlighted; there is
no recognition button, progress overlay or permanent box around detected text.
The cursor changes over recognized text. The bottom bar shows the selected text
without a prefix and pauses tips until deselection. If the entire selected text
is a web address, click it in the bottom bar to open it in the default browser
(`http://`, `https://`, and bare domains such as `www.example.com` are supported).
Right-click the selected words or the bottom-bar text for **Copy**. Cmd+C on macOS / Ctrl+C elsewhere copies
Unicode text; spaces, line boundaries and OCR block boundaries are retained.

Drag starting on the background selects a PNG area. Alt+drag forces area selection
even over text or objects. Added objects keep their normal selection/edit behavior,
and focused text entries keep their clipboard shortcuts. Escape, a background
click, page/tool changes and replacement of the document clear text selection.
Zoom preserves its geometry. A gesture started before OCR completes stays an area
gesture until release. There is no cross-page text selection or edge autoscroll.

## Implementation and limits

- Tesseract LSTM (`--oem 1`), `ita+eng`, automatic page segmentation (`--psm 3`),
  adaptive Sauvola thresholding (`thresholding_method=2`), and
  TSV output through `-c tessedit_create_tsv=1`. This does not require the optional
  installed `tsv` configuration file. PNG input and TSV output use pipes; document
  pixels and recognized text are never written to temporary files by Proview.
- PDF source text (including an existing OCR layer) takes precedence. A page with
  any selectable native words skips OCR. Consequently mixed pages currently expose
  only native text; recognition and merging of their raster regions is deferred.
- Camera JPEG orientation is normalized from EXIF when loading the source, including
  mirrored orientations. Image OCR uses the cropped, untransformed source; the word geometry then follows
  editor rotations and flips. This avoids attempting OCR on text mirrored by an
  editor transform. Native PDF rotation is handled by MuPDF; editor rotation is
  mapped to the recognized geometry. Automatic deskewing maps TSV boxes back to inclined quadrilaterals on the source.
  Perspective and page curvature are not corrected.
- PDFs are rendered at up to 300 DPI, with their longest side capped at 2400
  pixels. Image OCR uses the same 2400-pixel cap, preserving aspect ratio and never
  upscaling small images. Only the recognition input is downsampled; previews and
  exports retain source resolution. Transparency is flattened on white.
  User-added objects are outside recognition.
- One cancellable job per editor, active page first, 30-second deadline, and an
  in-memory LRU cache of 20 pages. At most 20 visible/requested pages are analyzed
  at once. OCR never sets the editor's annotation-busy state. Native extraction
  and rendering cannot be interrupted mid-call; canceled results are discarded
  before OCR or UI publication. The Tesseract subprocess itself is terminated.
- Zoom/scroll reuse results; source geometry edits invalidate affected entries.
  Redactions invalidate all pages, and recognition/extraction uses a separately
  sanitized PDF, including shared-image removal. Failure never falls back to
  uncensored text. OCR does not change source bytes, Undo or exported files.
- One setup warning per application session; per-page failures/timeouts are logged
  and cached as unavailable until invalidation, eviction or reopening. Ordinary
  logs report counts and timings, not recognized text.

## Reproduce the evaluation

`task ocr:probe` creates PNG/PDF fixtures, expected/actual text and `report.json`
in ignored `build/ocr-corpus`. Fixtures are generated from the bundled Go font;
no private documents are used. Accuracy is `max(0, 1 - word edit distance / expected
word count)`, case- and punctuation-sensitive and independent of whitespace.
This also penalizes incorrect reading order. The photograph case is a synthetic
4-degree slant plus illumination gradient, **not a real photograph**.

Timings include rendering, preprocessing, encoding and OCR, after the engine setup
check. They exclude GUI event scheduling and initial document loading. Go allocation
and heap measurements exclude MuPDF native memory and the Tesseract subprocess,
and are not peak process/RSS measurements. Use OS process monitoring for total
memory when evaluating large real documents. Results do not measure GUI latency.

Run `task check` for unit/regression tests. The ordinary suite does not require
Tesseract. The probe requires it and performs real recognition. A malformed TSV,
a missing language, timeout, canceled/replaced document, image transformations,
clipboard precedence, cache reuse/eviction, sanitized native text, shared-image
redaction propagation and failure without an unsafe fallback are covered
by focused tests.

Manual desktop acceptance checklist (repeat on each target platform):

1. Open each generated fixture with `task run`; select forwards/backwards,
   double-click, copy and paste into an external text editor.
2. Compare untouched image appearance, hover cursor and selection alignment at
   fit, 100% and enlarged zoom; rotate/flip/crop and repeat.
3. Drag before OCR finishes, Alt-drag over text/objects, edit an annotation entry,
   switch pages, Undo/Redo, replace documents and close during recognition.
4. Redact a native word and a scanned region, including a shared-image reuse;
   ensure copying cannot expose removed content, including during hover reveal.
5. Try real photographs, small text, low contrast and multipage documents; record
   first-selection time and total memory for both Proview and Tesseract.

See [the initial evaluation](ocr-prototype-results.md) for observed results and
which platform/runtime checks have actually been performed.

To measure a local image or PDF without adding it to the generated corpus:

```sh
TESSDATA_PREFIX="$PWD/build/ocr/tessdata" go run ./cmd/ocrprobe -file /path/to/image.jpeg
```

This prints word counts, confidence and timings only. Confidence is the engine's
estimate, not measured transcription accuracy. Local source files are not copied
and recognized text is not saved by this command.

## Automatic text rotation

Tesseract input is automatically deskewed when contrast-edge projections show a
clear dominant text direction. The detector searches corrections from -40 to
+40 degrees on an analysis image with an 800-pixel long side, then refines the
angle. Weak evidence and corrections below 0.75 degrees retain the original
input. This is a heuristic for printed text, not a guarantee on arbitrary photos.

Rotation is confined to the transient recognition input. Its white canvas expands
to contain all four image corners, with padding; no source pixels are cropped.
Recognized word quadrilaterals are mapped through the inverse rotation into the
original image coordinates, preserving selection geometry and copy order. The
viewer, source bytes, edits and exports are unchanged. No perspective or page
curvature correction is performed. Ordinary tests cover both rotation directions,
aligned/blank input, cancellation, corner preservation and inverse selection
geometry without requiring an installed OCR engine.
