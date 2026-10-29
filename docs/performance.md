# Interactive performance

Current implementation after the October 2026 performance work. No release or
packaging is implied. Existing document bytes, editable annotations and export
quality remain independent of disposable preview caches.

Deferred desktop profiling and possible architectural follow-ups are recorded in
[the profiling plan](performance-plan.md). They are hypotheses to measure, not
implemented changes.

## Invalidation and rendering

- `Document.SourceRevision` changes for source/geometry, page collection, form
  values and sanitation changes, including Undo/Redo. Ordinary overlay history
  changes only the general revision. Source revision is copied into rendering
  snapshots; it is runtime state, never editor history or PDF metadata.
- `BasePageKey` identifies source, page index, rotation and changed fields on that
  source page. Linked widgets invalidate every affected page. Overlay edits and
  redactions do not change the base raster; redaction overlays still paint above
  objects, and raster derivatives retain actual sanitation.
- OCR and QR use bounded caches keyed by source content. They no longer scan the
  entire page collection to prune twenty cached entries after each annotation.
  Sanitation changes invalidate recognition globally, including shared images.
- Enhanced page previews survive annotation changes. Field and native text edits
  share one coalesced base-preview worker; only changed resident pages render.
  Stale results are checked against page content keys, not unrelated overlay edits.
- Source field metadata is published once, indexed by page and independent of the
  raster mutex. Logical page sizes have a separate read lock, so UI metadata
  requests do not queue behind an expensive native render.

## Long documents and memory

- Page tops are indexed after zoom/layout. Viewport and recognition-page lookup
  use binary search; programmatic navigation explicitly loads the new viewport.
- The editor retains visible pages, two neighboring pages on either side, and
  pages pinned by selection, focus or active editing. Distant images and dynamic
  annotation previews are released and regenerated when revisited. Sidebar
  raster residency is independent of editor residency. Lightweight page geometry
  stays in place, preserving scrollbar size and zoom anchors.
- Ordinary source previews/thumbnails share a 64 MiB LRU budget per PDF source.
  Oversized individual rasters may render without being retained in that cache.
  Original bytes, history artwork, active UI images and the native renderer's own
  allocations are outside this budget; this is not an application-wide RSS cap.
- Native page/thumbnail loading admits at most two calls per window. Waiting
  requests leaving the viewport are canceled; already-entered native calls may
  finish, and obsolete results cannot repopulate an evicted view.
- Edited thumbnails remain eventual: one update at a time at 72 DPI after two
  seconds of editing inactivity. First visible-thumbnail loading stays lazy.

## Search

- Search identity uses the source revision, avoiding a full-document key rebuild
  for each progress callback. Only the current search highlight is refreshed;
  unchanged matches do not rebuild or refresh every page layer.
- Successive queries reuse immutable layouts with a 128-page / conservatively
  accounted 16 MiB LRU. Visible-page OCR results prime this cache. Geometry and
  sanitation keys prevent stale layouts from reaching a different source state.
  Closing Find releases its index and document reference. Search matching,
  cancellation, debounce and result limits remain unchanged.

## Verification and measurements

```sh
go test ./...
go vet ./...
go test -race ./pkg/document -run 'TestSource|TestBasePage|TestEvictedPDF|TestPreviewRendererConcurrent'
go test -race ./pkg/gui -run 'TestBasePreviews|TestCanceledPDFLoads|TestPDFDetailSurvives|TestPDFResidency|TestSearchIndex|TestSearchTextCache'
go test ./pkg/gui -run '^$' -bench BenchmarkLongPDF -benchtime=50x
```

Synthetic Linux/AMD64 measurements on an Intel i7-13700H (2026-10-19):

| Operation on 1,000 pages | Previous approach | Current approach |
| --- | ---: | ---: |
| Search/source identity check | 216 microseconds; 155 KB, 2,506 allocations | 0.29 microseconds; 73 B, 1 allocation |
| Visible-page lookup | 27 microseconds | 0.29 microseconds |
| Zoom layout, empty page layers | 0.85 ms; 12,601 allocations | 0.52 ms; 7,825 allocations |

