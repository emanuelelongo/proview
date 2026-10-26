# Multiple documents in one window

Implemented on `improvements/editor-workflow`, 2026-10-17. Packaging is separate.
The tested `prototype/pdf-forms` branch was fast-forwarded into `develop` and
removed locally and remotely before this work.

## Opening behavior

- Open file and file drops target the window where the interaction started.
  New blank images, images from the clipboard and Markdown/STL crops also join
  that window's sidebar and select the new document. Existing documents retain
  their edits, save destinations and view state.
- Startup paths and desktop Open With requests use the same ordered loader.
  A desktop request starts a new window when the active editor contains a
  document or has opening/work requests pending; an empty idle window is reused.
  Files in one batch share the receiving workspace, subject to the PDF rules.
  Requests within a window wait for its file/annotation work to finish.
- An empty window opens the first file normally.
- When the selected document and incoming file are both PDFs, an Open PDF dialog
  offers New window, Append pages and Cancel. The incoming file is validated
  before the choice appears. Cancel leaves existing documents unchanged.
- Append pages preserves native PDF sources, selects the first appended page,
  retains the current save destination and records one Undo operation. Redo
  retains access to the transferred sources.
- PDF and non-PDF files always use separate windows, in both opening directions,
  including file drops and desktop requests. No append dialog is shown for mixed types.
- Images and STL files retain documents in the current window. The sidebar shows
  clickable thumbnails with a selection border and no labels. Selecting an entry
  restores that document.
  Image thumbnails reuse displayed previews and resize with the sidebar. STL
  documents use a static 320 × 240 thumbnail of the initial isometric view,
  generated once in a worker and retained across camera changes and switches.
  Separators distinguish document thumbnails. Right-click a non-PDF thumbnail
  and choose **Close file** to activate and close that document, with the usual
  save prompt for unsaved changes.
  The selected PDF's page thumbnails remain below the document selector.
- Documents retain independent history, save destination, zoom, selected PDF
  page, scroll position and redaction destination warnings. STL documents retain
  their mesh and camera while other documents are selected.
- File > Close applies to the selected document, asks about unsaved changes and
  selects a remaining document. Closing the window checks all dirty documents;
  canceling preserves every document, including previously declined saves.
- PDF crops remain independent PNG documents in another window, preserving the
  PDF/non-PDF separation. Other generated images are independent, unsaved entries
  in the requesting workspace. Closing one never edits or closes its source.

## Selecting documents for output

Command-click on macOS / Control-click elsewhere toggles individual document
thumbnails. Shift-click selects a range from the anchor; combining the shortcut
modifier with Shift adds that range. A plain click returns to one selected file.
Selection follows retained document identities and uses sidebar order for output.
The last clicked document is displayed, independently of the other selections.

Right-click within the selection and choose **Print selected items…** or
**Create PDF from selected items**. Right-clicking an unselected thumbnail first
selects only that item. **File → Print…** also prints the selected documents, and
**File → Create PDF from Selection** creates a separate, unsaved PDF window.
Use Save/Save As in that window to choose its destination. Existing source files,
unsaved changes and histories remain intact.

Each selected image contributes one page at its full edited resolution. Creating
a PDF first offers **Same page size** (A4, US Letter or custom millimeters) or
**Individual page sizes**. Uniform output fits every page to the chosen size
and orientation without cropping; individual output gives each image a page
matching its bounds and retains native PDF page geometry. Markdown
documents contribute their complete paginated print layout. Models are excluded;
crop a model view into an image to include it. Output never uses sidebar preview
pixels. The same actions are available for selected PDF pages, preserving native
PDF text/vector content and including unsaved edits. See [Printing](printing.md).

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
A plain launch without file arguments may still create another process.
`--new-window` instead forwards an explicit new-window request, even without
files. `--work-dir` carries an absolute per-window dialog directory, taking
priority for opening and Save As without changing the process working directory
or persistent save-folder preferences. App-created child windows inherit it;
explicit new-window launches get their supplied directory or normal defaults.

