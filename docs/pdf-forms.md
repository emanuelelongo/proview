# PDF forms: implementation and session handoff

Integrated into `develop` on 2026-10-17 after manual acceptance.
The `prototype/pdf-forms` branch has been deleted.

Start here when continuing PDF form work. This is the current status and decision
record; update it whenever behavior, verification or remaining work changes.
`AGENTS.md` and `agent.md` link here. The prototype, fixtures and manual demo
were committed together on the prototype branch and merged into `develop`. No release or packaging was requested.

## Completed

Previously tested manually and accepted by the user:

- [x] Tabler `forms` toolbar button, enabled only for supported editable PDF fields.
- [x] Inline field editing; second click, Escape or another tool exits form mode.
- [x] Editable Save/Save As, unsaved indicator, Undo/Redo and isolated snapshots.
- [x] Text, multiline, character limits, comb, checkbox, radio, combo/editable combo.
- [x] Single-selection lists use visible rows, with selection retained in PDF appearances.
- [x] Opaque backgrounds prevent original PDF values reappearing during hover/focus.
- [x] Square checkboxes and circular radios, dark marks on a light face in both themes.
- [x] A single concentric SVG radio icon prevents dot/ring rounding drift at small sizes.
- [x] Editing typography and widget spacing scale with displayed page size,
      including fit-to-window changes, without replacing focused controls.

New work completed; automated verification, awaiting the user's desktop review:

- [x] Multiple-selection lists with independent checkmarks. Row clicks toggle one
      choice; arrows move the cursor, Space/Enter toggle it. Export uses array
      values and sorted selection indices; linked widgets, snapshots and history
      retain independent copies. Visible selected rows are highlighted in the PDF.
- [x] Password inputs and masked PDF appearances, including Unicode character
      counts. The logical value stays editable; the password flag does not encrypt it.
- [x] Embedded, standalone standard ResetForm buttons. Reset uses `/DV`, clears
      fields without a default, handles included/excluded field names or references
      and nonterminal name groups, and records exactly one Undo operation.
      Hidden/read-only fields are preserved. Other buttons/action chains remain inactive.
- [x] Declarative validation: required fields, character limits, single-line text,
      scalar/array cardinality, null characters and allowed choices. Required-field
      errors are shown inline; incomplete forms can still be saved as drafts.
      Required is a submission constraint, not a prohibition on saving unfinished work.
- [x] Exactly two brief toolbar background pulses when opening a fillable PDF.
      No persistent badge was added. Activating editing, replacing/closing the
      document or closing the window stops the effect; stale ticks are ignored.
- [x] Native, document and GUI regression tests for the new behavior.
- [x] Multiple-choice row backgrounds follow the list theme: only the checkbox
      face is light, with no stretched white strip or apparent padding.

## Remaining work and limits

These are not implemented; do not infer that supporting a text field executes
its attached validation or formatting script.

- [ ] User desktop review of multiple choices, password display, Reset, validation
      indicators and the two opening pulses (especially real display scaling).
- [ ] File-selection fields and a product decision about their use; no implicit attachments.
- [ ] Other actions: submission, import, links and action chains need explicit scope.
- [ ] Script-based date/number formatting, custom validation and automatic calculations.
      JavaScript remains disabled. The current validators enforce declarative PDF rules only.
- [ ] Certificate-based digital signatures and verification: separate from artwork
      signatures; see the [dedicated future plan](pdf-digital-signatures.md).
- [ ] XFA: separate engine/compatibility evaluation; currently rejected by form discovery.
- [ ] Rich-text fidelity and broader font/encoding compatibility. Editing text uses
      the Fyne theme; PDF appearances use MuPDF and the field's appearance settings,
      with fallback where the original font cannot be used. No font selector.
- [ ] Long/wrapped option labels and large lists need broader PDF appearance QA.
      Selection tinting follows MuPDF 1.24's multiline leading; labels that wrap
      can occupy more than one visual row. This limitation also affects mixed
      short/long labels before a selected item in a multiple-selection list.
- [ ] Native macOS/Windows and real third-party viewer regression checks. Local
      headless GUI tests do not certify those desktop environments.

