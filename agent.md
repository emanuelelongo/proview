# Proview: context for future agents

Development conventions, architectural constraints and regression prevention.
Keep this file current when those rules change. Operational commands and
dependency references are in `README.md`; version metadata is in `FyneApp.toml`.

## Language and collaboration

- Use **English only in code, comments, UI strings, errors and project documentation**.
  The user normally converses in Italian; replies may be in Italian.
- The user prefers practical implementation and small, coherent improvements.
  Follow explicit requests about planning, previews or delaying packaging.
- Preserve unrelated working-tree changes. Do not assume uncommitted work is
  disposable or commit it unless requested.

## Product and editing rules

Proview is a desktop image/PDF viewer and editor.
Supported images: PNG, JPEG, GIF, TIFF, BMP and WebP. GIF and multipage TIFF
show the first frame/page. Keep original bytes; new import formats save through
PNG/JPEG Save As instead of overwriting the source. Use document.OpenExtensions
for the opening picker and keep Linux/macOS file associations aligned. It supports reusable
signatures, single-line text, freehand drawing, highlighting, image cropping,
PDF page combination/reordering/deletion and blank pages.

- Keep original data separate from editable overlays. Editing must be reversible
  until export; each completed gesture should create one Undo operation.
- **Never rasterize existing PDF pages as an export fallback.** Preserve searchable
  text, vectors, image resolution, page boxes and rotation. SVG signatures and
  generated annotation geometry remain vector content; imported PNGs remain images.
  Raster signature vectorization is an explicit acquisition option that creates
  an SVG asset; it must not silently convert existing PNG signatures.
- Overlay rectangles use normalized page coordinates. Annotation dimensions use
  PDF points or original image pixels and must remain independent of view zoom.
- PDF crop deliberately creates an independent PNG editor window, including
  visible overlays, at preview resolution (144 DPI). It does not crop the PDF itself.
- Each editor window owns its document, history and save destination; all windows
  share one Fyne application. Closing one must not close the other editors.
- Use the shared `chooseFile` / `chooseFiles` helpers for document opening,
  signature import and PDF collection selection. Native pickers use
  `github.com/ncruces/zenity`; unavailable/failing backends fall back to Fyne.
  Cancellation must not trigger fallback. Keep selection work asynchronous,
  serialize pickers across windows, return results to the requesting editor and
  ignore results after it closes. Remember the last selected folder per window.
  Windows/X11 use Fyne's public native parent handles. On macOS, let osascript
  own the AppKit panel: attaching Standard Additions to the non-scriptable Fyne
  process fails. Restore the original editor's focus afterwards. Keep PDF Save As
  on its existing atomic path; never replace it with a truncating file writer.
  Fyne's `SetOnClosed` appends callbacks and does not accept nil: use an explicit
  guard when hiding the waiting dialog programmatically, including during fallback.
- Closing or replacing a dirty document uses **Save / Don't save / Cancel**.
  Continue only after a successful save; cancellation or failure retains the document.
- **File → Close** closes only the current document, keeps its editor window open,
  and is disabled without a document. Use Command/Ctrl+W and the existing unsaved-change flow.
  Mark **Exit** as Fyne's quit item to prevent an extra automatic **Quit** entry.
- Save As proposes **Untitled** with the appropriate extension.
- Successful local saves persist the last save directory in app preferences,
  shared across windows and restarts. Save As prefers that directory when it
  still exists, then the current file's folder, then the home directory.
- **Blank Page** adds a native white PDF page immediately after the selected page,
  matching its visible size/orientation, including CropBox and rotation. It selects
  the inserted page and is one Undo step. Without an open PDF, it opens the blank
  image form: 1200 × 800 pixels, white by default, optional transparency.
- Reorder PDF pages by dragging sidebar thumbnails, not a separate ordering form.
  Show a faded source, insertion line, vertical resize cursor and edge autoscroll.
  Release outside the sidebar or Escape cancels. Deletion must leave at least one page.

## Visual conventions and asset provenance

