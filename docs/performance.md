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
