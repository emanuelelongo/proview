# Native Markdown viewer

Status: implemented on `feature/native-markdown`; not a published release.
The viewer uses Go and Fyne only. It requires no webview, WPE, Node.js,
browser, container or additional display backend.

## Opening and workspace

Open `.md` and `.markdown` files through File → Open, the command line,
desktop file associations or drag and drop. Markdown shares the document
sidebar with images and STL models. Each Markdown entry gets a cached 320 × 240
preview of its opening content, laid out as a compact light card using the print
renderer. Text keeps a fixed readable size; overflow stops at the first card
boundary instead of shrinking the entire document. The worker renders only that
card, including initial code, tables, images and Mermaid, without capturing the
window. Scroll, zoom, theme and viewport size do not affect it. Switching keeps
the worker alive; closing the entry cancels it and rejects late results.
PDF/non-PDF opening keeps the existing
separate-window policy. Switching documents retains reading size and scroll
position, reusing the same text/layout view rather than rebuilding it. Inactive
views stop animation/render workers; returning restarts them, with a rebuild
only when the reading theme changed. Text runs share application font resources
when their glyph coverage permits, retaining native fallback for other scripts. Closing Markdown restores the previous retained document.

**View → Markdown Theme → System / Light / Dark** selects the reading theme.
The choice is saved in application preferences, applies to all open Markdown
windows and is restored after restarting. System is the default. The preference
affects the reading page, text, code and Mermaid previews independently of the
application chrome and surrounding workspace; thumbnails and printed output keep their light layout.
Syntax token colors are checked against the code background; low-contrast colors
fall back to the reading foreground, including error tokens whose Chroma style
assumes a colored background that the native text renderer does not draw.
Changing theme retains reading zoom and scroll position and clears
text selection. Inactive workspace documents use the saved choice when revisited.

**View → Show Markdown Source** toggles a read-only, monospaced view of the
complete source, including front matter and Markdown syntax. Text wraps to the
reading column; selection, copying, Find, zoom and the reading theme remain
available. Contents is disabled in source mode. Toggling closes Find, clears text
selection and returns to the top. The mode is retained only by the current open
workspace document, including when switching to another document. Closing and
reopening it, opening another file or restarting always defaults to rendered
Markdown; no preference or session setting stores the mode. Printing continues
to use the rendered document.

Markdown is read-only. Crop, annotation, transformation, signature and save
commands are disabled. Source files are never rewritten. Zoom In/Out adjusts
text size from 25% to 200%; Fit to Window restores the default reading size.
The text column reflows up to a 900-point maximum width. A theme-aware page
border, inner padding and contrasting workspace margins distinguish its bounds.
Arrow keys, Page Up,
Page Down, Home and End scroll when no control or dialog owns keyboard focus.

The right-hand `list-tree` icon toggles the **Contents** panel. It starts collapsed
to the same compact column as the editing tools; opening it reveals a translucent
heading panel above the right side of the paper. Clicking the icon again, clicking the Markdown page or
pressing Escape closes it. The reading width, wrapping and scroll position do
not change when toggling the panel. The button stays
at the same position and is highlighted while the panel is open. Its tooltip is
**Show contents** / **Hide contents**. Visibility is retained within the window
across document switching, zoom and theme changes. The floating panel is part of
the normal canvas tree: its list scrolls independently, while the uncovered page
and toolbar remain interactive. It stays within the viewport after resizing,
without resizing/recentering the window or rebuilding the document.

The **Contents** panel replaces the editing tool rail. It includes all
Markdown headings (levels 1–6, including Setext headings) in document order, with
indentation for their level. Click any heading to navigate to it, including the
current heading to return to its start. A colored bar and bold text identify the
current section, following wheel scrolling, keyboard navigation and internal links.
The outline scrolls to keep that entry visible. Duplicate titles remain separate
entries; formatting is reduced to readable heading text. Headings inside fenced
code or literal HTML are not indexed. Documents without headings show **No headings**.