These compare isolated old algorithms with their replacements, not end-to-end
application latency or GPU frame times. Automated regressions cover eviction and
reload, stable page geometry/history, pinned selections, obsolete load rejection,
linked fields, newest-value publication and reuse across searches. Real document
scrolling, display scaling and desktop memory behavior still need manual review.

## Markdown selection and scrolling (2026-10-27)

A native Linux/X11 CPU/heap investigation reproduced selection-related stalls
against `a691e28`, using the reported private `probe.md` (272 text blocks), a
1,681-block table/prose stress document and a 700-line highlighted Go block.
A temporary launcher used the actual GLFW application at 1000 × 740 logical
units, without changing normal preferences or copying document contents into the
repository. Five separate phases exercised idle, scroll, selection drag, scroll
with a selection and scroll after clearing it. Each phase scheduled 120 UI-thread
operations with 16 ms pauses; timings include UI queue latency and profiler
overhead, and are not measured display frame times.

The profiles identified:

- Selection refreshed every initialized block on each pointer event and repeatedly
  walked the canvas tree to resolve the same viewport origin. Native Fyne thread
  checks made these redundant refreshes particularly costly.
- Every renderer traversal allocated a combined text/highlight object slice.
  Hit testing rebuilt all fragment geometry and shaped prefixes in successively
  closer fragments rather than just the nearest fragment.
- Long code inside a Card lacked retained page coordinates. Its individual lines
  and highlight rectangles were traversed even when far outside the viewport.
  Newly visible child nodes also caused repeated same-size RichText layouts,
  reshaping/repositioning all code and reconstructing its selection highlights.

The viewer now caches selection geometry/minimum sizes until layout or refresh,
resolves page origin once per drag, updates changed ranges, queues one repaint,
reuses render slices, culls individual lines/highlights and skips unchanged-size
layouts. Full source and offscreen geometry remain available for selection/copy.

Representative native results on the same i7-13700H Linux/AMD64 machine:

| Operation | Before | After |
| --- | ---: | ---: |
| Private probe: selection event p95 | 8.54 ms | 1.55 ms |
| 700-line code: selection event p95 | 69.72 ms | 7.41 ms |
| 700-line code: scroll with selection p95 | 23.45 ms | 11.48 ms |
| 700-line code: allocated during selection phase | 346.36 MiB | 15.78 MiB |
| 700-line code: allocated during selected-scroll phase | 211.68 MiB | 28.27 MiB |
| 700-line code: selected-scroll sampled CPU | 2.21 s | 1.16 s |

The 1,681-block synthetic table/prose case still reaches roughly 15–17 ms in the
upper latency tail: native traversal of the many remaining structural containers
is still material there, even though median drag latency fell from 4.69 to
0.36 ms. These fixes do not promise a fixed frame budget for arbitrarily large
Markdown documents.

CPU/heap/allocations were captured with `runtime/pprof`; allocation totals include
profiling overhead. Heap was sampled after explicit GC between phases, separately
from cumulative allocation churn. In eight further select-all/scroll cycles,
retained Go heap stayed between 57.635 and 57.644 MiB; process RSS varied around
234–235 MiB, including native fonts/OpenGL resources. This run found allocation
churn and redundant UI work, without evidence of continuing heap growth in that
scenario. Cached selection geometry trades some retained memory for avoiding
repeated allocations. This does not establish a universal memory cap or exclude
leaks in other documents/backends; native macOS/Wayland verification is separate.

The repository benchmark isolates drag hit testing and the repeated
layout/minimum-size/render traversal of a selected 300-line highlighted block,
without a sidebar thumbnail worker. Two one-second runs gave:

