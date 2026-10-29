# Document text search

Implemented on `prototype/pdf-forms`, 2026-10-16, in a separate commit from forms
and native text correction. No dependency on the native correction experiment.
Released in v1.10 on 2026-10-19.

Find searches OCR text in both images and scanned PDF pages inside Proview.
Recognized text is not embedded in saved images or exported PDFs; OCR does not
create a searchable PDF for use in other applications.

- Find: Ctrl/Command+F. Next/previous: F3/Shift+F3 or Ctrl/Command+G and
  Ctrl/Command+Shift+G. Inside Find, Enter/Shift+Enter navigate; Escape closes.
- Literal case-insensitive search, Unicode NFC normalization and whitespace
  normalization; phrases can cross line breaks within a page. No regex/replace.
- Sequential background scan of the entire document, with progress, partial
  results, wraparound navigation and a highlight on the current match. Partial
  OCR word matches highlight only matching character boxes when available.
  Native PDF text and OCR words without valid character geometry retain whole-word
  highlights. Match offsets map normalized Unicode back to the original text.
- Uses `Document.SelectableText`: native PDF text first; existing local OCR for
  images or pages without native text when enabled. Native PDF search works
  without OCR being enabled. Recognition stays transient and never modifies PDFs.
- Reuses visible-page recognition and retains layouts across queries in a
  128-page / 16 MiB bounded cache. Source-revision checks are independent of
  annotation edits and do not traverse all pages for each progress update.
  Only the current match layer refreshes. One search worker, 180 ms
  debounce, per-page timeout, cancellation and stale-result guards; 10,000-match
  cap shown explicitly. Failed/unavailable pages are counted, not treated as
  a successful scan with no matches.
- Source geometry/sanitation changes invalidate results. Redacted text uses the
  existing sanitized selection path. Search and highlights do not change history.

Scope: source-page text, not Proview's added vector-text annotations. The native
selection pipeline does not expose newly edited form values as page text. Mixed
pages with native text do not additionally OCR embedded images. Only the first
frame of GIF and first page of multipage TIFF are searched; GIF playback does not
change the OCR input.

Entry points: `internal/textselect/search.go`, `pkg/gui/search.go`, menu/key
bindings and a dedicated highlight in `signatureLayer`. Tests cover Unicode,
phrases, result caps, all-page traversal, native-vs-OCR fallback, cache reuse,
cancellation, wraparound, Escape and unchanged history. GUI tests are headless;
desktop keyboard/focus and real OCR quality remain manual verification.

## Markdown

Markdown uses the same Find bar, menu commands, shortcuts and match cap. It
searches the retained rendered text blocks with `textselect.Layout.Find`, without
OCR or a document snapshot. Matches retain block/UTF-8 byte ranges, including
phrases across blocks, Unicode normalization and strikethrough decorations.
A separate highlight shares the selection renderer's retained geometry and
culling; Find never replaces the user's text selection. Navigation scrolls both
the page and any horizontal code/table viewport. Rebuilds retain the current
match index; document switches cancel pending work and close Find.

Implementation: `pkg/gui/markdownSearch.go`, `markdownSelection.go` and the shared
search lifecycle. GUI tests cover formatted Unicode phrases, tables/code,
strikethrough offsets, vertical/horizontal navigation, independent selections,
zoom rebuilds and double/triple-click selection. See [Markdown viewer](markdown-viewer.md).