Read-only and hidden fields are intentionally not editable. Signature fields and
unsupported action buttons are not activated. Ordinary filling does not flatten
widgets. The existing redaction export deliberately bakes visible appearances
and removes fields/actions while sanitizing the document.

## Implementation map and invariants

- `internal/pdfengine/bridge.h`, `bridge.c`, `forms.go`: independent MuPDF contexts,
  field discovery, defaults/reset-action metadata, scalar/array writes, regenerated
  appearances and full-page previews including widgets. MuPDF is **1.24.9**, matching
  the bundled go-fitz archives. Never change those headers independently of the ABI.
- `pkg/document/forms.go`: editable values keyed by immutable source and widget ID;
  linked widgets synchronize by field name. `SetFormValues` canonicalizes multiple
  choices in option order. `ResetForm` applies one reversible transaction.
- `pkg/document/model.go`, `overlay.go`: deep-copy array values in snapshots/history;
  use `maps.EqualFunc`/`slices.Equal`, not pointer equality. Sources stay immutable.
- `pkg/document/pages.go`: apply each source's field edits before native page collection,
  rotation/merge and overlay export. Form edits must survive those operations.
- `pkg/gui/forms.go`, `formMultiList.go`, `formToggle.go`: page-aligned controls,
  selection synchronization, passive required-field borders and preview refresh.
- `pkg/gui/toolbar.go`, `menu.go`, `lifecycle.go`: pulse presentation and lifecycle.
  Do not replay pulses during history/page rebuilds of the same document.

Important engine details:

1. go-fitz renders page contents without widget appearances. Form pages use the
   native `RenderFormPage` path; removing it makes filled values disappear from previews.
2. MuPDF 1.24 draws list labels but does not highlight selections. The bridge
   regenerates list appearances and adds a light Multiply tint over selected rows,
   preserving the generated text/fonts. `/V`, `/I` and `/TI` must agree. The first
   selected item is scrolled into view; offscreen items remain interactive on editing.
3. MuPDF 1.24 does not mask password appearance text. During synthesis only, use
   masked `/V`, then restore the logical value without requesting another synthesis.
   Never save the masked text as the actual password or execute document scripts.
4. Fyne hover colors can be translucent. Every control needs an opaque background;
   the native PDF underneath still contains its original field appearance.
5. Radio ring/dot SVGs use filled circles, not strokes: SVG stroke scaling caused
   very thick rings at small sizes. Keep both circles in the same rasterization.
6. Multiple-choice rows use the same light-face form toggles as standalone checks,
   with `inList` enabled so the surrounding area stays transparent when the row
   stretches the toggle vertically. Hover/focus tint only the checkbox face.
   Programmatic synchronization must not trigger another edit or toggle keyboard navigation.

## Verification and reproducible fixtures

Commands:

```sh
go test ./...
go vet ./...
# Focused checks while extending forms:
go test ./internal/pdfengine ./pkg/document ./pkg/gui -run 'Test.*Form|Test.*List|Test.*Multi|TestRadio|TestReset'
```

The full test and vet results for the latest changes are recorded below. Tests cover rendered selection pixels, masked appearance streams,
logical field values, linked page widgets, reset scopes/defaults, protected fields,
undo/redo and snapshot isolation, export/reopen, theme contrast, keyboard behavior,
inline validation and pulse cancellation. PDF structure is independently checked
with pdfcpu; native previews use MuPDF. No additional runtime dependencies added.

Fixtures:

- `internal/pdfengine/testdata/forms.pdf`: text, checkbox, combo with distinct
  display/export values, read-only and hidden fields.
- `internal/pdfengine/testdata/forms-extended.pdf`: two pages; password, linked
  multiple-choice widgets, required text/checkbox, defaults, reset-all, reset-subset
  (reference plus group name), reset-exclude, read-only/hidden fields and an inactive
  SubmitForm button. This is a small synthetic structural fixture, not a polished demo.

Latest verification (2026-10-15, Linux): `go test ./...` passed; `go vet ./...`
passed. Fyne interaction tests use the headless test driver. New desktop behavior
is ready for user review; no packages were built or published.

Follow-up verification (2026-10-16): multiple-choice rows visually checked with
headless renders in light and dark themes after removing the stretched light
background around checkboxes; `go test ./pkg/gui` passed.
