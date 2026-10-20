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

In Select, drag starting on recognized OCR text to select individual characters
and text across words and lines on that page. Double-click selects a whole word.
Words without valid character coordinates retain whole-word selection. Only
selected characters or words are highlighted; there is no recognition button,
progress overlay or permanent box around detected text.
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
  hOCR output through `-c tessedit_create_hocr=1 -c hocr_char_boxes=1`. Character
  coordinates come from the same recognition pass, without an extra subprocess
  or recognition request during dragging/search. PNG input and hOCR output use
  pipes; document pixels and recognized text are never written to temporary files
  by Proview.
- Predominantly dark inputs are converted to light backgrounds before deskewing
  and recognition. A bounded pixel sample must be at least 75% dark (luminance
  below 128); transparency is interpreted over white. Only the transient OCR
  raster changes, with unchanged dimensions and word coordinates. This heuristic
  helps dark terminal/editor screenshots; it does not segment mixed light/dark
  regions. See [Tesseract's input-quality guidance](https://tesseract-ocr.github.io/tessdoc/ImproveQuality.html).
- A conservative Otsu retry (`thresholding_method=0`) runs only when at least
  80% of sampled source pixels are dark (grayscale below 96) and the primary
  result is not already strong (mean confidence at least 90 and at least 90% of words with
  confidence 80 or above). Light scans and strong primary results keep one pass.
  Eligibility is checked before polarity normalization and deskewing.
  The retry uses the same normalized, encoded pixels, language models,
  segmentation and deskew geometry, with a three-second subprocess deadline. It is skipped when
  fewer than four seconds remain in the parent job. Retry errors/timeouts keep
  the primary result; document cancellation still discards the entire job.
- Supplemental words require confidence at least 85, a letter or digit and no
  bounding-box overlap with any primary or previously accepted supplemental word.
  Existing words retain text, geometry, confidence, line/block IDs and relative
  reading order. Nearby supplements join original lines; new lines get fresh IDs.
  Pages exceeding 4096 words in either pass retain the primary result to bound
  pairwise geometry work. This is deliberately conservative: incorrect primary
  words are not corrected, and some missing words can remain unavailable.
  Confidence is heuristic; supplemental words can still be wrong.
- Both passes finish in the existing background job before publishing one layout.
  Selection can become available slightly later; its word indexes never change
  underneath an active gesture. Zoom/scroll reuse the combined cached result.
- PDF source text (including an existing OCR layer) takes precedence. A page with
  any selectable native words skips OCR. Consequently mixed pages currently expose
  only native text; recognition and merging of their raster regions is deferred.
- Camera JPEG orientation is normalized from EXIF when loading the source, including
  mirrored orientations. Image OCR uses the cropped, untransformed source; the word geometry then follows
  editor rotations and flips. This avoids attempting OCR on text mirrored by an
  editor transform. Native PDF rotation is handled by MuPDF; editor rotation is
  mapped to the recognized geometry. Automatic deskewing maps word and character
  boxes back to inclined quadrilaterals on the source.
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
in ignored `build/ocr-corpus`. Fixtures are generated from the bundled Go font,
including a dark screenshot with small text; no private documents are used.
Accuracy is `max(0, 1 - word edit distance / expected
word count)`, case- and punctuation-sensitive and independent of whitespace.
This also penalizes incorrect reading order. The photograph case is a synthetic
4-degree slant plus illumination gradient, **not a real photograph**.

Timings include rendering, preprocessing, encoding and OCR, after the engine setup
check. They exclude GUI event scheduling and initial document loading. Go allocation
and heap measurements exclude MuPDF native memory and the Tesseract subprocess,
and are not peak process/RSS measurements. Use OS process monitoring for total
memory when evaluating large real documents. Results do not measure GUI latency.

Run `task check` for unit/regression tests. The ordinary suite does not require
Tesseract. The probe requires it and performs real recognition. Malformed hOCR,
missing/invalid character coordinates with word fallback, a missing language, timeout, canceled/replaced document, image transformations,
clipboard precedence, cache reuse/eviction, sanitized native text, shared-image
redaction propagation and failure without an unsafe fallback are covered
by focused tests. Retry tests cover the light/strong-result bypass, immutable
primary words, uncovered-region insertion and ordering, duplicate/noise rejection,
empty-primary recovery, coordinate mismatch, errors, deadlines and cancellation.
A subprocess regression test verifies that dark-input normalization preserves retry
eligibility and both passes retain character coordinates.

Manual desktop acceptance checklist (repeat on each target platform):

1. Open each generated fixture with `task run`; select forwards/backwards,
   drag inside words in both directions, double-click, copy and paste into an
   external text editor. Search for a substring and compare its character highlight.
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

On 2026-10-20, a local dark terminal screenshot tested through the document
recognition path improved from 48 to 130 detected words after polarity
normalization. Words with confidence at least 80 increased from 18 to 109;
mean confidence increased from 60.9 to 88.0, with runtime about 0.5 seconds.
These are detection/confidence measurements, not transcription accuracy against
a reference. The synthetic dark screenshot scored 100% word accuracy; existing
corpus accuracy was unchanged, including the known mixed-PDF limitation.
With character geometry enabled, the same screenshot still yields 130 words in
about 0.5 seconds, and corpus transcription accuracy is unchanged. Search cache
accounting includes character allocations within its existing 16 MiB budget. A
12,000-character synthetic selection benchmark measured about 0.7 ms to prepare
selection units and 0.45 ms per worst-case nearest-character lookup, with no
lookup allocations (Linux amd64, Intel i7-13700H). These are local measurements,
not a cross-platform GUI latency guarantee. Both changes are not yet released.
The local screenshot was not committed.

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


## Dark screenshot retry verification

On 2026-10-24, two user-provided desktop PNGs were evaluated locally with the
bundled macOS ARM64 runtime and the same document recognition path. The mixed
code/diagram screenshot increased from 25 recognized words to 36, retaining all
primary words; the already strong log screenshot stayed at 204 words with mean
confidence 92.2. Counts and confidence are not measured transcription accuracy.
The private files were not added to the repository.

The generated eight-case OCR corpus was also rerun: clean screenshot, A4 scan,
two-column image and scanned PDF retained 100% word accuracy, and blank input
remained empty. Mixed native/raster PDF behavior retains the documented native
text-only limitation. Native desktop selection gestures and other platforms
still require the acceptance checks above.
