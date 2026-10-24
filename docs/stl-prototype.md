# STL viewer prototype

The local prototype opens ASCII and binary `.stl` files in a read-only 3D viewport.
The mesh loader and CPU fallback are implemented in Go. On macOS the viewer
tries an isolated OpenGL GPU renderer, reusing the existing go-gl dependency.
The original model coordinates are retained; navigation changes only the camera.

## Try it

From the repository root:

```sh
go run ./cmd/gui internal/model3d/testdata/cube.stl
```

The app's **File → Open** picker also accepts STL. Existing startup-file handling
supports opening STL from the command line. STL associations are declared in the macOS bundle and Linux desktop entry;
Windows registers Open With support per user when launched. See the
[platform backlog](../next-to-do.md) for remaining desktop verification.

For the usual build with bundled OCR, run `task run`, then open an STL through
File → Open. A direct `go run` or the local `build/proview-stl-prototype` binary
uses the ordinary unbundled developer build; STL itself requires no OCR assets.

## Controls

- Drag to orbit around the model.
- Hold Space and drag to pan.
- Scroll, use the zoom toolbar buttons, or use the usual zoom shortcuts to zoom.
- Press 1–7 for Isometric, Top, Bottom, Front, Rear, Left and Right, respectively.
- Press 0 to toggle Wireframe. These commands also appear in View, only for STL.
- Selecting a standard view resets zoom and pan while preserving wireframe.
- Fit to Window (`Command/Ctrl+0`) resets zoom and pan without changing orientation.
- Crop selects a rectangular screenshot of the current shaded/wireframe view.
  Confirm Crop to select an independent, editable PNG in the same sidebar; the STL camera and
  mesh stay intact. Escape cancels; changing the camera cancels the selection.
- File → Close (`Command/Ctrl+W`) clears the model and keeps the window open.

The status bar shows triangle count and bounding-box dimensions in model units.
STL does not provide a unit setting here; dimensions are not labeled as millimeters.
Save, signatures, drawing tools, image transforms, clipboard document editing
and PDF page operations are unavailable in model mode. Creating a new image adds a sidebar
entry in the same window and retains the model. PDFs open separately. Selecting an image/PDF restores
its usual editing tools.

## Scope and limits

- Orthographic camera, white surfaces on a dark background, two-sided lighting.
  Shared-vertex shading smooths shallow facets while retaining sharp edges.
  Low-polygon silhouettes still reflect the geometry of the STL.
- Maximum input size: 64 MiB; maximum facet count: 500,000.
- Invalid numbers, incomplete facets and non-finite coordinates produce errors.
  Degenerate triangles are skipped; an empty result is rejected.
- Render size is capped at 2000 pixels on its longest side. GPU rendering keeps
  multisample antialiasing (up to 4 samples) during dragging. The CPU fallback
  uses a 1000-pixel limit during dragging, then 2× supersampling while still.
  Camera updates are coalesced; Close cancels the worker and clears its frame.
- Wireframe uses a depth prepass to hide rear edges. Very close surfaces may show
  edge artifacts because of the depth tolerance.
- No mesh editing, repair, color extensions, slicing or STL export.
- UI unit tests and local rendering checks do not establish native usability or
  frame rates on Windows/Linux. Real model and desktop testing remain necessary.

## Inspect the renderer

A small probe uses the same loader and renderers without opening a window:

```sh
go run ./cmd/stlprobe -file internal/model3d/testdata/cube.stl
go run ./cmd/stlprobe -gpu -frames 60 -file internal/model3d/testdata/cube.stl
go run ./cmd/stlprobe -file internal/model3d/testdata/cube.stl \
  -wireframe -out build/stl-prototype/wireframe.png
```

Use `-view isometric`, `top`, `bottom`, `front`, `rear`, `left` or `right`
to inspect the standard views.
The probe prints triangle count, load time and render time and writes a PNG under
ignored `build/` by default. The included cube is generated geometry.

Run `task check` for application checks and `go test -race ./internal/model3d`
for parser/renderer race checks. Loader tests include binary headers beginning
with `solid`, malformed ASCII/binary files, invalid numbers and tiny geometry.
Renderer tests cover camera changes, wireframe, frame ownership and cancellation;
GUI tests cover mode transitions, command availability, navigation and reader cleanup.
The earlier [feasibility study](stl-viewer-feasibility.md) records the external
renderer experiment that led to this internal implementation.

## GPU experiment

The GPU path currently supports **macOS with CGO**. Linux and Windows still use
CPU rendering; their independent native contexts have not been implemented yet.
If context creation, shader compilation or GPU drawing fails, the viewer falls
back to CPU automatically. The diagnostic log reports the renderer actually used.
Force software rendering for comparison:

```sh
PROVIEW_STL_RENDERER=cpu go run ./cmd/gui internal/model3d/testdata/cube.stl
```

Each STL viewport owns an accelerated OpenGL 3.2 core CGL context on a dedicated
locked OS thread. Fyne's graphics context is never accessed or modified. Positions
and crease-aware normals are uploaded once per mesh; shaders handle camera
transforms, interpolated lighting and rasterization. Multisample color/depth
attachments resolve into an RGBA frame, then read back to Fyne. This transfer
keeps the existing Crop behavior and immutable-frame ownership. Resizing recreates
attachments; Close/replacement releases the context and its buffers on its owner
thread. Multiple STL windows have independent contexts.

This is an intermediate integration: it still copies frames from GPU to CPU and
back into Fyne's display texture. A direct shared-texture viewport would require
a supported Fyne rendering hook or changes to its driver. OpenGL is deprecated on
macOS; the prototype tests the integration and potential gain rather than choosing
an eventual graphics API for every platform.

A local Apple M4 probe at **1000 × 700**, 60 camera frames, on a generated sphere
with **318,400 triangles**, measured approximately **3.1 ms/frame GPU** versus
**116.7 ms/frame CPU**. Both include frame generation/readback; the GPU batch also
includes context initialization and initial mesh upload. CPU uses four-sample
supersampling, GPU uses four-sample MSAA and interpolated fragment lighting. These
are renderer measurements on synthetic geometry, not measured GUI frame rates.

Hardware checks are opt-in so ordinary tests require no graphics session:

```sh
PROVIEW_TEST_STL_GPU=1 go test -race ./internal/modelgpu -count=1
```

They verify actual GPU selection, independent contexts, camera/CPU silhouette
agreement, wireframe, frame ownership, attachment resizing, mesh replacement and
worker shutdown. Standard tests cover the CPU fallback, cancellation and limits.

References: [Apple pixel format/context attributes](https://developer.apple.com/library/archive/documentation/GraphicsImaging/Conceptual/OpenGL-MacProgGuide/opengl_pixelformats/opengl_pixelformats.html),
[Khronos framebuffer multisampling](https://wikis.khronos.org/opengl/Framebuffer),
[OpenGL frame readback](https://wikis.khronos.org/opengl/GLAPI/glReadPixels).

STL documents in the workspace sidebar use a static isometric thumbnail instead
of a filename. A bounded CPU render runs once per retained document, outside the
UI thread. Camera changes never regenerate it; closing the document cancels
pending thumbnail work and stale results are discarded.