- Use compact icon buttons with readable tooltips instead of wide text buttons.
  Property controls should align in height; colors use swatches and dimensions
  use compact preset selectors. Keep forms spacious enough to use comfortably.
- Toolbar order is Open file, Blank page, Select, Crop, Draw, Highlight, Shapes, Text,
  Signature, Redact, Zoom out, Zoom in. With no document, only Open file and
  Blank page stay enabled. File, Edit, View and Tools menu commands must track
  document availability as well. Keyboard shortcuts are shown in their menu
  items; tool switches use the platform shortcut modifier plus Shift and a
  mnemonic key.
- Toolbar order begins **Open file**, then **Blank page**. Their icons are
  `folder` and `file-spark`, respectively.
- Use **Tabler Icons outline assets**, from https://github.com/tabler/tabler-icons,
  pinned to revision `74929e50416e2b7c0abb8368cdc74bdcb2560ab6`.
  Assets live in `pkg/gui/icons/tabler`; retain the MIT license and provenance.
  Fetch new icons from that revision's `icons/outline/<name>.svg` when available.
- Load toolbar icons with `tablerIcon`: its conversion of SVG strokes into filled
  outlines is intentional, because Fyne themes recolor fills. Do not bypass it.
- Current icon names: `signature`, `crop`, `pointer`, `text-outline`, `sketching`,
  `highlight`, `xxx`, `zoom-in`, `zoom-out`; stroke width is **stroke-straight** and opacity
  is **brightness-up**.
- The final application icon is **Arcade**: white document and magnifying glass,
  violet/blue/cyan gradients inspired by the 1980s. `assets/app-icon.png` is the
  transparent 1024 × 1024 source.
  It was generated with imagegen; the final prompt/provenance is in `assets/README.md`.
- The app PNG must remain embedded through `assets/icon.go` and set with
  `App.SetIcon`. `FyneApp.toml` also references it for packaging. Packaged applications
  must not depend on the current working directory to find their icon.

## Architecture and performance

- Entry point: `cmd/gui`. Packages: `document` (sources, model, history, export),
  `gui` (Fyne), `signatures` (library and SVG acquisition), `annotations` (geometry,
  text and fonts), `images` (image operations).
- Go 1.26; Fyne 2.7.4; MuPDF through `go-fitz` for previews; `pdfcpu` for native
  PDF page/export operations; `tdewolff/canvas` for vector geometry.
- `internal/pdfengine` provides native glyph selection and sanitized redaction.
  Use `RedactAndSanitize` for plain exports; `RedactComposedPDF` validates all
  original sources before flattening pdfcpu's generated overlay layer wrappers.
  **Never use `TrialRedact` in application exports.** Security acceptance checks
  run in ordinary tests, `task redact:probe` and native CI.
  Bundled `go-fitz v1.24.15` uses native MuPDF **1.24.9**; vendored bridge headers
  must match the native ABI when dependencies are upgraded.
- Redact (`xxx`) selects words/lines of source text or existing OCR. It draws
  fixed opaque black bands, has no property controls, and records one Undo per
  gesture. Marks follow stable page IDs; delete/reselect instead of move/resize.
  When a drag starts outside selectable text, create a clipped rectangular area
  redaction instead. Keep the gesture mode fixed until release, even if it crosses
  text. Blank/no-OCR pages also support rectangles; ignore clicks/tiny drags.
  No new OCR. Export removes content after all
  ordinary overlays are composed, without rasterizing the page.
- Redaction hover reveals are temporary view state: fade the black band while
  hovering if the original content still exists in the session. Restore opacity
  on exit/tool/zoom changes; never change mark data, history, thumbnails or exports.
  Exported/redacted source content cannot be reconstructed after reopening.
- Zoom uses the platform shortcut modifier (Command on macOS, Control elsewhere):
  plus/equal (including Shift and keypad variants), minus, zero for Fit to Window,
  and modifier + mouse wheel. The passive `zoomScrollSurface` intercepts only
  scrolling in the document viewport, querying `desktop.Driver.CurrentKeyModifiers`
  instead of tracking key state. Forward unmodified scroll events unchanged; do
  not capture hover/click/drag events or route document zoom through the sidebar.
  Hold **Space** and drag to pan above fit zoom on all desktop platforms; keep
  Space available to text fields and dialog controls. Control does not activate pan.
