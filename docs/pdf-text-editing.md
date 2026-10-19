# Native PDF text editing prototype

Integrated into `develop`; diagnostic messages updated 2026-10-17. The native experiment is now a
constrained application prototype; there is no `pdftextprobe` build tag.
No new OCR, paragraph reflow, release or packaging.

## Accepted interface and implemented behavior

- [x] Separate Tabler `file-type-pdf` toolbar button, next to form filling;
      Tools > Edit PDF Text, Ctrl/Command+Shift+E. Enabled for PDFs without
      pending redactions. Form filling and correction are mutually exclusive.
- [x] Click a native word to open the compact Edit PDF Text dialog with its
      value, Apply/Cancel and compact Move (`arrows-move`) and Duplicate
      (`copy-plus`) icons beside the entry, with passive Move / Duplicate hover
      tooltips inside the dialog. No separate
      Delete button, descriptive paragraph or numeric
      position controls. Selectable geometry excludes widget/annotation appearances.
- [x] Clearing the value and applying removes the word as one Undo operation.
      Move closes the
      dialog and arms dragging on the selected word. The highlight follows the
      mouse; release applies the native edit once, in page units independent of
      view zoom. Pending text changes travel with the move in that same operation.
      Move stays active after each drop so the position can be refined with another
      drag; every drop is a separate Undo step. Escape cancels movement
      (including queued native work), then exits the tool
      on a second press. Errors restore the starting highlight and allow retry.
- [x] Duplicate closes the dialog and immediately arms dragging from the source
      word. It copies the dialog value (including pending text edits) while
      retaining the original word. No document/history change occurs until a
      valid drop; Escape cancels, including queued preparation. The first drop
      creates one native searchable copy and one Undo step. Subsequent drags
      refine that copy through normal Move rather than creating more copies.
      Empty/whitespace-only duplication is disabled. Alignment references include
      the original word while duplicating.
- [x] Cyan alignment guides during movement: the baseline and left edge snap
      to other words on the same page. Hold Shift to disable snapping while
      retaining guides; pressing/releasing Shift also updates a stationary drag.
      Acquisition/release tolerances are 6/10 Fyne display units, independent of
      zoom, with hysteresis to prevent jitter. Selected text is excluded from
      references; horizontal text uses native baseline origins, including fonts
      of different sizes. Guides disappear on release/cancellation. The moving
      preview remains a rectangle; the native text updates after each drop.
- [x] Apply replaces the actual source glyphs and writes real searchable PDF
      text. Other content stays fixed. Edits, moves and duplicates that would
      overlap other source text are rejected before committing, with a simple
      user-facing message. Failed attempts leave history unchanged and retain
      the dialog or drag mode for retry. Undo remains available for valid edits.
      Original font/size/color are retained where the supported path permits.
- [x] Preparation runs outside the UI thread. Errors leave the dialog available
      for revision. Cancel/Escape discard pending dialog work; Escape again exits
      the mode. Changed/closed document guards reject stale work.
- [x] One correction is one Undo operation. Original source bytes stay unchanged.
      Save/reopen and subsequent corrections retain searchable text.
- [x] Corrected native bytes become a new immutable source for the current page
      collection. Stable page IDs and separate overlays/redactions are retained;
      snapshots/history keep the old sources. Pending form values are applied
      into the new source before field object numbers change, and remain editable.
- [x] Source changes refresh page previews, thumbnails, recognition and search,
      including Undo/Redo. The selection highlight follows zoom.

## Native implementation and resolved gates

`internal/pdfengine/textedit.{go,c,h}` exposes `CorrectionTextLayout` and
`CorrectText` / `DuplicateText`. These own independent MuPDF contexts and catch all native
exceptions before returning to Go. They use the existing MuPDF 1.24.9 ABI.

The initial experiment appended replacement text to the page, which changed
paint order. **This is now fixed:** a character filter removes selected glyphs
and inserts an isolated text Form XObject immediately after the source `ET`,
inside the existing graphics/clip context and before later page content. New
font resource names are isolated inside that Form. Shared page content streams
are detached; Form XObjects are filtered per instance.

