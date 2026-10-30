# Desktop profiling and possible follow-up work

Status: partially implemented, 2026-10-30. The retained-memory review recorded in
[performance.md](performance.md) implements a first pass of Phase 3 B: a shared
PDF raster allowance, MuPDF store bounds, coalesced memory release, inactive
image/GIF trimming, text-source pruning, fewer bridge copies and Markdown
workspace budgets. Desktop RSS profiling, end-to-end measurements and full
cross-window accounting are still pending; treat the remaining options as
hypotheses, not confirmed bottlenecks. No release or packaging is part of this
plan.

## Questions to answer

1. Does keeping a lightweight container for every page still cause visible
   layout or zoom stalls on long PDFs?
2. Does memory grow excessively across sources, documents and windows despite
   the current per-source raster budget and viewport eviction?
3. Do background raster, OCR or QR jobs delay visible pages or interactive
   previews, and where does that delay occur?

Preserve the current highlight and magnifier responsiveness. Sidebar thumbnails
may lag by several seconds: the user explicitly accepts this tradeoff. The
existing two-second idle refresh is intentional, not a latency regression.

## Phase 1: establish a repeatable baseline

- Record the revision plus any uncommitted diff, Go/native dependency versions,
  OS, CPU, RAM, GPU, display scaling, window size and OCR configuration. Use a
  separate `--data-dir` under ignored `build/` to avoid forwarding launches to
  another running instance. Keep settings and OCR availability identical between
  comparisons; this option does not isolate global theme or the OCR runtime cache.
- Use local real PDFs alongside synthetic fixtures. Record checksums, page count,
  dimensions and document characteristics. Do not commit private documents.
  Include a small control (10–30 pages), a long text/vector PDF (500–1,000 pages),
  an image-heavy scanned PDF, a mixed-size/rotated PDF and a form/annotation-heavy
  PDF. Include appended pages from several independent sources.
- Restart the app for each cold run; distinguish empty app caches from OS file
  cache state. Measure warm revisits separately. Use at least five repetitions
  per scenario; collect enough interaction samples for useful p95/p99 latency,
  rather than calculating tail percentiles from five observations.
- First reproduce any slow interaction without instrumentation. Compare the
  instrumented build against it to quantify capture overhead. Use `v1.4` only as
  an optional reference for equivalent operations; the current implementation is
  the primary baseline and supports different functionality.

| Scenario | Repeatable actions | Main observations |
| --- | --- | --- |
| Open and navigate | Open, scroll continuously for 30 seconds, jump first/middle/last, revisit | First visible page, blank-page duration, frame stalls, canceled work |
| Zoom | Repeat zoom in/out around the same pointer anchor and Fit to Window | Input latency, layout duration versus total page count, stable anchor |
| Highlight and magnifier | Highlight several lines, drag a magnifier for 10 seconds, repeat over existing overlays | Input-to-preview latency, obsolete renders, raster allocation rate |
| Background contention | Repeat navigation and gestures while search/OCR/QR are active | Queue wait versus native render time, UI dispatch delay, foreground latency |
| Forms and history | Type rapidly, edit linked widgets on different pages, Undo/Redo, revisit | Latest-value publication, affected-page renders, typing latency |
| Memory lifecycle | Traverse a long PDF, revisit it, append sources, switch documents, open/close another window | Peak and settled memory, cache reuse, ownership after close |

## Phase 2: collect profiles and attribute the cost

The GUI currently has no dedicated CPU/heap/trace command-line switches. Add
minimal opt-in development instrumentation before attempting GUI profile capture;
do not document unimplemented flags as working commands. Write artifacts under
`build/performance/<run-id>/`, disabled by default. Keep capture bounded and
finalize profiles on normal exit. Avoid logging document text or field values.

Collect separate short captures rather than enabling every expensive probe at
once:

- Go CPU and allocation/heap profiles; compare live retained bytes with cumulative
  allocations. Take snapshots after opening, traversal, document close and window
  close. A forced GC can help diagnose retention in a separate run; do not force
  GC during latency measurements.
- Go execution trace and sampled mutex/block profiles for a reproduced stall.
  Distinguish UI work, source-lock contention, semaphore/queue wait, native render,
  worker completion and publication through `fyne.Do`.
- Native/platform profiling when Go stacks stop at CGO or heap figures do not
  explain process memory: for example Instruments on macOS or perf on Linux.
  Include the OCR subprocess tree and GPU/texture allocations where observable.
  RSS alone is not proof of a leak; allocator retention and driver caches can
  outlive Go objects.
- Correlated request timings: input received, work enqueued, worker started,
  raster complete, UI callback executed. UI callback completion is not proof that
  a frame is displayed; use platform frame capture or a synchronized recording
  for end-to-end latency when available. Label a publication-only measurement.
- Counters for resident pages/thumbnails, pinned pages, pending/active jobs,
  canceled and discarded jobs, raster bytes, cache hits/misses/evictions and
  rerenders. Attribute shared immutable images once by ownership rather than
  adding every reference as a separate allocation.

For every hotspot, report its contribution to the observed stall or retained
memory, its owning subsystem and a reproducible scenario. Separate high CPU from
waiting and distinguish cold rendering from repeated unnecessary rendering.