| Isolated operation | Before | After |
| --- | ---: | ---: |
| Selection drag | 2.62–2.70 ms/op; 1.88 MB/op | 0.127–0.129 ms/op; 1.7 KB/op |
| Unchanged selected frame | 2.16–2.21 ms/op; 1.05 MB/op | 0.023 ms/op; 0 allocs/op |

These are software-driver component benchmarks, not end-to-end GPU frame rates.
Regressions cover selection replacement, culled code navigation/copy, highlights,
UTF-8 hit testing after resize and allocation-free unchanged selected layouts.

```sh
go test ./pkg/gui -run 'TestMarkdown.*Selection|TestMarkdownTable' -count=1
go test -o /tmp/proview-md-selection.test ./pkg/gui -run '^$' \
  -bench '^BenchmarkMarkdownSelection$' -benchmem -benchtime=2s \
  -cpuprofile /tmp/proview-md-selection.cpu \
  -memprofile /tmp/proview-md-selection.heap
go tool pprof -top /tmp/proview-md-selection.test /tmp/proview-md-selection.cpu
go tool pprof -alloc_space -top /tmp/proview-md-selection.test /tmp/proview-md-selection.heap
go tool pprof -inuse_space -top /tmp/proview-md-selection.test /tmp/proview-md-selection.heap
```

## Markdown image loading (2026-10-28)

Local images decode through a bounded buffered file reader instead of retaining
an additional copy of the compressed file. Header inspection precedes decoding;
the remaining 64 MiB document budget is checked before allocating the raster.
Accounting conservatively reserves eight bytes per pixel to include 16-bit PNGs,
so image-heavy documents can reach the limit earlier than under the former
four-byte estimate. Repeated failed destinations are attempted only once per
load. Resource confinement, the 16 MiB file limit and eight-million-pixel limit
remain in effect. This budget excludes decoder scratch space and UI resources.

Two Linux/AMD64 i7-13700H runs of `BenchmarkLoadMarkdownImage`, loading one
1024 × 1024 opaque PNG with deterministic random pixels through `Load`, measured:

| Metric | Before | After |
| --- | ---: | ---: |
| Load time | 10.97–11.20 ms | 8.82–8.93 ms |
| Allocated bytes per load | 11.32 MB | 4.28 MB |
| Allocations per load | 283 | 253 |

Fixture generation is outside the timed region. These are isolated warm-file
loading measurements, not process RSS, peak memory or desktop frame timings.

```sh
go test ./internal/markdown -run '^$' -bench BenchmarkLoadMarkdownImage -benchmem -count=2
```

## PDF glyphs, workspace ownership and raster transforms (2026-10-28)

- Native PDF glyph layouts previously accumulated for every visited page in
  `Source.textLayouts`, independently of the bounded GUI search cache. They now
  use a source-local LRU limited to 128 pages and a conservative 16 MiB (including
  glyph slice capacity). Oversized layouts are returned without caching. Eviction
  preserves source bytes, selection geometry and caller-owned copies; revisiting
  an evicted page extracts its geometry again. This is not a global memory cap.
- Removing documents and editor windows now clears the unused slice tail.
  Otherwise closing the last entry could retain its document, history, thumbnails
  or canvas through the backing array while other entries remained open.
- Raster rotation/mirroring uses typed pixel access instead of allocating boxed
  colors per pixel. It serves camera orientation, rotated PDF previews, QR rasters
  and overlay transforms. Unsupported image implementations retain the generic
  path. Regression tests compare exact pixels against generic conversion across
  all turns/mirrors, transparent colors, 16-bit pixels and offset image bounds.

Isolated 90-degree rotation measurements on Linux/AMD64 i7-13700H, three
iterations per case, with zero-initialized 2048 × 1536 source images:

| Raster | Before | After | Allocated bytes before → after |
| --- | ---: | ---: | ---: |
| RGBA | 67.2 ms | 28.0 ms | 25.2 → 12.6 MB |
| NRGBA | 64.4 ms | 23.1 ms | 25.2 → 12.6 MB |
| JPEG YCbCr 4:2:0 | 101.6 ms | 37.9 ms | 37.7 → 12.6 MB |