- Redaction exports strip metadata, attachments, fields/actions/comments and
  alternative/accessibility text. Visible appearances are baked first. Clean
  selected pixels in **every reuse** of an affected shared image, including
  clipped reuses. Unsupported structures must return an error, never an unsafe
  overlay-only fallback; details are in `internal/pdfengine/README.md`.
- Preserve the original editor source and Undo after saving redactions. Confirm
  irreversible export and effects on shared images. Retain destination redaction
  keys outside Undo and warn before overwrites that could restore removed data.
  PDF Save As must select a path before opening any writer: Fyne FileSave
  truncates existing files before its callback. Use the atomic PDF save chooser.
- PDF pages use stable references to their source and index, with independent
  page IDs. Reordering/insertion must retain caches and remap overlays with pages.
  Rendering snapshots isolate mutable editor state while sharing synchronized sources.
- Use `collectPDFPages` when collecting original source pages: pdfcpu rejects
  C2PA's `AFRelationship /C2PA_Manifest`. The compatibility path accepts only that
  extension during validation and validates the collected output normally.
- PDF previews render lazily at 144 DPI; thumbnails at 72 DPI to stay sharp at
  the sidebar's maximum width. Load thumbnails
  while scrolling, not only when clicked. Avoid eager rendering of the entire PDF.
- Expensive loading, rendering and encoding run outside event callbacks. Apply UI
  changes with `fyne.Do`/`DoAndWait`, retaining document/generation guards against
  stale results. Wait for pending annotation changes before save, close or page edits.
- Prepared page sources become owned by the receiving document. Do not close
  transferred sources; Undo/Redo and saved-state snapshots may still reference them.
  `Document.Close` gathers sources referenced by current, saved and history states.
- Reusable signature storage is separate from inserted copies. Library edits or
  deletion must not alter signatures already placed in a document.
- Signature entries have no user-facing names. New saves omit name metadata;
  legacy names are opaque JSON compatibility data so old cloud hashes stay valid.
  Sort library entries by creation time and ID. The toolbar opens a scrollable
  preview dropdown: click to insert, with Manage signatures as its final entry.
  Management rows have a full-height red trash button attached on the right;
  clicking a drawn preview opens its editor. Closing cloud settings returns to
  management. Keep dropdown dismissal and asynchronous loads scoped to the window.
- Selected imported SVG signatures show Thickness beside Color. `Asset.Thickness`
  expands vector outlines without changing original bytes; zero restores the
  original artwork. Preview, raster image export and vector PDF export must agree.
- Signature storage: macOS `~/Library/Application Support/Proview/signatures`;
  Windows `%LOCALAPPDATA%/Proview/signatures` (home AppData/Local fallback);
  Linux `$XDG_DATA_HOME/proview/signatures` (default `~/.local/share/proview/signatures`).
- Mouse signatures retain original strokes for later smoothing. Smoothness 0 restores
  raw strokes; higher values regenerate cubic curves. Show ink during the gesture.
  SVG import intentionally rejects unsupported features rather than silently changing
  appearance: text, embedded images, gradients, filters, masks, external references,
  and group-level opacity. Path fill/stroke opacity is supported.
- Included fonts are Go Sans, Go Mono and Latin Modern Roman. Keep their licenses
  bundled and available from **Help → Third-Party Licenses**.

## Fyne/macOS regressions to avoid

- Register the macOS open-document Apple event handler before showing the first
  window: GLFW runs AppKit's launch loop inside native window creation. Reinstall
  at `NSApplicationWillFinishLaunchingNotification` and queue paths until the
  editor is ready; register after Show and Finder launch documents are rejected.
  Preserve GLFW's application delegate and dispatch to `fyne.Do` from a Go worker.
