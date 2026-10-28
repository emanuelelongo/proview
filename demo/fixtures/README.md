# Demo inputs

These are ready-to-use files committed in the repository. Keep them as-is when
recording demo media. If a fixture needs a change, an agent can edit or replace
that file directly, inspect the result and update its SHA-256 in `manifest.json`.
The recorder checks those hashes to detect accidental input changes.
Never add real customer files, credentials, signatures or personal information.

| File | Features and intended tests |
| --- | --- |
| `showcase.pdf` | Six distinct pages; native editable Helvetica text, multiple search matches, vector geometry, QR image, interactive AcroForm controls, landscape page, blank annotation/signature space, image-only scan for OCR |
| `merge.pdf` | One distinct extra page for append, collection and ordering tests |
| `crop.jpg` | 1280 × 840 synthetic landscape for crop, rotate, draw, shapes and text |
| `scan.png` | 1600 × 1100 high-contrast English text and QR for OCR, search, selection and highlight |
| `johndoe.png` | User-supplied John Doe example for acquisition/vectorization and signing demos |
| `lenna.png` | Standard Lenna portrait for the crop demonstration |
| `transparent.png` | Overlapping translucent shapes on transparent pixels for alpha/export checks |
| `qr.png` | QR code containing only `https://example.com/proview-demo` |
| `model.stl` | User-supplied Desktop model for orbit, wireframe and frame-crop demonstrations |
| `reading.md` | Short local reading sample with a Mermaid flowchart, table, Go code and headings for the floating Contents panel |

PDF pages: 01 text/annotation, 02 search/redaction/QR, 03 forms, 04 landscape
vector chart, 05 signature space, 06 image-only scan. Page 06 intentionally has
**no native text**, allowing OCR fallback. Fields cover required/max-length,
comb, multiline, password, choice controls, checkbox, radio, multi/single
list, reset and read-only behavior. This is representative demonstration material,
not exhaustive PDF compatibility/security coverage; retain the engine test corpus.

Except for the supplied STL/John Doe files and Lenna, the drawings, wording and geometry were generated for this
repository and follow its license. Raster text uses the bundled Go fonts
(`golang.org/x/image/font/gofont`, Go's font license); PDF native text references
standard Helvetica. The QR encoder is the existing gozxing dependency. The supplied John Doe example and Lenna are documented below; no private
document is embedded.

`model.stl` is the original user-supplied Desktop model, stored directly in this
folder. No separate source copy or Desktop dependency is needed.

`johndoe.png` is the John Doe example signature supplied by the user from the
Desktop; its original bytes are kept here. It replaces the earlier generated
Alex Example artwork in signature demos.

`lenna.png` is the standard 512 × 512 Lenna image, used by the crop case. Source:
[Wikipedia's file record](https://en.wikipedia.org/wiki/File:Lenna_(test_image).png),
[original PNG](https://upload.wikimedia.org/wikipedia/en/7/7d/Lenna_%28test_image%29.png).
Original photograph: Dwight Hooker / Playboy (1972). This fixture retains its
original copyright; it is not relicensed under the repository's AGPL license.