Allocations fell from 3.1–6.3 million to two per operation. The remaining
allocation is essentially the output raster. Timings depend on source pixels,
format and system load; these are not end-to-end opening or scrolling timings.

```sh
go test ./pkg/images -run '^$' -bench BenchmarkTransformLargeImage -benchmem -benchtime=3x
go test -race ./pkg/document -run 'TestSourceTextCache|TestTextGeometry|TestPreviewRendererConcurrent'
```

Tests also cover byte/page eviction limits, recent-page retention, regeneration
after eviction, caller mutation isolation and cleared workspace/window references.
The STL loader and existing PDF raster residency were inspected without changes;
this pass did not establish a new measured bottleneck in those paths.

## Markdown workspace font reuse (2026-10-29)

A software-driver probe opened the reported swarm migration, operator migration
README and gateway migration Markdown files, captured each opening viewport,
then made three full cycles through their workspace entries. Heap profiles used
`runtime/pprof`; `runtime.MemStats.HeapAlloc` was sampled after GC at each phase.
The private files were read locally; neither their contents nor absolute paths
are test fixtures. Embedded decoded images accounted for approximately 83 MiB.

The dominant growth was parsed font data: Fyne keys ordinary font faces by theme
scope, so newly constructed Markdown scopes created additional copies. Switching
also rebuilt each entire reading view. Retained Go heap after opening all three
was 203 MiB, then 272, 342 and 411 MiB after successive cycles. Sharing explicit
font resources for text runs covered by the selected font reduced duplication;
retaining each workspace entry's reading view then bounded repeated switching.
The equivalent combined run measured 157 MiB after opening and approximately
158 MiB after each of the three cycles. These are Go heap figures, not native
process RSS or a universal memory limit; transient decoding, graphics resources,
font fallback and Fyne caches remain additional costs.

Rendered text uses shared font resources only when that font covers the entire
run. Unsupported scripts/emoji retain Fyne's native fallback. Views suspend GIF
playback and cancel Mermaid work while inactive; resuming restores the outline,
clears old selection and requests diagrams in a fresh context. An inactive view
rebuilds if the saved reading theme changed. Closing an entry clears its retained
view reference. No forced GC, process memory limit or private Fyne cache API is
used in the application.

Regression tests cover shared font identities/coverage fallback, repeated
workspace switches reusing the same blocks, inactive-theme invalidation and the
existing selection/layout/renderer-eviction paths. The GUI suite also checks PDF
page/thumbnail residency. PDF/image source caches already have their separate
bounds; this investigation did not measure equivalent native RSS workloads for
those formats and does not establish a global cap for them.

## Enlarged Markdown image previews (2026-10-29)

A separate software-driver probe used a shared 3000 × 2000 NRGBA image and ten
open / fit / 100% / 200% / close cycles, capturing the window after each scale.
After GC, retained Go heap increased by approximately 0.06 MiB before and 0.01 MiB
after the change. It did not reproduce a persistent Go-heap leak. Cumulative
allocation churn fell from approximately 7,929 MiB to 3,276 MiB, including the
software driver's capture/resampling buffers. These totals are not peak RAM or
native desktop/GPU measurements.

The overlay now uses `ImageScaleFastest`, like the main image viewer, allowing
GPU scaling without the additional software downsampling buffer. Each editor
reuses one preview/`canvas.Image` identity across repeated openings and document
switches, instead of leaving a separate native texture-cache entry for every
click. Closing clears its source image, animation, document and focus references
and refreshes the image before removing the overlay. Source pixels remain owned
by the Markdown document; no duplicate decoding is performed. Native texture
residency still follows Fyne's cache lifecycle and was not profiled in this
software-driver test.

Regression tests cover image identity reuse, cross-document replacement, cleared
references on close, zoom/pan/fit/focus handling and animated image playback.