The list renders only its visible rows and locates the current section by binary
search over retained heading positions. Reflow, zoom, theme changes and completed
Mermaid renders update those positions without reparsing source during scrolling.
Switching documents restores the appropriate outline and reading position.

## Supported content

- Headings, paragraphs, nested bold/italic, strikethrough and inline code.
- Complete YAML front matter at the start of a document (`---` through `---` or
  `...`) is excluded from the reading/print tree, while remaining in the copied
  source. Unclosed blocks and separators elsewhere retain ordinary Markdown behavior.
- Docusaurus-style `:::note`, `:::tip`, `:::info`, `:::warning`, `:::danger` and
  `:::caution` containers become labeled callouts with normal Markdown children.
  Optional titles, nested callouts, lists, links and code blocks are supported.
  Callouts have a rounded tinted background, a colored left border and an icon
  beside the uppercase title: green/bulb for tips, amber/warning triangle for
  warnings and cautions, red for danger, and blue/info for notes and information.
  Backgrounds follow the reading theme; custom titles retain the original
  callout category. The selectable Markdown content and link behavior remain
  available inside the panel.
- Ordered/unordered nested lists, task lists, quotations and separators.
- GFM tables with column alignment and inline formatting. Short tables keep
  their natural width; columns share widths across all rows and allocate more
  space to longer content. Text wraps between words and cells align at the top.
  Tables with too many columns or unbroken identifiers wider than the reading
  column scroll horizontally inside their own viewport. Vertical wheel movement
  over a table continues scrolling the document. Links preserve column alignment.
- Fenced code in a rounded, contrasting theme-aware background, with a copy
  button in a reserved right-hand column and no displayed language label. The
  text viewport and horizontal scrollbar stop before that column. The declared language still selects
  syntax highlighting, using Chroma and palettes
  for the current light/dark theme. Unknown languages remain plain code.
  Code overflow scrolls horizontally inside its block, with flat edges and no
  scroll-edge shading. Vertical wheel/trackpad
  movement over code continues scrolling the document.
- Mermaid diagrams rendered natively as images, with an always-visible compact
  code-copy button. Successful previews do not show a source section.
- Relative local raster images; missing/remote images show their alternative text.
- Animated local GIFs play while visible, including in the enlarged image overlay.
  Offscreen GIFs pause; repeated uses of the same resource share a player. Switching,
  rebuilding or closing the view stops its clock and rejects obsolete updates.
  Printing and thumbnails retain the immutable first frame.
- Static HTML image tags, standalone or inside simple `p`/`div` wrappers, with
  `alt`, numeric pixel `width` and left/center/right paragraph alignment. Basic
  `strong`/`b`, `em`/`i` and `br` tags in these paragraphs are supported. Static
  `a` links preserve their text, formatting and image children; linked images
  retain the enlarged-preview interaction. Link-only HTML paragraphs also support
  inline anchors; mixed Markdown formatting and inline HTML anchors remain literal.
  HTML is
  converted into the same immutable Markdown tree for reading, printing and
  thumbnails. Source copying remains unchanged. HTML image paths starting with
  one slash (such as `/assets/logo.png?raw=true`) resolve beneath the document
  directory, rather than the filesystem root. Images remain local and subject
  to the existing path, symlink, decoding and memory limits.
- HTML `<details>` / `<summary>` disclosures. Click the chevron or summary to
  expand/collapse. Disclosures use a rounded cyan-tinted panel with a thin
  border and, when expanded, a divider between the summary and body; colors
  follow the reading theme. The `open` attribute sets the initial state; omitted summaries
  use **Details**. Summary markup is reduced to readable text; multiline bodies
  retain ordinary Markdown, including nested disclosures, lists, code, links,
  local images and callouts. Compact single-line HTML disclosures support the
  same static HTML subset as other HTML blocks. Open state is local to the open
  workspace document and survives switching, zoom and theme changes. Reopening
  starts from the source's `open` attributes. Collapsed bodies are excluded from
  selection, Find and Contents until expanded. Toggling clears selection and
  recomputes Find. Printing and thumbnails include all disclosure contents.