## Phase 3: choose the smallest justified intervention

### A. Full virtualization of page UI

Proceed if profiles show page containers, layout or refresh traversals dominating
scroll/zoom, and the cost increases with total page count at a fixed viewport.
Current code evicts distant images and dynamic widgets but retains page geometry;
zoom still traverses the page collection.

- Entry points: `pkg/gui/pdfResidency.go`, `zoom.go`, `signatureLayer.go`,
  `signature.go` and the long-PDF benchmarks in `performance_test.go`.
- First remove any remaining avoidable all-page refresh. If that is insufficient,
  prototype a viewport host that mounts only visible/neighboring/pinned page
  widgets, with lightweight geometry or spacers preserving total scroll extent.
  Reuse the page-top index; do not eagerly rebuild every page on scroll.
- Retain stable page IDs, mixed page sizes, fixed gaps, zoom anchors, search and
  keyboard navigation, sidebar selection and reorder. Focus, selection, pending
  drawing and inline edits must survive eviction; define explicit pin/unpin rules.
- Compare 100, 500 and 1,000-page documents with the same viewport. Acceptance:
  widget/layout work scales with mounted pages, tail zoom latency improves and
  navigation/history tests and desktop gestures remain correct.

### B. Shared memory budget across PDF sources and windows

Proceed if repeated-source/multi-window scenarios show disposable rasters account
for material retained memory, or per-source budgets add up excessively. The
current 64 MiB/source limit excludes UI images, native allocations and history.

- Entry points: `pkg/document/raster_cache.go`, `document.go`,
  `preview_renderer.go`, `pkg/gui/pdfResidency.go`, `pdfDetail.go`,
  `searchCache.go` and window/document teardown in `lifecycle.go`.
- Build an ownership inventory first. Address unintended retained references
  before introducing another cache manager. If justified, replace independent
  raster limits with a shared accounting/eviction policy and explicit ownership.
- Prefer evicting invisible thumbnails and unused detail rasters. Keep a useful
  foreground working set and bound in-flight allocations; avoid repeated
  render/evict cycles for one oversized raster. Audit lock ordering and avoid
  global locks held through native rendering or GUI callbacks.
- Never evict original bytes or history artwork as though they were disposable
  previews. Shared images remain immutable; no buffer reuse while referenced.
- Acceptance: disposable retained bytes stay within the documented shared budget
  plus an explicit active/in-flight allowance; closing documents releases owners;
  revisit latency and render counts do not reveal cache thrashing. Report RSS
  separately; a preview-cache budget is not a process-memory cap.

### C. Prioritize foreground rendering over background work

Proceed if visible requests spend significant time waiting behind thumbnails,
prefetch, OCR or QR work. The current two-slot limit applies to initial page and
thumbnail loads per window; it is not a shared scheduler for every native caller.

- Entry points: `pkg/gui/pdfLoads.go`, `pdfDetail.go`, `pdfBasePreviews.go`,
  `thumbnailRefresh.go`, `lensPreview.go`, `ocr.go`, `qrcode.go`, `search.go`
  and source rendering/locks in `pkg/document/document.go`.
- Map actual contention first: per-source native serialization, CPU pressure,
  per-window admission and GUI dispatch require different remedies. Adding
  workers cannot solve a serialized source lock.
- If needed, introduce bounded priority admission at the shared contention point:
  current interaction/visible pages before neighboring prefetch, with sidebar
  refresh and speculative recognition lower. Deduplicate equivalent requests,
  replace obsolete pending work and cancel requests before native entry.
- Already-entered native calls are not currently interruptible. Do not promise
  preemption or remove source locking without proving native thread safety.
  Use bounded aging/fairness so background work still finishes after interaction.
- Acceptance: foreground p95/p99 queue wait falls under the contention scenario,
  active work remains bounded across windows, stale results cannot publish and
  background queues drain after idle. Verify cancellation, fairness and close.

## Validation and completion

Implement and measure one justified change at a time. Keep it only when the
improvement exceeds run-to-run variability and does not materially regress the
control cases. Proposed initial guardrails, to confirm after baseline capture:
target visible feedback within 50 ms p95 for warm gestures on the test machine,
investigate every stall over 100 ms, and investigate control-case regressions
over 10%. These are investigation targets, not established product guarantees.

Run focused regression tests for changed ownership, scheduling or virtualization,
then the checks in [performance.md](performance.md), including relevant race
tests. Recheck real desktop scrolling, zoom, highlight, magnifier, linked forms,
search, Undo/Redo, close/reopen and multiple windows. Preserve sanitized redaction
sampling, painter order, source quality and vector PDF export throughout.

Record results in a dated report linked from `performance.md`:

- Exact baseline/candidate revisions and local diffs, machine, corpus and steps.
- Per-scenario sample counts, p50/p95/p99 latency, peak/settled memory and job/cache
  counters; include variability and profiling overhead.
- Profile artifacts and attribution, selected intervention and rejected hypotheses.
- Before/after desktop evidence, automated checks and remaining limitations.

Stop after profiling if no material bottleneck remains. Full virtualization,
shared memory accounting and scheduling are independent options, not a mandatory
three-part rewrite. Update current behavior documentation only for work actually
implemented; retain this plan's deferred items explicitly for the next session.
