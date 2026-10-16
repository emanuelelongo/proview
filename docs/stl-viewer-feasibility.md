# STL viewer feasibility

Assessment date: 2026-10-10. Historical design study and initial measurements.
The viewer has since been implemented; use [STL viewer prototype](stl-prototype.md)
for current behavior and remaining work. Recommendations below describe the
original proposal rather than an active implementation backlog.

## Recommendation

An interactive STL viewer is feasible within the current Go/Fyne application.
Start with a dedicated, read-only 3D mode, a validated ASCII/binary STL loader,
and a CPU renderer that publishes images to Fyne. A GPU backend can be considered
if measurements on real models and destination platforms justify it.

The existing image/PDF editor should keep its current document model. An STL
mesh needs its own camera and geometry state, plus explicit command availability.
Adding only a `FormatSTL` constant would make existing image editing commands act
on a model incorrectly.

## First version

- Open `.stl` in either ASCII or binary representation.
- Center the model and fit it to the viewport, initially in an isometric view.
- Drag to orbit; scroll or use zoom buttons to zoom.
- Hold Space and drag to pan, consistent with the image/PDF viewer.
- Offer Fit, Reset and front/top/side/isometric views.
- Display a shaded solid, with wireframe as an optional view mode.
- Show triangle count and bounding-box dimensions in model coordinates.
- Reuse Open, Close, independent windows and file-manager opening behavior.
- Keep Save, document annotations, OCR, signatures and PDF page commands disabled
  for models. Camera changes do not make the file dirty.

Snapshot export to PNG could follow separately. Mesh editing, slicing, print
preparation and mesh repair are beyond the initial viewer scope.

