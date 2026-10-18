# Standard editable PDF annotations

Implemented on 2026-10-18; Preview–Proview compatibility accepted by the user. No release or
packaging is implied. Added text, drawing, highlights and shapes remain selectable
and editable after saving a PDF and reopening it. Existing PDF page content stays
vector/searchable. Image exports remain flattened images.

## File representation

Only standard PDF annotations and appearance streams are written. There is no
Proview-specific editor payload, embedded JSON or sidecar.

| Editor object | PDF subtype |
| --- | --- |
| Added text | FreeText, with Contents, default/rich styling and embedded font |
| Freehand drawing | Ink |
| Text-following highlight | Highlight, with QuadPoints and Multiply blending |
| Rectangle / oval | Square / Circle |
| Line / arrow | Line, with OpenArrow or imported ClosedArrow endings |
| Star / callout | Polygon |

Appearances preserve the visual result, including vector geometry and text glyph
outlines. Standard fields retain editable text, geometry and styling. All twelve
bundled font faces are embedded for text regeneration, including OpenType/CFF.
Other editors can substitute their own fonts when editing: PDFKit, for example,
reports Helvetica for the editable font while displaying the original appearance.

Simple single-path vector strokes stored in Stamp annotations (as Preview can
produce) are recognized from standard PDF drawing operators and imported as Ink
objects. Original appearances are retained until editing. Single closed polygon paths, including Preview stars, also become editable
polygons when their fill and stroke geometry/colors agree. Complex, curved-fill,
image and text stamps remain static. No Apple-specific annotation payload is required.

Signatures remain unselectable in Proview after reopening. Signature-only exports
keep the existing baked-page representation. In documents mixing signatures and
editable annotations, signatures, inserted artwork and magnifiers become standard
read-only, locked Stamp annotations. This preserves their order relative to text
and shapes. The standard markup Subject `Signature` identifies signature stamps so even a
signature made from a single stroked path stays static. These flags and subject
are editing hints, not tamper protection or certificate signatures. Magnifiers reopen as static artwork.

## Reopening and limitations

Supported annotations become ordinary editor objects with movement, properties,
copy/paste, deletion and Undo. Highlights remain anchored to text: selection and color editing are allowed, but
movement and resizing are disabled. Stars and
callouts reopen as generic polygons: their vertices remain editable through
movement, resizing and color, but their parametric star/callout identity is lost.

This is not a general annotation editor. Import normally requires a usable appearance and
geometry/style the editor can represent. Standard Line objects with missing or
empty appearances can regenerate from their endpoint/style fields. Unsupported annotations remain in the
source PDF, visible and preserved on export. Examples include multiline text,
FreeText callouts/backgrounds/justified layouts, multipath Ink, other unsupported line
endings, hidden/read-only markup and unrecognized subtypes. Foreign fonts map to
the bundled families when text is edited; rich text with mixed inline styles is
not reproduced. Imported appearances remain exact until an editable property
regenerates them. Import is bounded to 1024 objects and 64 million preview pixels;
remaining objects are preserved in the source.

Applied redactions use the composed, baked and sanitized export path. This removes
editable annotation data along with other metadata; a redacted export deliberately
does not retain annotation editing. Native PDF text editing remains a separate
operation on original page text. Fillable fields remain editable alongside markup.

## Implementation

- `pkg/document/pdf_annotations.go`: standard dictionaries, export and import.
- `pdf_annotation_objects.go`: resource graph copying, page box/rotation mappings,
  isolated appearances, affine transforms and validated writing. Always validate
  contexts before writing: otherwise native CID font resources can be lost.
- `pdf_annotation_stroke.go`: bounded recognition of a single stroked path in
  standard appearance operators, plus matching filled/stroked polygons; rejects
  curved signature artwork, mixed colors, mismatched geometry and complex clips.
- `pdf_annotation_fonts.go`: bundled TrueType/OpenType font embedding and maps.
- `Source` keeps exact original bytes and a separate immutable base with only
  successfully prepared annotations detached. Document/page operations retain
  the original source owners, including appended collections.
- `Overlay.pdfAppearance` and `pdfMarkup` are immutable, shared runtime caches of
  standard appearance bytes and markup fields. Property edits replace appearance
  data; snapshots/history never mutate cached payloads. Successive saves unwrap
  isolated forms and reuse fonts to avoid accumulating nested resources.
- GUI stamp widgets reject selection, dragging and resizing. Unsupported markup
  is rendered through the native PDF renderer, which also handles form widgets.

## Verification

Tests cover save/reopen/edit/delete/Undo, all four page rotations and offset crop
boxes, text rotation/mirroring, vector clipboard roundtrips, locked signature
ordering, standard author/subject/identifier retention, unsupported annotations,
append/duplicate/rotate, repeated saves with bounded size, all twelve embedded
font faces, native text editing, secure redaction and editable forms. Headless GUI
tests cover reopened text selection, highlights without resize and locked stamps.
Additional tests cover existing-text inline edit/cancel/delete/save failures,
one-step Undo, closed-arrow repeated saves, missing appearances, stamp-to-Ink
editing and conservative rejection of unsupported stamp content. The Desktop
`test.pdf` was also imported and its red stroke edited/exported/reopened in memory,
without modifying the original file.

Apple PDFKit recognizes all nine fixture objects as standard annotations. Its
rendered thumbnail has been visually inspected; changing text/font/color/position
with PDFKit, saving and reimporting passes appearance and editability checks at
all four rotations. Acrobat has not been tested manually. The user accepted the Preview–Proview
compatibility tests for the upcoming release on 2026-10-18. The static magnifier
after reopening is an accepted limitation; no private Apple metadata support is planned.

Generate synthetic PDFs for review (ignored build artifacts):

```sh
PROVIEW_ANNOTATION_PROBE_DIR="$PWD/build/annotation-probe" go test ./pkg/document -run TestStandardAnnotationInteroperabilityFixture -count=1
```

If that directory also contains `pdfkit-0.pdf`, `pdfkit-90.pdf`, etc. produced by
PDFKit with text `Changed with PDFKit`, font size 20 and red text, the same opt-in
test checks their reimport and repeated export.

## Interaction refinements

Object selection starts on mouse-down rather than waiting for the double-click
interval; double-click still opens text editing. The inline entry retains a
nonzero caret width, keeps its previous artwork while the coalesced replacement
is prepared, and preserves the writing baseline as glyph heights change.
Read-only unsigned signature widgets with empty appearances do not gain MuPDF's
orange SIGN prompt. Preview rendering preserves actual signature artwork; this
adjustment is confined to the disposable render document, never the saved PDF.
Regression tests cover immediate selection, caret/baseline stability, polygon
stamp editing through save/reopen, empty signature markers and real appearances.

New highlights use full color with standard Multiply blending in their PDF
appearance. Raster exports and the editor use the same blend calculation over
preceding objects, keeping black text black. Imported opacity remains intact;
there is no fixed 30% rule or user-facing opacity control. GUI highlight fragments
are computed in one page pass in the coalesced preview worker, never on selection
or input callbacks. Layer rebuilds retain the last completed fragments until the
replacement is ready, avoiding flicker when another highlight is added. Cached
fragments are pruned on removal and cleared when the document/page changes. PDF page content remains vector/searchable, including when
redaction requires the flattened export path.
