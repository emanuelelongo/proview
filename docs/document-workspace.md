# Multiple documents in one window

Implemented on `improvements/editor-workflow`, 2026-10-17. Packaging is separate.
The tested `prototype/pdf-forms` branch was fast-forwarded into `develop` and
removed locally and remotely before this work.

## Opening behavior

- Open file, file drops, startup paths and desktop Open With requests share the
  same loading and routing behavior. Files in a batch are processed in order;
  requests received during file/annotation work wait until it finishes.
- An empty window opens the first file normally.
- When the selected document and incoming file are both PDFs, an Open PDF dialog
  offers New window, Append pages and Cancel. The incoming file is validated
  before the choice appears. Cancel leaves existing documents unchanged.
- Append pages preserves native PDF sources, selects the first appended page,
  retains the current save destination and records one Undo operation. Redo
  retains access to the transferred sources.
- Other combinations retain documents in the current window. The sidebar shows
  clickable image thumbnails with a selection border and no labels; PDF
  and STL entries retain names. Selecting an entry restores that document.
  Image thumbnails reuse displayed previews and resize with the sidebar.
  The selected PDF's page thumbnails remain below the document selector.
- Documents retain independent history, save destination, zoom, selected PDF
  page, scroll position and redaction destination warnings. STL documents retain
  their mesh and camera while other documents are selected.
- File > Close applies to the selected document, asks about unsaved changes and
  selects a remaining document. Closing the window checks all dirty documents;
  canceling preserves every document, including previously declined saves.
- Crops and generated images retain their existing independent-window behavior.

## PDF page thumbnails

PDF thumbnails show only a page number in an accent-colored circle at the
bottom right of the preview, with spacing between cards. Image thumbnails have
no label or badge. The PDF thumbnail scrollbar is hidden in every hover/scroll
state; wheel/trackpad scrolling and drag-edge autoscrolling remain available.

Click selects one page. Command-click on macOS / Control-click elsewhere toggles
individual pages, and Shift-click selects a range from the last anchor. Combining
the shortcut modifier with Shift adds the range to the current selection. A
multiple selection remains visible while the document scrolls; a single selection
continues to follow the active page. Selection tracks stable page IDs across
reordering and view rebuilds and is reset when switching documents.

Drag any selected thumbnail to move the group, retaining document order. Every
selected preview fades during the gesture; the insertion marker, edge scrolling
and Escape/outside cancellation work for the whole group. Right-click a selected
page to keep the group, or an unselected page to select it alone. Context and File
menus delete or duplicate the selection. Copies form an ordered group after the
last selected original and become selected. Each move/delete/duplicate is one
Undo operation; copied pages, overlays and redactions get independent IDs while
retaining immutable sources/artwork. Deletion must leave at least one page.

Thumbnails load lazily for the visible sidebar range plus one following page.
The first page reuses the opening preview; remaining pages show a static
`Loading…` label until rendered. Sidebar scrolling and viewport layout/resize
request visible thumbnails. Layout callbacks are coalesced and deferred until
sizes are valid, including the first transition from zero height, so opening or
enlarging a window does not require an extra scroll to start loading.

Rendering remains asynchronous, and obsolete document/generation results are
discarded. Successful thumbnail loads taking at least one second emit
`pdf_thumbnail_slow` with page number and elapsed time; fast loads add no log I/O.
Regression tests cover initial zero-height layout, resizing without scrolling,
lazy loading, placeholder completion and closed-window callbacks. Group operation
tests cover modifier/range selection, stable IDs, drag cancellation, one-step
Undo/Redo, page objects/redactions, native vector export and deletion protection.

## Keyboard navigation

Without modifiers, arrow keys scroll the image/PDF viewport by 40 view units per
keypress (including key repeat), clamped to the scrollable area. Page Up/Page Down
activate the previous/next PDF page, scroll it into view and reveal its thumbnail;
first/last page boundaries do not wrap. Multiple thumbnail selections survive
page navigation. Navigation is view state and never creates an Undo operation.

Focused controls (including inline text and PDF form fields), dialogs/popups,
modifier combinations, busy operations and active editing gestures retain their
keys. Page Up/Page Down have no image/STL binding, and existing STL view shortcuts
are unchanged.

## Desktop request forwarding

macOS Finder Apple Events continue through the existing native bridge. Startup
paths, including Windows/Linux Open With launches, first try forwarding to a
running primary process. The receiver queues these requests on the UI thread.
A launch without file arguments may still create another process.

`internal/openfiles` owns a per-user OS file lock and publishes a loopback TCP
endpoint with a random authentication token in the user's configuration folder
(`Proview/desktop`). Endpoints bind only to 127.0.0.1. Requests accept bounded
absolute paths and require the token; relative launch paths become absolute in
the sending process. OS locks release after crashes; a new primary replaces
stale endpoint metadata. Forwarding failure falls back to normal local opening.
`--check-ocr` still exits before desktop registration or forwarding.

## Verification

- `pkg/gui/documents_test.go`: document state/history isolation, PDF choices,
  append Undo/Redo/source ownership, window close cancellation, Finder routing
  and STL camera restoration.
- `pkg/gui/droppedFiles_test.go`: empty/dirty/busy windows, multiple drops,
  Unicode paths, missing files and closed-window delivery.
- `internal/openfiles/openfiles_test.go`: second-launch forwarding, absolute
  paths, endpoint restart and rejection of unauthenticated/relative requests.
- Run `go test ./...` and `go vet ./...`.

Native Finder/Explorer/desktop behavior and visual interaction still need manual
acceptance on the destination platforms; headless GUI tests and cross compilation
do not establish that acceptance.

## Zoom anchoring

Modifier + mouse wheel keeps the document point under the pointer. Toolbar and
keyboard zoom keep the viewport center. In a multipage PDF, the anchor belongs
to one page: its page coordinates are restored after layout, including centering
and fixed inter-page spacing. This prevents jumps toward earlier pages when
zooming deep in a document. Offsets clamp only at document edges. New documents still open at the beginning, fitted to the window. Intermediate
scroll notifications are suppressed until the anchor has been restored; visible
pages are then loaded/activated once, preserving active inline drafts.