- **Do not refresh the native macOS menu for state-only changes.** `Menu.Refresh`
  rebuilds AppKit menu items through `resetDarwinMenu` and can cause a SIGSEGV.
  Preserve `refreshMenuState`'s Darwin no-op;
  native menu items read Disabled/Checked when opened.
- Tooltips must remain passive content in the normal root `tooltipSurface`.
  Popup tooltips intercept mouse input, causing flickering and missed clicks.
- Thumbnail hover must not paint a gray overlay over the preview.
- Capture a thumbnail drag's source on mouse-down and update the destination on
  mouse-up. GLFW may choose another card after crossing the drag threshold.
- Retain Fyne's native-view guard in `processMouseMoved`: setting a cursor on a
  nil native window causes a panic. Fyne 2.7.4 includes the guard; 2.7.0 does not.
- MuPDF `premature end of data in flate filter` warnings concern PDF data;
  diagnose them separately from native-menu and nil-window crashes.

## Build, packaging and verification

- Use `Taskfile.yaml`: `task run`, `task test`, `task vet`, `task check`, `task build`.
  `task --list` describes all tasks. CGO and platform-native Fyne libraries are required.
- Native targets: `task build:macos`, `task build:linux`,
  `task build:windows:native`. `task build:windows` uses the bundled Windows Docker toolchain
  from any host.
  Choose `ARCH=arm64` or `ARCH=amd64` as supported by the C compiler.
  Windows builds use `-H windowsgui` to suppress the console.
- `task package:macos` creates `build/macos-<arch>/Proview.app`. It converts the PNG
  through `sips`/`iconutil`, installs the `.icns`, writes bundle metadata, signs ad hoc,
  verifies the signature, touches the bundle for Finder and deletes the intermediate
  executable. Only the `.app` should remain in that architecture's output directory.
- Application ID: `com.emanuelelongo.proview`. Version and build come from
  `FyneApp.toml`. macOS uses the numeric portion of the version for
  `CFBundleShortVersionString` and retains the full beta label in
  `CFBundleGetInfoString`. The bundle is suitable for local use; Developer ID signing
  and notarization are not implemented. The bundle declares PDF, PNG, JPEG, GIF,
  TIFF, BMP, WebP and STL file associations without changing the user's default
  applications. STL uses the system UTI
  `public.standard-tesselated-geometry-format` with Viewer role; Linux launchers
  declare `model/stl`.
- Windows startup registers supported extensions through `internal/fileassoc`
  under `HKCU\Software\Classes` using application-specific ProgIDs and
  `OpenWithProgids`. Refresh commands when the portable executable moves;
  never overwrite extension defaults, `UserChoice`, or other apps' entries.
  `--check-ocr` exits before GUI startup and must not register associations.
- `task tools:cross` installs fyne-cross v1.6.3. `task cross:linux` requires Docker and fyne-cross in PATH;
  `task cross:windows` requires only a running Docker daemon.
  Windows cross builds layer Go 1.26.0 over the upstream Fyne cross image using
  `docker/fyne-cross-windows/Dockerfile`; the upstream image's bundled compiler
  can lag behind the `go` version in `go.mod`. Windows outputs go under
  `build/windows-amd64`; legacy Linux cross outputs go under `fyne-cross`.
  Run artifacts on the target OS before claiming support.
- `task package:linux` creates a native AMD64 or ARM64 AppImage with the existing
  desktop entry and icon. It downloads the pinned linuxdeploy release into the
  ignored `build/tools` directory and verifies its SHA-256 checksum. Package on
  the oldest supported Linux release to avoid raising the glibc baseline.
- If `iconutil` reports `Invalid Iconset` despite correct PNG sizes, execution inside
  an agent sandbox can prevent conversion. Check execution permissions before
  changing valid assets, and follow the current permission policy.
- For agent tests with restricted cache/config writes, use writable locations, e.g.:
  `GOCACHE=/private/tmp/proview-go-cache XDG_CONFIG_HOME=/private/tmp/proview-config`.
- Test meaningful risks: PDF vector/text preservation, page dimensions/rotation,
  overlay remapping, Undo/Redo, transparent exports and save cancellation/failure.
  GUI unit tests use Fyne's test driver and do not prove actual Finder/Dock behavior.