- Clicked HTTP/HTTPS/mailto links, relative document links and heading anchors.
  Link labels wrap with the paragraph, including long unbroken URLs, and retain
  clickable byte ranges and text selection across wrapped lines.

Click a loaded image (including HTML images) or a rendered Mermaid diagram to
show a temporary enlarged overlay using its original decoded pixels. It has no
native window frame, title bar or buttons. The editor reuses one preview image
object and scales it on the GPU where available; closing clears the source and
document references rather than retaining another image for each click. The viewport fills the available window
with a 24-unit dismissible margin on each side, without a fixed maximum size.
Click outside the image or press
Escape to dismiss it; losing focus also dismisses it. The dismissing click is
consumed, so it cannot follow a link or activate a tool underneath.

Use Command/Control + mouse wheel to zoom around the pointer and drag to pan;
ordinary scrolling remains available. `+`/`-`, `0` (Fit) and `1` (100%) also work
from the keyboard, with or without the platform modifier. Fit follows window
resizing, with the new scale set before child layout. Manual zoom preserves the
viewed center during resize, subject to image-edge clamping; repeated identical
sizes do no work. Reading zoom, position, files and session entries stay unchanged;
switching/closing the source document closes the overlay. Links, loaded images
and rendered diagrams show a hand cursor on hover; ordinary text keeps its
selection cursor.

Mermaid uses the pinned sketch renderer described below. If a diagram is invalid,
unsupported or exceeds its limits, a message is shown and its original source
is displayed automatically. Following Markdown remains visible. Formulas display their
source text. Unsupported HTML tags and blocks display literally; scripts, styles
and embedded content are never executed. Remote images are not fetched.

Drag to select rendered text across paragraphs, headings, table cells and code
blocks. Double-click selects a Unicode word, including across inline formatting;
a third click at the same position selects the rendered line within that text
block or table cell. Clicking text returns keyboard focus from Find to the page. Formatting and syntax colors remain visible. Edit → Copy /
Command/Ctrl+C copies the selection, preserving code whitespace; without a
selection or focused text field it copies the full Markdown source. Right-click
text and choose **Copy text** to copy the selection, or the whole block when
nothing is selected. Code blocks retain their complete-source copy button.
Escape, zoom and document switching clear the text selection.
Links remain clickable and diagrams keep their image context menu.

**Edit → Find** / Command/Ctrl+F searches rendered text, including headings,
paragraphs, link labels, table cells, code and strikethrough text. Matching is
case-insensitive with Unicode NFC and whitespace normalization; phrases can span
adjacent text blocks. Hidden front matter, Markdown syntax and text inside images
or rendered diagrams are excluded. There is no OCR or source-file modification.
Enter/F3/Command-or-Ctrl+G advances; Shift reverses direction. Results wrap and the
current match is highlighted independently of text selection. Navigation reveals
matches below the viewport and inside horizontally scrolled code/tables.
Escape closes Find without discarding the user's text selection. Zoom/theme
rebuilds recompute results and retain the current match index. Search runs after
180 ms of idle typing in a cancellable worker, rejects obsolete results after
query changes or closing/switching documents, and caps results at 10,000.

Right-click a rendered diagram and choose **Copy image** to copy its PNG at the
rendered resolution. Markdown Crop has been removed, including capture jobs,
selection overlays and diagram-layout deferral. Mermaid updates publish normally.

The reading page uses a slightly lighter shade of its selected reading theme
(white in Light mode). The surrounding workspace always keeps the application
background color, independently of the Markdown theme.

Prose and table cells retain their full layout, but text farther than one viewport
above or below the visible region is omitted from the renderer tree. Scrolling,
reflow and diagram layout update that region; returning to a block reuses its
retained text and selection geometry. This avoids Fyne's native per-frame traversal
and measurement of every offscreen wrapped line. Code panels retain independent horizontal scrolling. Individual text lines and
selection rectangles are culled too, including within a single long paragraph or
syntax-highlighted code block. Card contents retain page coordinates just like
prose and tables, so offscreen code remains selectable and programmatic navigation
reveals its destination. This is rendering culling, not a document memory cap.