MuPDF can defer a CTM if an entire text object is removed. The filter flushes it
with an empty-path `n` before the still-pending `BT`. A conservative preflight
rejects a live graphics path crossing `BT`, or graphics paths inside text, so
this operation cannot discard source geometry. Per-filter state keeps recursive
Form instances independent. Tests cover whole transformed text objects and
re-editing generated text Forms.

## User-facing failures and diagnostics

Unsupported edits show a simple message explaining that the page's formatting
cannot yet be edited and that the document is unchanged. Detailed engine errors,
including page number and spacing operator/value, are recorded in
[local diagnostic logs](logging.md). Public messages use "edit"/"text editing";
historical internal API/type names retain "Correction" for now.

Custom character (`Tc`) and word (`Tw`) spacing are now supported for the
otherwise eligible horizontal Latin text path, including the double-quote
text-showing operator. The native filter tracks spacing/font size through q/Q,
converts it to visible page units and retains the original positioning of all
unselected glyphs. Replacement glyphs use individual positioned spans to avoid
integer-TJ rounding of fractional spacing. The isolated replacement Form resets
inherited spacing and stores `ProviewTextSpacing` (font size, character and word
spacing) so later edits retain the intended formatting even with emitted CID
fonts. Word spacing follows the source encoding's single-byte code-32 rule.
Spacing wrappers use public processor callbacks and context-local state; they
do not depend on MuPDF's private processor memory layout.

A real-document check on 2026-10-17 reproduced and fixed the previous rejection
of `Tw=-0.036714` on page 2 of a five-page PDF. Three sampled words now accept
edits without displacing unselected glyphs; identical-text replacements render
pixel-identically at 144 DPI. The original file stayed unchanged. No personal
input PDF was copied into the repository.

Mixed effective spacing within one selected range and nonzero spacing inherited
across a source Form invocation remain rejected. MuPDF 1.24.9's Form filtering
starts a fresh text state, so inherited spacing needs separate fidelity work.
Spacing set inside Forms is supported and remains isolated per instance.

## Transparency outside the selected text

Native edits no longer reject an entire page merely because another graphic or
text object uses opacity, a blend mode or a soft mask. The worker tracks the
selected glyphs' effective fill opacity, blend mode and mask through q/Q and
nested Form invocations. Stroke-only opacity does not block ordinary filled text.
Unrelated artwork and source text keep their graphics state and paint order.

Text that itself uses these effects, or belongs to a transparency-group Form,
remains unsupported. Changes that leave transparency active at the source ET
also remain rejected, since replacement insertion occurs there. These failures
return no output and keep the existing simple UI message with engine details in
logs. The checks use bounded in-memory state in the native edit worker; no new
I/O or drag-preview work was added.

Regression tests cover opacity, masks, blending, partial graphics-state updates,
restoration and nested Forms, including refusal of effects on selected text.
Rendering comparisons allow at most one 8-bit channel level for transparency
compositing rounding; other existing exact-render tests remain unchanged.
A local check on 2026-10-18 edited `Vivamus` on page 3 of the user's five-page
PDF: an identical replacement rendered pixel-identically; replacing it with
`Sed` changed only the word's pixels, and all other pages rendered identically.
pdfcpu validated both outputs. The Desktop original stayed unchanged and was
not added to fixtures or the repository.

## Overlap protection

The native edit worker checks the proposed glyph positions against other source
text before writing an output version. Replacement and Move exclude the selected
source range; Duplicate includes it. Checks use glyph ink bounds (falling back to
layout bounds for unsupported obstacle geometry), rather than full line-height
boxes that can intersect in ordinary paragraphs. Coincident glyph origins are
also blocked because they can make later source identification ambiguous. A
0.02 PDF-point tolerance avoids rejecting touching edges due to rounding.

This checks source text, including nested source Forms; it does not treat images,
vector artwork or separate Proview overlays as text obstacles. It does not repair
already ambiguous source content automatically. There is no new I/O or expensive
work during drag preview: validation runs once in the native worker on Apply/drop.