- **Build** in `.github/workflows/ci.yml` is the single build workflow. Its
  `runner` input selects GitHub-hosted native builds or self-hosted cross builds;
  pull requests always use GitHub-hosted runners. Version selection, quality
  checks and publication use the selected runner. Gate publication on the
  selected build path succeeding while the other path is intentionally skipped.
  Check actual CI results before reporting success on destination platforms.
- Self-hosted jobs require only the custom `macos-self-hosted` label, assigned
  to an Apple Silicon Mac. Reuse `packaging/build.py` and Taskfile
  packaging. Select Homebrew Python through `packaging/self_hosted_python.sh`
  in every self-hosted job; require `tarfile.data_filter` support and export its
  bin directory through `GITHUB_PATH` so Task also uses it. Retain local Go caches;
  disable setup-go's
  remote cache restore to avoid overwriting read-only module directories.
  `packaging/self_hosted.py` runs Linux/Windows tools in Docker.
  Preserve Ubuntu 22.04 AMD64 and Ubuntu 24.04 ARM64 AppImage baselines.
  Colima on Apple Silicon requires `colima start --vz-rosetta` for AMD64 jobs;
  QEMU user-mode emulation can corrupt Go runtime memory. Keep Linux AppDir
  staging under the container's `/tmp`, with cleanup on success and failure:
  libstdc++ copy_file creates mode-0200 files that fail on VirtioFS bind mounts.
  Keep AMD64 linuxdeploy compatible with Rosetta by clearing AppImage identification
  bytes at offset 8 in a separate executable copy after checksum verification.
  Preserve the original download and the generated release AppImage headers.
  Keep metadata and Google OAuth configuration identical to native builds. Windows tests are
  cross-compiled, not executed; do not report cross artifacts as runtime-tested.
- `ld: warning: ignoring duplicate libraries: '-lobjc'` alone is not a build failure;
  check the command's exit status.
- Generated binaries, `build`, `fyne-cross`, `.task` and `.DS_Store` are ignored.
  Keep generated output out of version control; retain source assets and licenses.

## Shapes, clipboard and transformations

- Overlay kinds distinguish library signature copies, pasted images and annotations.
  `images.Transform` stores source-axis flips followed by clockwise quarter turns.
  Keep source artwork/drawing data independent of transforms. Use the same transform
  and rectangle for preview, image rendering and vector PDF export; independent
  shape-axis resizing requires matching PDF artwork aspect ratios.
- Whole-image transformations apply after the source crop and transform all overlay
  rectangles/orientations in one history operation. Map crop selections back through
  the inverse image transform. Page dimensions must not render the transformed image.
- Text remains active after insertion and edits the current object. Commit entry
  edits on focus loss/submission or before another placement without clearing the
  entry. There is no check button or automatic text cleanup. Empty defaults never
  create text objects.
- Holding Shift temporarily activates Select from Text, Draw, Highlight or Shapes (including
  Magnifier), highlighting its toolbar icon and restoring the insertion tool on
  release. Keep insertion defaults separate from temporary object property edits.
  Never interrupt entry typing or a mouse gesture; complete moves/resizes as one
  Undo step even when Shift is released before the mouse. Poll native modifier
  state while these tools are active because focused Fyne controls receive key
  events instead of canvas handlers. Stop polling on tool changes/window close.
- Magnifier zoom uses only a slider, aligned with the 36-pixel property row.
- Area selection is transient UI state and does not mark documents dirty. Background
  drag selects an area; Alt drag works over objects. Clear selection when switching
  tools/pages/documents. Text-focused clipboard shortcuts take precedence.
  Successful area copy briefly flashes and fades the selection, then deselects it.
  Failed copies retain the selection; delayed feedback must not affect a new selection.
