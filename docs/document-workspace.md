# Multiple documents in one window

Local implementation on `develop`, 2026-10-17. Not committed or packaged.
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
  clickable image thumbnails with a selection border and dirty indicators; PDF
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
