# Native Markdown viewer

Status: implemented on `feature/native-markdown`; not a published release.
The viewer uses Go and Fyne only. It requires no webview, WPE, Node.js,
browser, container or additional display backend.

## Opening and workspace

Open `.md` and `.markdown` files through File → Open, the command line,
desktop file associations or drag and drop. Markdown shares the document
sidebar with images and STL models. Each Markdown entry gets a cached thumbnail
of its rendered viewport after diagram loading; it stays stable while reading
and switching documents. PDF/non-PDF opening keeps the existing
separate-window policy. Switching documents retains reading size and scroll
position. Closing Markdown restores the previous retained document.

Markdown is read-only. Annotation, transformation, signature and save
commands are disabled. Source files are never rewritten. Zoom In/Out adjusts
text size from 75% to 200%; Fit to Window restores the default reading size.
The text column reflows up to a 900-point maximum width. A theme-aware page
border, inner padding and contrasting workspace margins distinguish its bounds.
Arrow keys, Page Up,
Page Down, Home and End scroll when no control or dialog owns keyboard focus.

## Supported content

- Headings, paragraphs, nested bold/italic, strikethrough and inline code.
- Ordered/unordered nested lists, task lists, quotations and separators.
- GFM tables with column alignment and inline formatting.
- Fenced code with explicit language highlighting, using Chroma and palettes
  for the current light/dark theme. Unknown languages remain plain code.
  Code overflow scrolls horizontally inside its block. Vertical wheel/trackpad
  movement over code continues scrolling the document.
- Mermaid diagrams rendered natively as images, with an always-visible compact
  code-copy button. Successful previews do not show a source section.
- Relative local raster images; missing/remote images show their alternative text.
- Clicked HTTP/HTTPS/mailto links, relative document links and heading anchors.

Mermaid uses the pinned sketch renderer described below. If a diagram is invalid,
unsupported or exceeds its limits, a message is shown and its original source
is displayed automatically. Following Markdown remains visible. Formulas display their
source text. HTML tags and blocks display literally; no scripts, styles or HTML
are executed.

Right-click a prose block and choose **Copy text** to copy the block's plain text.
Code blocks have a copy button that preserves the original code, including
whitespace. Edit → Copy / Command/Ctrl+C copies the full Markdown source when
no text field has focus. Drag selection across rendered rich text is not yet
implemented; Fyne's RichText widget does not provide it.

Right-click a rendered diagram and choose **Copy image** to copy its PNG at the
rendered resolution. **Crop** freezes the visible Markdown viewport, including
text and diagrams at the current theme and zoom. Confirming a rectangular
selection opens an independent, unsaved PNG in the same window's sidebar and
selects it. Crop activates before the captured pixels are converted in a worker,
so the first drag and confirmation can proceed during preparation. The snapshot
uses GPU scaling instead of CPU resampling; late copies after cancellation or
switching cannot replace the view. Pending Mermaid layout updates wait until
Crop ends. Wheel events are consumed by the crop overlay to keep the view fixed. The original Markdown remains available. Escape, zoom, resize or
document switching cancels the selection. Crop captures visible content only.

## Loading and resource bounds

`internal/markdown` parses UTF-8 in a background opening worker, accepting a BOM
and normalising CRLF. Source is limited to 2 MiB. Goldmark supplies the GFM tree;
Chroma tokenises fenced blocks up to 128 KiB and 32,768 tokens. Larger blocks
retain plain code. Parsing and token preparation are independent of Fyne.

Images are loaded in that worker, without network requests. Paths must stay
inside the canonical source directory, including after symlink resolution.
Absolute paths and remote resources are not loaded automatically. Individual
images are limited to 16 MiB encoded and eight million decoded pixels; aggregate
image loading has a bounded budget. Non-file URI documents do not resolve
relative resources. External links open only after a click.

`pkg/gui/markdownViewer.go` maps the tree to native Fyne objects, with a local
theme for text sizes and token colours. `editorDocument` owns the immutable
Markdown model and its reading state. Markdown never creates an editable
`document.Document` or enters PDF/image export paths.

## Mermaid rendering

The only new module is `tuios.dev/sketch`, pinned to
`v0.3.2-0.20261024080457-fed94a1a2d22` (the revision inspected in the
[renderer review](mermaid-renderers-review.md)). The application imports its
core and raster packages, without tuios CLI, terminal drawing or plot packages.
No remote fork has been created.