STL describes triangles and has ASCII and binary variants, with no standard
appearance model. Color extensions are inconsistent, so use a single configurable
material initially. See the Library of Congress descriptions of the
[format family](https://www.loc.gov/preservation/digital/formats/fdd/fdd000504.shtml)
and [binary STL](https://www.loc.gov/preservation/digital/formats/fdd/fdd000505.shtml).
Do not label dimensions as millimeters without an explicit unit choice.

## Fit with the current application

Repository inspection found these integration points:

- `pkg/document/document.go`: the current loader creates raster pages or PDF pages.
- `pkg/document/model.go`: edit history, page references and overlays belong to
  the existing image/PDF model.
- `pkg/gui/menu.go`: opening currently yields only `*document.Document`.
- `pkg/gui/history.go`: most command availability depends on `document != nil`;
  several commands assume that every non-PDF document is an image.
- `pkg/gui/lifecycle.go`: Close and unsaved-change handling target the current
  image/PDF document.
- `pkg/gui/zoom.go` and `pan.go`: zoom and pan currently operate on a 2D scroll area.
- `Taskfile.yaml` and the Linux desktop entry: file associations need an STL entry
  after the viewer is actually implemented.

Proposed organization:

- `internal/model3d`: validated geometry, STL loading, camera math and rendering.
- `pkg/gui/modelViewer.go`: the Fyne viewport and model navigation controls.
- Keep a separate active mesh/viewer state in each editor window. Add shared
  checks for whether any content is open and which commands it supports.
- Route opening to the appropriate loader and publish the completed result only
  after loading succeeds. Reuse the existing dirty-document confirmation.
- Apply command checks to keyboard shortcuts and action functions as well as
  menu items. Close must clear either kind of content and leave the window ready
  to open another file.

Fyne 2.7.4 already provides `canvas.Raster` and `canvas.Image`; a worker can render
an `image.Image`, then publish it through `fyne.Do`. Keep heavy rendering out of
Raster's generator and UI callbacks. See the
[Fyne Raster API](https://docs.fyne.io/api/v2/canvas/raster/), also verified against
this repository's installed Fyne version.

During interaction, use a bounded resolution and coalesce camera updates. After
release, render at the final viewport resolution. One rendering job per viewport,
immutable published images and a generation check prevent stale frames from
appearing after another view is requested or the model is closed. A renderer that
cannot interrupt an in-flight frame needs bounded workloads; discarding the
result does not stop its CPU work.

## Renderer experiment

[FauxGL](https://github.com/fogleman/fauxgl) is a pure-Go software renderer with
triangle rasterization, depth testing, lighting and wireframe support. Its project
uses the MIT license. It is a useful candidate/reference, but the unmodified
revision tested here is not ready for direct integration.

The study ran outside the repository in a temporary Go module, using FauxGL
revision `abf826acbbbdd68114b32f9a6b7e6db1bd79d45a` and its `fogleman/simplify`
dependency. No application dependencies or source files were changed.

Two issues were reproduced:

1. A synthetic sphere render took more than a minute without completing its
   first measured group of frames. A stack dump showed rasterization workers
   still iterating in the pixel loop. Removing the scanline start optimization
   in the temporary copy allowed the same workload to complete promptly.
2. An incomplete ASCII facet with one vertex caused an index-out-of-range panic
   in the STL loader. Source inspection also showed ignored numeric parse errors.

A first attempt to guard the scanline calculation completed quickly but left
visible holes in the sphere. It was discarded. The measurements below use the
second experiment, which removes that optimization and scans each triangle's
pixel bounding box. The resulting sphere image was visually inspected and did
not show the earlier holes. This is a feasibility experiment, not a validated
production patch.

Environment: macOS ARM64, Go 1.25.6, 10 logical CPUs. Geometry: generated
icospheres, serialized to binary STL and loaded back. Rendering: Phong shading,
back-face culling, three camera orientations, no supersampling. Times are the
median of three frames and include clearing the buffers and drawing. They exclude
Fyne texture upload, UI handling, PNG encoding and initial context allocation.

| Triangles | Binary STL | Load time | Render at 600×400 | Render at 1000×700 |
| --- | --- | --- | --- | --- |
| 20,480 | 1,024,084 bytes | 4.5 ms | 3.9 ms | 8.1 ms |
| 81,920 | 4,096,084 bytes | 13.0 ms | 4.4 ms | 8.9 ms |
| 327,680 | 16,384,084 bytes | 54.0 ms | 12.2 ms | 16.1 ms |

These results support trying a CPU viewer. They do not establish interactive
frame rates in Proview, performance on other machines, or behavior on complex
real-world models. The upstream renderer's own performance discussion is also
not a substitute for application measurements.

## Loader and rendering requirements

Use a bounded, validated loader with an `io.Reader` interface. A small internal
STL parser is a reasonable choice; alternatively, audit an existing parser such
as [russoj88/stl](https://github.com/russoj88/stl) before adopting it. That parser
was reviewed as an option but was not benchmarked or integrated.

- Detect binary STL from its header, triangle count and file length; a binary
  header may begin with `solid`, so that word alone cannot distinguish variants.
- Check counts and size limits before allocating, including integer overflow.
- Reject truncated facets, invalid numbers and non-finite coordinates.
- Handle empty meshes and zero-size bounds without invalid camera calculations.
- Recompute unusable normals and define how to handle zero-area triangles.
- Preserve original geometry while using centered/scaled render coordinates.
- Bound render size and triangle count, and test clipping and degenerate geometry.
- If adapting FauxGL, retain its notices and pin the reviewed source. The parser
  and rasterization issues above need dedicated fixes and regression coverage.

## Next implementation step

Build a small Fyne viewport prototype with the validated loader and chosen
renderer, then measure opening, orbiting, resizing, zoom and Close on real STL
samples. Include both solid and wireframe views, several mesh sizes, malformed
files, disconnected components and imperfect normals. Measure memory and UI
latency as well as render time.

Aim for responsive navigation at roughly 30 frames per second during interaction
on representative models, with a sharper final frame after release. This is a
proposed acceptance target, not a measured result. Test on native macOS, Windows
and Linux before claiming desktop compatibility. Only then connect the viewport
to the app's opening flow, capability checks and file associations.