`internal/openfiles` owns a per-user OS file lock and publishes a loopback TCP
endpoint with a random authentication token in the user's configuration folder
(`Proview/desktop`). Endpoints bind only to 127.0.0.1. Requests accept bounded
absolute paths and require the token; relative launch paths become absolute in
the sending process. The versioned endpoint advertises launch-option support.
Older running instances still accept ordinary file requests; requests with new
options fall back to a new process before sending files, so an older server cannot
silently ignore the new-window/directory options. OS locks release after crashes;
a new primary replaces
stale endpoint metadata. Forwarding failure falls back to normal local opening.
`--data-dir` creates a separate process-wide profile: signatures, logs and the
private `desktop/` endpoint use that root, while Fyne application preferences and
keychain credentials use a root-derived identity. Requests never change a running
process's profile. Aliases of the same root share IPC; different roots and the
default application do not. See [launch options](development.md#command-line-window-and-dialog-options)
for framework preferences and cache locations.
`--check-ocr` still exits before desktop registration or forwarding.

## Verification

- `pkg/gui/documents_test.go`: document state/history isolation, PDF choices,
  append Undo/Redo/source ownership, window close cancellation, Finder routing
  STL camera restoration and desktop-request window isolation.
- `pkg/gui/droppedFiles_test.go`: empty/dirty/busy windows, multiple drops,
  Unicode paths, missing files and closed-window delivery.
- `internal/openfiles/openfiles_test.go`: second-launch forwarding, absolute
  paths, endpoint restart and rejection of unauthenticated/relative requests.
- Run `go test ./...` and `go vet ./...`.

Native Finder/Explorer/desktop behavior and visual interaction still need manual
acceptance on the destination platforms; headless GUI tests and cross compilation
do not establish that acceptance.

## Zoom anchoring

Use Command on macOS or Control elsewhere with `+` / `=` to zoom in,
`-` to zoom out and `0` to fit the window. Shift variants, keypad plus/minus
and Italian keyboard positions are accepted. The platform modifier with the
mouse wheel also zooms. Fit is the initial size, rather than the minimum:
images/PDFs can shrink to approximately 7% of fit, and Markdown to 25% text size.
Images stay centered with their editing layers at the requested size.

Modifier + mouse wheel keeps the document point under the pointer. Toolbar and
keyboard zoom keep the viewport center. In a multipage PDF, the anchor belongs
to one page: its page coordinates are restored after layout, including centering
and fixed inter-page spacing. This prevents jumps toward earlier pages when
zooming deep in a document. Offsets clamp only at document edges. New documents still open at the beginning, fitted to the window. Intermediate
scroll notifications are suppressed until the anchor has been restored; visible
pages are then loaded/activated once, preserving active inline drafts.

## PDF display detail

Ordinary previews retain their 144-DPI coordinates and lazy loading. The primary
visible page gets a background rendering matched to view zoom and display scale,
bounded to 600 DPI, 16 million pixels and an 8192-pixel long side. Each window
retains one enhanced page; changing pages restores the previous page's ordinary
preview. Zoom changes are coalesced, with the current image visible until the
latest result is ready. Rendering changes only pixels, never page size, scroll
position, annotation coordinates, source data or history. Highlight fragments
use the enhanced density too, so highlighted text does not retain a blurry patch.
Stale results after document changes, form edits, native text edits or Undo/Redo
are discarded. Failures retain the ordinary/last valid preview and are logged.

## Markdown documents

Dragging the sidebar divider resizes existing document cards and previews in
place. It does not rebuild the document list, restart thumbnail workers or
recursively refresh the document viewport. The reading layout still reflows to
its new width; explicit refreshes continue to propagate theme changes.

Markdown files share the sidebar with images and STL models. Their thumbnail
is a fixed 320 × 240 light card of the opening content, generated in a worker
using the Markdown print layout and stopped at the first card boundary. It never
shrinks the whole document or captures the live window. Scrolling, zoom, theme,
viewport size and document switching leave it unchanged. Reading size and scroll position survive document switching. They stay
read-only and never use PDF/image history or saving. The existing PDF/non-PDF
separate-window rule also applies to Markdown. See [Markdown viewer](markdown-viewer.md).