Fyne can evict idle text renderers together with their local theme overrides.
When recreating a renderer directly for painting, as well as before refreshing
or reflowing retained text, the viewer checks its embedded
RichText theme and restores the reading scope if its colors or font size have
fallen back to the application theme. Renderer construction also handles the
refresh triggered by restoring that scope before it is cached. This avoids white text on a light reading
page after idle time/navigation in a dark application. Theme restoration runs
only when the scope differs, without refreshing the complete document on scroll.

Selection hit testing reuses fragment geometry until layout/theme refresh and
measures byte offsets only in the nearest fragment. A drag resolves the viewport
origin once, updates only changed selection ranges and queues one canvas repaint.
The selection renderer caches minimum size and skips repeated layouts at identical
sizes; reflow and refresh invalidate those caches. This prevents newly visible
lines or selection rectangles from repeatedly laying out the entire code block.

**File → Print…** prints the whole Markdown document using a separate light A4
layout, including syntax-colored code, local images and Mermaid. The output does
not depend on the reading zoom or visible viewport. See [Printing](printing.md)
for pagination rules and verification status.

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

Static images have a 64 MiB aggregate decoded budget.
Pre-decode checks reserve eight bytes per pixel for 16-bit formats; retained
static buffers are charged at their decoded representation. GIFs additionally share a
128 MiB animation budget per document, checked before decoding their frame arrays;
the estimate includes palette buffers and compositing images. The shared GIF
decoder limits logical screens to eight million pixels and animations to 2,000
frames. Animations exceeding these limits remain static. Frame offsets,
transparency, disposal, delays and repeat counts are preserved; delays below
20 ms use 100 ms. Decoding happens in the opening worker, never on a playback tick.

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
- GIF regressions cover offset/transparency/disposal composition, delays, repeat
  counts, pause/resume, immutable frames, truncation and allocation limits. GUI
  tests cover direct opening, editing/Undo, switching/closing, linked/repeated
  Markdown GIFs, offscreen pause, enlarged playback and theme teardown. Repository
  demo GIFs fit the shared animation budget.
- GUI tests cover queued opening, workspace switching and closing, read-only
  controls, zoom/scroll restoration, heading links, keyboard navigation and cached
  opening-content thumbnails independent of canvas capture and reading state,
  background completion after switching and closed-document callback rejection.
- Preview layout tests cover a fixed single-card boundary for long prose, code
  and tables, retained opening headings, omitted tail content and cancellation.
- Mermaid adapter tests cover valid output, cache reuse/eviction, theme and zoom,
  source/edge/image budgets, cancellation and concurrent callers.
- Mermaid GUI tests cover asynchronous rendering, theme/zoom/viewport changes,
  source fallback, preservation of following content, late-layout scroll restoration
  closing during work, exact code copying and image clipboard output.
- Outline tests cover all six heading levels, formatted/duplicate titles, actual
  pointer navigation, wheel/keyboard/anchor tracking, reflow and Mermaid completion,
  theme/workspace restoration, empty documents and a virtualized 400-heading list.
  Crop is disabled in the Markdown toolbar and menu; image Crop remains available.
  Overlay tests cover the compact default, fixed button position, translucency,
  unchanged reading width/text geometry/offset, independent scroll input, Escape,
  retained section tracking while hidden and visibility across rebuilds and
  workspace switching, without recreating document widgets or resizing the window.
  The outline and its current-section marker were visually inspected in a
  software-rendered frame. A native Linux/X11 probe on the reported private
  document verified current-section tracking and outline navigation; 100 scroll
  steps with 16 ms scheduled pauses completed in 1.76 seconds.