- Native image clipboard uses `golang.design/x/clipboard` v0.9.0 behind a testable
  adapter. Initialize lazily, run I/O/PNG work outside event callbacks, report backend
  errors and ignore completion for closed/replaced editors. Do not encode images in
  Fyne's text clipboard. Pasted images retain their own immutable PNG data.
  `clipboard.ErrNoData` for the private object format means Paste must try the
  image format; it is not a backend failure. Test adapters must model this error.
- Duplicate pages retain immutable synchronized sources but get new page, overlay
  and redaction IDs. Copy all editing state; keep shared-image redaction rules.
- Shapes remain vector geometry on PDF export. Callouts have a fixed tail and no
  integrated text; stars have five points; arrows follow their drag direction.
- Magnifiers are circular, dynamic, exclude every other magnifier, and sample only
  earlier objects in stacking order. Their zoom is independent of diameter (1–8×).
  Export only the visible disk at 300 DPI for PDF; never embed hidden corner pixels.
- `SafeRenderPage` uses actual PDF removal/sanitation before raster derivatives when
  redactions exist, including all shared-image reuses. Both area copy and magnifiers
  must use this safe path; visual redaction masks and hover reveals are insufficient.
- Lens drag previews are coalesced per layer and checked against document revision
  and generation. Keep expensive rendering off the UI thread; stale results must
  not replace a newer preview.
- Canvas SVG output needs CSS rgba normalized to explicit paint opacity before
  reparsing; preserve alpha rather than relying on the upstream fractional-alpha parser.

- Selected objects take precedence over area selection for Copy. Publish PNG and
  `application/x-proview-object+json` atomically with the native clipboard's
  `WriteAll`; Paste prefers the validated object format. Keep assets, original
  strokes, annotation properties, stroke thickening and transforms independent
  from source documents. Allocate a new overlay ID and one Undo step per paste.
  Preserve physical dimensions across pages/windows and offset repeated pastes.
  Replacing clipboard contents must remove the object format, never fall back to
  a stale in-memory object. Objects intersecting redactions use sanitized area PNG
  copying instead of publishing hidden editable content.
- Right-click objects for Bring to Front, Bring Forward, Send Backward and
  Send to Back. Reordering uses `Document.ReorderOverlay`, affects only objects
  on the same page, and records one Undo step. Preview, hit testing, lens samples
  and exports follow the same overlay order; redactions remain above objects.
- Text placement waits for pending entry commits rather than dropping the click.
  Pending text creation must not override a subsequent explicit tool change.

## Development releases

- Original project code is licensed under AGPL-3.0-or-later; retain the root
  `LICENSE` and `NOTICE`. Third-party components keep their own terms.
  `docs/licensing.md` records the dependency review and open distribution work,
  including incomplete scanx licensing and native MuPDF/source inventories.
  Do not report binary license compliance as complete from the root license alone.

- Releases are manual through the `branch` workflow input. Pushes do not run CI;
  pull requests run native CI checks and packaging without publishing.
  `develop` produces `v1.<number>`; other branches produce `v1-dev.<number>`.
  Google Drive is available in every build; do not add build/runtime provider
  flags or duplicate package variants. Do not gate provider selection, the connection
  button or saved connection restoration on embedded OAuth configuration. Missing
  configuration is reported when authorization is attempted. Connection still
  requires OAuth client configuration and the user's authorization. Both runner choices
  use the same Build workflow run number minus the repository variable `RELEASE_RUN_OFFSET`
  (default zero) for release tags and native package versions. The offset is
  currently 2 so the next build starts release numbering at `v1.0` after the
  failed initial run; bundle build numbers retain the original workflow run number.
  Keep the workflow at `ci.yml` to preserve its existing counter. Pin checks,
  builds, metadata and the release target to the same resolved branch commit.
- Keep all five package targets (macOS ARM64/AMD64 app ZIPs, Windows AMD64 GUI
  executable ZIP, Linux ARM64/AMD64 AppImages), with SHA256 checksums in each release.
  Reuse Taskfile packaging; CI version changes stay inside runner working copies.
- Gate publication on quality and all matrix jobs; upload into a draft before
  publishing. Reruns replace published assets and checksums. Restrict
  `contents: write` to the release job; no personal token is needed.