`internal/mermaid` is a Go-only adapter. It generates SVG and rasterizes it
straight to an immutable RGBA image. A document-local LRU cache reuses results
by source, viewport width, text zoom, pixel density, theme and page background.
Diagrams adapt their layout and text size to the reading viewport. Their raster
background matches the page so contrast correction keeps unboxed labels visible. Raster density
follows the window's display scale, up to 4x and within the pixel budget.

The GUI starts jobs after layout, coalesces changes and rejects obsolete results.
Closing, switching or rebuilding the view cancels waiting jobs. An active sketch
call cannot be interrupted; its bounded work completes and its stale result is
discarded. Four bounded render slots allow concurrent diagram layouts, while
in-flight request coalescing ensures identical requests share single-pass rendering.

Current limits:

- Up to 16 Mermaid previews per document view; later blocks remain source.
- 16 KiB source and 128 expanded flowchart edges per diagram.
- 1 MiB generated SVG; at most two million pixels and 4096 pixels per raster axis.
- Cached images total at most eight million pixels (about 32 MB), with at most
  32 cache entries per document. Active previews can also retain images.

Links and images inside diagrams are disabled. Rendering needs no network access.
The source remains available even when rendering fails. Rendered diagrams are
static: interactive Mermaid links, animation and embedded HTML are not provided.

The native rasterizer uses its bundled Arimo and Go Mono fonts. Italian accents
are supported; missing glyphs in other scripts, including Chinese/Japanese, can
appear as boxes. Font fallback remains a future library improvement. A diagram's
syntax and layout follow this independent renderer, rather than a promise of
full equivalence to official Mermaid.

## Verification

- Loader tests cover GFM content, exact highlighted source, UTF-8/BOM/CRLF,
  source limits, local images and rejection of escaping/remote resource paths.
- GUI tests cover queued opening, workspace switching and closing, read-only
  controls, zoom/scroll restoration, heading links, keyboard navigation and cached
  viewport thumbnails with closed-document callback rejection.
- Mermaid adapter tests cover valid output, cache reuse/eviction, theme and zoom,
  source/edge/image budgets, cancellation and concurrent callers.
- Mermaid GUI tests cover asynchronous rendering, theme/zoom/viewport changes,
  source fallback, preservation of following content, late-layout scroll restoration
  closing during work, exact code copying and image clipboard output.
- Markdown crop tests cover viewport pixel mapping, cancellation and independent
  image creation in the same workspace without source changes, immediate dragging
  and confirmation during a blocked snapshot copy, late-copy rejection and
  deferred diagram updates.
- Fyne software-rendered frames were visually inspected for prose, tables,
  strikethrough, code highlighting and flowchart/sequence previews in both themes. These do not establish native desktop
  input, accessibility or Linux/Wayland runtime verification.

Run `go test ./internal/markdown ./pkg/gui -run 'Test(Load|LocalImages|Markdown|Mermaid)'`.
For a manual trial, open `internal/markdown/testdata/reading.md` with the normal
application. Use its standard build instructions; there are no Markdown build
flags or extra runtime dependencies. Native Ubuntu 24.04/GNOME verification is
still pending.

## Deferred work

Rendered text drag selection, in-document search, remote-image opt-in and live
reload are not implemented. Mathematical rendering and HTML layout remain
future decisions. This replaces the discarded webview probe.

The [native Mermaid renderer review](mermaid-renderers-review.md) records the
source examination and comparisons that led to this integration.


## Crop responsiveness check

On 2026-10-24, a native macOS investigation identified the multi-second Crop delay
when opening Markdown documents with code and Mermaid diagrams: diagrams were
previously serialized behind a single global rendering slot (`renderSlot = make(chan struct{}, 1)`),
causing sequential diagram queues, chained layout reflows and thumbnail capture delays.
Parallelizing diagram rendering to four concurrent bounded slots with in-flight call
coalescing eliminates the multi-second bottleneck. Moving pixel conversion to
a worker and enabling GPU scaling for the frozen snapshot further reduces Crop
activation to 9–10 ms. Image/PDF/STL Crop reuses existing preview pixels instead
of capturing a Markdown viewport.