- Text-selection tests cover styled/wrapped paragraphs, cross-block and reverse
  drags, replacing selection ranges, UTF-8 hit testing, exact code whitespace,
  copy precedence and clearing. A 300-line highlighted-code regression checks
  offscreen selection/copy, line/highlight culling, navigation, resize and cached
  hit-test invalidation, plus allocation-free repeated layout/measurement/traversal
  at the same size. See [performance measurements](performance.md) for profiling
  and reproducible selection benchmarks.
- A long-table regression checks offscreen text exclusion, scrolling to both ends,
  programmatic jumps, stable page height, retained cross-block selection and
  reflow after resizing.
- Table layout tests cover compact tables, shared content-based column widths,
  wrapped cell heights, narrow/wide reflow, light/dark themes and reading zoom.
  Overflow tests cover independent scroll axes, link alignment and selection
  coordinates after horizontal scrolling. Software-rendered table previews in
  both themes were visually inspected.
- Formatting regressions cover hidden front matter and preserved source offsets,
  nested admonitions with literal code fences, print/thumbnail content, and
  clickable wrapped links within narrow light/dark reading columns.
- Theme tests cover persisted preferences, checkmarks across windows, newly
  opened windows, preserved reading state and Mermaid palette changes.
- Fyne software-rendered frames were visually inspected for prose, tables,
  strikethrough, code highlighting and flowchart/sequence previews in both themes. These do not establish native desktop
  input, accessibility or Linux/Wayland runtime verification.

Run `go test ./internal/markdown ./pkg/gui -run 'Test(Load|LocalImages|Markdown|Mermaid)'`.
For a manual trial, open `internal/markdown/testdata/reading.md` with the normal
application. Use its standard build instructions; there are no Markdown build
flags or extra runtime dependencies. Native Ubuntu 24.04/GNOME verification is
still pending.

On 2026-10-27, a temporary native Linux/X11 scroll probe using the reported
`probe.md` (about 10 KB, with long table cells) took 6.23 seconds before rendering
culling and 1.91 seconds afterwards for 100 scheduled scroll steps. Both runs
included a 16 ms pause per step. CPU profiling identified native renderer-tree
traversal and minimum-size checks as the principal work. These are local scripted
scroll timings, not a manual input or frame-rate benchmark; the private document
and temporary profiling launcher are not repository fixtures.

After content-based table sizing, the same native scroll probe completed in
1.78–1.91 seconds across two runs. Column widths and wrapped row heights are cached across scroll
frames and recomputed on available-width or theme/font-size changes. Table
overflow reuses code blocks' axis routing and keeps offscreen text culling active
inside the horizontal viewport. Code and table viewports reserve scrollbar space
only when their content overflows horizontally. Culling reads the current scroll
offset while building the render tree, including programmatic navigation, which
does not reliably trigger Fyne's `OnScrolled` callback.

## Deferred work

In-document search, remote-image opt-in and live reload are not implemented. Mathematical rendering and HTML layout remain
future decisions. This replaces the discarded webview probe.

The [native Mermaid renderer review](mermaid-renderers-review.md) records the
source examination and comparisons that led to this integration.


## Rendering responsiveness history

On 2026-10-24, a native macOS investigation and CPU profile identified the causes
of rendering delays when opening Markdown documents with syntax-highlighted code
and Mermaid diagrams. The investigation started with the former Crop command;
that command and its capture-specific workarounds were removed on 2026-10-27.
The general rendering improvements remain:

- Diagrams were serialized behind a single global rendering slot (`renderSlot`),
  causing sequential diagram queues and chained layout reflows; parallelizing
  diagram rendering to four concurrent bounded slots with single-flight request
  coalescing removes the worker serialization.
- Each completed diagram previously called `v.content.Refresh()` and
  `v.scroll.Refresh()`, which re-applied `ThemeOverride` (clearing Fyne's global
  font-measurement caches) and recursively refreshed and evicted textures across
  every rich-text block in the document. `markdownPage` and `markdownText` now
  keep a single page-level theme scope, cache `MinSize()` per wrap width, coalesce
  adjacent same-color Chroma tokens on each line, and relayout diagram height
  updates without resetting unchanged text blocks.