- Keep the Linux ARM64 package job on Ubuntu 24.04 or newer: bundled go-fitz
  MuPDF archives reference glibc 2.38 C23 conversion symbols that are absent
  on Ubuntu 22.04. Linux AMD64 can retain its older build baseline.
- Windows CI selects the runner's `C:/mingw64/bin/gcc.exe` explicitly. Do not
  install the latest MSYS2 compiler over it: that runtime failed to link bundled
  MuPDF's `__intrinsic_setjmpex` references. Keep native PDF tests enabled.

## OCR selection prototype

- Status bar tips are embedded from `pkg/gui/tips.txt`, one complete English tip
  per line; blank lines and `#` comments are ignored. Review that file when adding
  features rather than embedding tip strings in Go source.

- Packaged/Task builds enable transient text selection in Select automatically,
  using native PDF text first and a bundled Tesseract 5 subprocess otherwise.
  Direct unbundled builds still use `PROVIEW_OCR=1` for development. Setup/evaluation instructions
  are in `docs/ocr-prototype.md`; packaging details are in `docs/ocr-packaging.md`.
- Keep OCR independent of the annotation queue and its busy flag. Publish results
  only for the current document/content key, cancel obsolete work and bound the
  in-memory cache. No OCR data belongs in editor history or exported content.
- Selection starts on a word; background/Alt drags select PNG areas. Fix gesture
  mode at mouse-down. Preserve object and focused-entry clipboard precedence.
- Text extraction and OCR on redacted PDFs must use sanitized source content,
  including shared-image effects. Never fall back to the original text on errors.
- `task ocr:probe` generates deterministic development fixtures and a JSON report;
  synthetic images do not establish quality on real photographs. Ordinary tests
  must not require an installed OCR binary.

- QR recognition is always enabled for images and PDF source pages, independently
  of OCR availability. It uses the pure Go gozxing decoder in a separate bounded
  background queue/cache. Click or tap selects a whole QR before OCR hit testing;
  reuse text highlighting, status, copy/context menu and Escape. Only clicking
  the status URL opens a browser. Preserve exact QR payloads, including trailing
  URL punctuation. Source crop/rotation and sanitized shared-image redactions
  invalidate QR results using the same source keys as text selection.

- Normalize camera JPEG EXIF orientation on source loading, including mirrors,
  before view/OCR/crop/export. Keep original encoded bytes unchanged. OCR input
  has a 2400-pixel long-side cap and uses Sauvola adaptive thresholding; selection
  geometry stays normalized to the displayed source rather than OCR resolution.
- OCR estimates a dominant text rotation (up to ±40 degrees) from contrast-edge
  projections. Keep deskewing transient, expand the white canvas to preserve all
  source corners, and invert word geometry back to source coordinates before
  applying document transforms. Do not correct perspective or alter view/export
  pixels as part of recognition.

- Release OCR assets are embedded with `proview_ocr_bundle` for each supported
  target; build them with checksum-pinned `packaging/ocr_runtime.py`. Keep the
  Windows distribution to one executable and reject external helper DLLs.
  Extract only verified assets atomically into a private user cache, never into
  the download directory. Do not overwrite/remove runtimes used by other app
  processes. Native bundled builds must pass `--check-ocr` before packaging.

## Raster signature acquisition prototype

- **New signature** opens **Acquire signature** with required radio options
  **Import from file** (default) and **Draw with mouse**, then equal **OK** /
  **Cancel** buttons. Import uses
  SVG directly and converts JPEG/PNG into transparent monochrome SVG outlines
  using `pkg/signatures/vectorize.go`. Existing stored PNG assets stay unchanged.
  The editor is titled **Import signature** and shows equally sized source/preview
  panels, one **Sensitivity** slider, maximum smoothing, and equal footer buttons.
  Evaluation instructions are in `docs/signature-vectorization.md`;
  `task signature:probe` produces local artifacts.
- Normalize EXIF orientation, cap input/working dimensions, preserve holes/dots
  and avoid changing the scan. Fit contours using the existing cubic fitter;
  these are filled outlines, not inferred original pen strokes.