## Restricted movement in tagged PDFs

Added 2026-10-19. Tagged PDFs accept movement of unchanged, otherwise eligible
horizontal text when the selection covers complete page-level `BT`/`ET` objects.
A word may span several such objects, including Quartz PDFs with one object per
glyph. A word embedded in a larger text object remains unsupported. Replacement,
deletion and duplication in tagged PDFs also remain unsupported; attempted edits
show a plain-language message explaining the supported actions.

This is a separate native movement path. A dry filter pass proves unique
source-glyph ownership and complete text objects. The writer then translates the
original text matrices in place, keeping encoded strings, fonts, per-glyph
spacing, marked-content scopes and logical content order. It never appends
replacement text after the source `ET`. The structure tree, ParentTree, ID tree
and alternative strings remain intact. A new stream isolates shared or
array-valued page contents.

The restricted path rejects source Form invocations, graphics-state/CTM changes
inside text objects, malformed text/state nesting, explicit structure stream
references and layout attributes/classes whose geometry would require updating.
Structure traversal is bounded.
Existing conservative font/style/geometry, inline alternative-text, transparency
and overlap checks remain in force. This does not repair source tagging or
provide arbitrary tagged-PDF editing.

`basePDF` reuses any complete source in its natural page order, including a source
created by a native edit. Recollecting those same pages through pdfcpu would drop
their logical structure tree. Page collection/reorder/merge still uses the
existing collection path; this work does not add tag remapping to those operations.

Regression tests cover whole-word and per-glyph objects, fractional spacing and
kerning, transformed CTMs, shared streams, structure/backlink equivalence,
unchanged logical string order, repeated moves, overlap/unsupported rejection,
one-step Undo/Redo, snapshots and editable save/reopen. Zero-distance moves render
pixel-identically; other page content remains fixed.

A local check used three words from the user's one-page Quartz PDF on 2026-10-19.
The complete structure graph (ignoring semantically absent null entries), original
glyph positions and raw reading sequence stayed unchanged after moving the words.
MuPDF and independent Poppler rendering verified zero-distance moves; changed
pixels in other moves stayed within the old/new word regions. Overlapping drops
were rejected. Private artifacts stay under ignored `build/`; the Downloads
original was not modified or added to fixtures. Desktop dragging needs user review.

## Current limits and remaining work

- [ ] Desktop review of actual clicking, focus, Escape, dragging and
      rendering at real display scales. GUI verification so far is headless.
- [ ] Selection is one word per click. Dragging a range, single-character edits,
      and inline typing are not implemented.
- [ ] No font chooser, size/color changes, automatic shrinking or reflow. Missing
      glyphs return an error; no silent substitution is performed by correction.
- [ ] Only uniformly styled horizontal Latin text and Latin/euro replacements.
      Mixed styles, rotated/vertical text, multi-character ligatures, ambiguous
      source-glyph matches and unsupported glyph mappings fail without output.
- [ ] Tagged PDFs support only the unchanged-text movement subset above. The
      page-wide preflight intentionally rejects inline alternative text,
      non-filled/invisible text, Type 3/substituted/synthetic fonts, non-default
      horizontal text scaling. A supported selection on an otherwise unsupported
      page can therefore also be rejected. Transparency is checked on the selected
      text as described above.
- [ ] Insertions are at the end of a source text object. Original kerning is not
      reproduced inside a replacement; neighboring glyph positions are preserved.
      New overlaps are blocked. An existing overlap can still be shortened,
      deleted or moved to a free position when its source glyphs are identifiable.
      Truly ambiguous source glyphs with coincident origins remain rejected,
      including for deletion, to avoid modifying unintended source content.
- [ ] Broader CropBox/UserUnit, complex fonts/shaping, producer and real-document
      compatibility. Synthetic fixtures do not establish arbitrary PDF fidelity.
- [ ] More complex form widgets/actions with text correction and macOS/Windows
      viewer checks. The current integration fixture exercises a text widget.
- [ ] Each correction prepares a complete PDF source version. Versions stay owned
      until document close, including versions evicted from history, because a
      rendering snapshot may still use them. Large documents/many corrections
      need a more economical representation and explicit snapshot lifetimes.
