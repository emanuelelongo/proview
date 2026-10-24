# Native Go Mermaid renderer review

Reviewed on 2026-10-24 for Proview's native Fyne Markdown viewer. This is a
feasibility review, not an integration or a claim of full Mermaid conformance.

After this review, Proview integrated the pinned sketch core/raster module.
Current behavior and limits are in [Markdown viewer](markdown-viewer.md).
The observations below describe the review snapshots and sample inputs.

## Recommendation

**tuios.dev/sketch is the strongest adoption candidate in this review.** It is
a Go port of Obsidian's Pele, not an official Mermaid renderer. It is a separate
Go module inside the tuios monorepo; the tuios application is not required.

A narrow standalone fork is reasonable if we need native raster font fallback,
cooperative cancellation or project ownership. First pin and wrap the upstream
module; fork when a concrete required change cannot be provided by its public
API. Preserve the port's relationship to Pele and the source/font notices.

The likely Proview path is:

    Mermaid source → sketch.Render → SVG → raster.Draw → image.RGBA → Fyne

No browser, display backend, Node runtime or temporary image file is needed in
that path. Official Mermaid CLI was used only for review references.

## Source snapshots

| Candidate | Inspected commit | License text found |
| --- | --- | --- |
| [sketch](https://github.com/Gaurav-Gosain/tuios/tree/fed94a1a2d2288c345f6082730c714ce7172c0a9/sketch) | fed94a1a2d2288c345f6082730c714ce7172c0a9 | MIT for Pele and the port; separate font notices |
| [go-mermaid](https://github.com/zkrebbekx/go-mermaid/tree/c07349c3aa2e4e3a0a34fc09d90a29a56e13adc9) | c07349c3aa2e4e3a0a34fc09d90a29a56e13adc9 | MIT |
| [mmgo](https://github.com/julianshen/mmgo/tree/77988c9e285db08eb0f249c927fcdcb0a89370b7) | 77988c9e285db08eb0f249c927fcdcb0a89370b7 | MIT; separate font notices |
| [mermaid-go-renderer / mmdg](https://github.com/bvolpato/mermaid-go-renderer/tree/9eb7db50aafa58d6c427ed847a395ea11b615bbb) | 9eb7db50aafa58d6c427ed847a395ea11b615bbb | Packaging metadata says MIT, but no LICENSE/COPYING/NOTICE file was found in the committed tree |

The mmdg source grant needs clarification before treating it as a fork candidate.
This observation concerns that snapshot, not other revisions.

sketch documents its Pele pin as 8fad23bd51783e4df1204e177a7fb0d2a30dcd13
(v1.8.0). Its golden tests compare against Pele output. That establishes a
porting reference, not equivalence to the official Mermaid implementation.

## Method

- Cloned the sources into an isolated temporary directory. Proview's application
  code and dependencies were not changed by this review.
- Read public APIs, parser/layout boundaries, raster paths, licenses, CI and
  tests. Ran all four upstream Go test suites with -count=1 -timeout 90s -cover.
  All passed after resolving local toolchain/workspace setup.
- Rendered the same **14 valid inputs** through each public API. Checked SVG
  XML parseability and retention of selected expected labels.
- Supplied **three invalid inputs**: empty source, unknown diagram type and
  an unterminated flowchart node.
- Rendered PNG with each native raster API and inspected comparison sheets.
  Reference PNGs came from official Mermaid CLI 12.0.0, resolving Mermaid 12.2.0,
  with existing Brave in a fresh headless Puppeteer profile.
- Also displayed selected generated SVGs in the browser to distinguish layout
  defects from native PNG font limitations.
- Cross-built sketch and go-mermaid probes for Linux AMD64 with CGO_ENABLED=0.
  Those executables were not run on Linux.

Cases cover a return edge, titled subgraph, semicolon syntax, grouped endpoints,
styles, a newer cloud shape, Unicode labels, sequence alternatives/notes, class
inheritance, composite states, ER fields, Gantt sections, mindmaps and YAML
front matter. They are an independent sample, not a complete conformance suite
or a performance benchmark.

## Results

| Candidate | Valid inputs producing SVG | With all checked labels | Invalid inputs rejected | Assessment |
| --- | ---: | ---: | ---: | --- |
| sketch | 14/14 | 14/14 | 3/3 | Best overall starting point in this sample |
| go-mermaid | 14/14 | 13/14 | 3/3 | Clean, smaller architecture; visible fidelity gaps |
| mmgo | 11/14 | 11/14 | 3/3 | Substantial implementation; common syntax gaps and heavier integration |
| mmdg | 14/14 | 12/14 | 1/3 | Successful output can hide malformed or misinterpreted input |

**SVG success and label retention do not establish correct semantics or layout.**
A cloud rendered as a rectangle still passes the label check, and SVG text can
survive while the native PNG contains missing-glyph boxes.

### sketch

Strengths observed:

- Flowcharts, composite states, sequence alternatives, class compartments and
  newer shapes were legible in the inspected output.
- Source, output and flowchart-edge limits are public options. The rasterizer
  has a pixel limit and additional work budgets.
- Native raster output is already an image.RGBA, suitable for Fyne.
- Parsing/rendering are separated; the API provides structured errors,
  theme settings, dimensions and a measurement hook.
- Notices identify Pele and the port. Source includes a script for checking
  changes after its Pele pin.
- The core-plus-raster probe linked only sketch and x/image / x/text outside
  the standard library. Other module requirements support CLI/terminal output
  and were not linked into this probe.

Gaps observed:

- Native PNG uses embedded Arimo / Go Mono fonts. Japanese/Chinese labels in
  the sample became missing-glyph boxes. SVG retained the text, and the browser
  displayed it using system fallback fonts.
- Inspected public raster options do not offer arbitrary font fallback
  injection. A font-provider API would be a concrete fork contribution.
- No cooperative context.Context cancellation was found in the public render
  path. Background execution and ignoring stale results do not cancel work.
- Approximately 96,000 Go lines including tests, with numerous generated
  parser tables. A fork must track the upstream port, not just a drawing helper.
- Visual output intentionally differs from official Mermaid.

The sparse monorepo checkout needed GOWORK=off to test the module independently.
This was checkout setup, not a library defect.

### go-mermaid

Strengths observed:

- Typed stage errors, a separate raster package and small packages per family.
- Fuzz tests, golden images and CI configured for Linux, macOS and Windows.
- Approximately 17,000 Go lines including tests: a compact codebase to own.
- Correctly rejected all malformed sample inputs.

Gaps observed:

- Gantt SVG omitted the checked section label "Native".
- Sequence condition/message labels overlapped. Displaying the SVG in the
  browser reproduced the defect, excluding PNG rasterization as its sole cause.
- New cloud-shape syntax retained the label but produced a rectangle.
- Rasterization manually adds labels and arrows over oksvg; native PNG lacked
  readable CJK glyphs.
- No caller-configurable source/output/pixel budgets or cancellation were found
  on the inspected public render/raster path.

A credible fallback if we want a smaller codebase and accept more layout work.

### mmgo

Strengths observed:

- Separate parser, model, layout, rendering and output packages.
- Explicit Dagre-style layout stages and extensive tests.
- Class/state sample diagrams were readable; malformed inputs returned errors.

Gaps observed:

- Rejected valid single-line semicolon flowcharts, grouped endpoints
  (A & B --> C & D), and the YAML-front-matter sample.
- Titled subgraph displayed its raw "service[Service]" declaration.
- Gantt date labels overlapped, including when the SVG was shown in the browser.
- Native PNG also lacked readable CJK glyphs.
- The probe links a wider canvas/font/typesetting dependency graph. Its canvas
  revision is newer than Proview's current one; adoption needs a separate
  assessment of effects on existing canvas use.
- No caller-configurable source/output/pixel budgets or cancellation were found
  on the inspected public output path.

Its README's full-parity language is not supported by these independent cases.

### mmdg

Strengths observed:

- SVG/PNG APIs, many parser/layout implementations and comparison tooling.
- Some sequence/ER examples were readable.

Gaps observed:

- Unknown types fall back to flowchart parsing. Both an unknown header and an
  unterminated node returned SVG without an error.
- The Unicode/ampersand example was misparsed: "R&D" was not retained as one node
  label and extra nodes appeared.
- YAML title was dropped. Composite-state/class PNGs had visible overlaps.
  Some PNGs also contained a stray arrow near the image origin.
- No source license text was found in the tree, despite MIT packaging metadata.
- No caller-configurable budgets or cancellation were found on the public API.

These issues make it a less suitable base for a viewer that must explain
unsupported input rather than show misleading partial diagrams.

## Adoption or fork plan

1. Use an independent adapter returning an image, dimensions and a typed error.
   Proview owns background jobs, caching and UI.
2. Pin sketch to an immutable revision, disable automatic resource/link loading
   and set conservative source, edge, output and pixel limits.
3. Resolve raster font coverage and report missing glyphs. Browser SVG fallback
   is not evidence that native PNG supports the same characters.
4. Add cooperative cancellation if bounded jobs still affect interactive use.
5. Keep independent cases and official references for each supported feature.
   Unsupported or failed diagrams must retain their source.
6. If forking, keep API additions small and preserve the source layout,
   Pele provenance and upstream tests. Document the update process. An independent
   module can omit terminal/CLI integration without rewriting the rendering core.

The review itself created no remote fork, upstream issue or application change.
The later integration is documented separately above.

## Review workspace

Temporary upstream clones, comparison renders, OCR evaluation files, and
profiling helpers were removed after their findings were recorded. The pinned
source revisions, test method and observed comparison results are summarized
above; no diagnostic files are required by the application.