- Preview work uses cancellation, a bounded mailbox and revision checks. Save
  only the current valid SVG; cancel on dialog/window close and preserve crop
  coordinate mapping during live selection and window resizing. Keep the original
  image visible and update the preview without a crop confirmation. Ordinary
  document cropping still requires confirmation. Do not commit personal signature images.

## STL viewer prototype

- STL uses `internal/model3d` and the separate `modelViewport` in each GUI window.
  Do not treat meshes as raster `document.Document` objects. Model mode is read-only;
  editing, signatures, save and OCR remain unavailable, while Open/Close/zoom work.
  Crop captures the displayed frame into an independent, unsaved PNG window.
  Selection freezes frame publication; Escape/camera changes/Close cancel it.
- STL has no additional on-canvas controls/help. View contains STL-only commands:
  1–7 select Isometric, Top, Bottom, Front, Rear, Left, Right; 0 toggles wireframe.
  Native macOS hides that section through an AppKit menu delegate without rebuilding
  Fyne menus or changing callback IDs. White surfaces use a dark viewport background.
- Orbit with drag, pan with Space+drag; camera changes never dirty the model.
  Rendering stays in a worker with coalesced updates and immutable published frames.
  The macOS GPU prototype uses `internal/modelgpu`, an isolated CGL/OpenGL 3.2
  context on a locked worker thread, cached mesh buffers, interpolated lighting and
  up to 4× MSAA. Never access Fyne's GL context. Readback preserves Crop/frame
  ownership. Other platforms and GPU failures use CPU; `PROVIEW_STL_RENDERER=cpu`
  forces it. CPU still frames use 2× supersampling and shared-vertex shading with
  a sharp-edge threshold; keep the faster drag path and bounded render buffers.
  GL initialization uses sync.Once; VBO attribute offsets use the binding's
  VertexAttribPointerWithOffset (PtrOffset fails Go checkptr). Release contexts
  and all GL objects on their owning worker thread. Native GPU tests are opt-in
  with `PROVIEW_TEST_STL_GPU=1`, and must fail if software fallback is used.
  Close/replacement cancels rendering and rejects frames from the previous mesh.
- Preserve source coordinates and report dimensions in unspecified model units.
  Keep STL parser size/count checks and malformed/degenerate geometry handling.
  Evaluation instructions and scope are in `docs/stl-prototype.md`.

## Signature cloud current state

- Google Drive and S3 use `CurrentStateProvider.SyncCurrent`, with a lightweight
  index, latest payload per signature, tombstones and persisted deduplication aliases.
  Shared index validation and acknowledgement belong in `cloud/current_state.go`.
- Publish replacement payloads before the index; index writes require a Drive v2
  ETag / If-Match condition and `newRevision=false`. Retry conflicts by rereading
  and merging; never use unconditional overwrites. Verify downloaded index bytes
  against the checksums in both Drive v3 listing and fresh v2 metadata; media
  downloads use a different ETag from the metadata used for conditional writes.
- Cache unchanged indexes, reuse validated local payloads after restart, cap
  transfers at four concurrent requests, and cache folder discovery for five minutes.
- Migrate legacy operations before retiring them. Trash only validated legacy files
  or superseded payloads observed before publication, never concurrent unindexed
  uploads. Persist pending cleanup IDs so interrupted cleanup retries safely.
- Current-state sync must preserve deduplication aliases after assets change and
  retain deletion records across offline devices. The local write-ahead journal
  remains for recovery; neither provider reuploads its history.

- S3 checks `current-index.json` with conditional GET / If-None-Match, publishes
  `current/<revision-id>.json` payloads with If-None-Match: *, and replaces the
  index using its ETag / If-Match. Retry conflicts by rereading and merging.
  Require valid payload keys beneath the configured prefix and bounded indexes.
  Read legacy operations only when no index exists. Keep unreferenced objects
  recoverable without adding DeleteObject permissions; never expire all payloads
  by age because an old payload can still be current. All archive clients must
  upgrade before editing: old clients cannot read the new index protocol.