- [ ] Correction is disabled while redactions exist. Recognition/search continue
      using the existing sanitized path; no original redacted content is exposed
      by the correction selector.

## Verification and reproducibility

Spacing regression tests also cover positive/negative Tc/Tw, q/Q restoration,
double-quote text, font/CTM scaling, spaces in replacements, deletion, re-editing,
spacing elsewhere on a page, shared Form instances and conservative rejection
of inherited/mixed spacing. No-op renders and replacement/reference renders
must be pixel-identical, neighboring glyphs must retain their geometry and
pdfcpu independently validates output.

Tests in `internal/pdfengine/textedit_test.go` cover shorter/longer replacements,
delete/move, `Tj`/kerning-adjusted `TJ`, standard and embedded TrueType fonts,
accents/euro, a real subset generated with the existing `tdewolff/font` package,
missing-glyph rejection, repeated words, shared Forms/Contents, repeated editing,
clipping, later opaque shapes, color changes within a text object and unsupported
text states. Regression tests reject new width/position overlaps, including
coincident origins, and allow safe repair/deletion of identifiable existing
overlaps. Duplication retains the original text and its geometry, produces
independently editable/searchable native text, and rejects copies on top of the
source. All preserve neighboring glyph positions and reject ambiguous duplicate
source glyphs.
Non-white background/vector/image pixels outside the edit zone are
unchanged at 144 DPI. pdfcpu independently validates generated output.

`pkg/document/text_editing_test.go` covers snapshots, one-step Undo/Redo,
independent duplicated pages, stable IDs, pending form values, overlays, editable
save/reopen, stale preparation and redaction exclusion. GUI tests cover toolbar
eligibility, mode exclusion, selection/dialog creation, zoom and Escape,
explicit deletion, mouse moves at two zoom levels, pending text changes and
cancellation before a queued move commits, repeated drops with independent
Undo steps, no-op drops, Shift with visible guides, baseline alignment, zoom
invariant snap tolerances and guide hysteresis. Duplicate tests cover pending
dialog text, immediate drag mode, rejection/retry at the original position,
empty values, Escape, two zoom levels, continued movement of the created copy
and independent Undo steps. Apply/Move overlap rejections leave history intact. Native extraction tests verify
that different font sizes share a baseline while their bounding boxes differ.

```sh
go test ./...
go vet ./...
# Optional ignored before/after PDFs and MuPDF PNGs:
PROVIEW_TEXT_EDIT_OUTPUT="$PWD/build/text-editing-prototype" \
  go test ./internal/pdfengine -run '^TestTextCorrection' -v -count=1
```

Poppler `pdftotext` and `pdftoppm` provide independent extraction/render checks.
No new runtime dependency was added. Proview's old vector-text annotation export
is intentionally not used for corrections, nor is redaction sanitation.

## Entry points

- `internal/pdfengine/textedit.*`: content extraction, eligibility, removal,
  insertion, font emission, restricted tagged movement and full rewrite.
- `pkg/document/text_editing.go`: immutable preparation and reversible source
  replacement. Rendering snapshots omit ID counters, so state comparison ignores
  those allocation counters while validating editable content.
- `pkg/gui/textEditing.go`: mode, lazy page geometry, word selection, dialog,
  asynchronous preparation, delete/move transactions, cancellation, stale guards
  and base-preview invalidation.
- `pkg/gui/textAlignment.go`: native word references, baseline/left-edge guides,
  snapping with hysteresis and Shift override.
- `pkg/gui/{gui,menu,history,lifecycle,annotationTools}.go`: toolbar, page layers,
  actions, history refresh and mode lifecycle.

References: [PDF reference](https://opensource.adobe.com/dc-acrobat-sdk-docs/pdfstandards/pdfreference1.4.pdf),
[Acrobat behavior](https://helpx.adobe.com/acrobat/using/edit-text-pdfs1.html),
[Foxit linked blocks](https://www.foxit.com/blog/how-to-link-and-join-text-objects/).
