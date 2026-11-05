# Large JPEG region experiment

## Status and scope

Phase 1 on `experiment/large-image-regions`, evaluated on 2026-11-04. The decoder
and request worker are isolated behind `largeimageexperiment && cgo`. The ordinary
application still has its 32 megapixel opening limit. No viewer, editing, export,
packaging or release behavior changes in this phase.

Entry points:

- `internal/imageregion`: native reduced/region JPEG decoding and request worker.
- `cmd/imageprobe`: timings, process-memory comparison and viewport-event stress.

The next phase should integrate the experiment into the actual viewport, preserving
the original bytes, original-pixel document geometry and full-resolution exports.
Do not pass a reduced preview through the ordinary document model as its source.

## Decoder choice and limitations

The experiment uses the installed libjpeg-turbo through pkg-config (`libjpeg`,
version 2.1.5 on the tested Linux host), with native scales 1, 1/2, 1/4 and 1/8.
`jpeg_crop_scanline` selects the horizontal span and `jpeg_skip_scanlines` skips
preceding rows in bounded chunks. This avoids allocating the complete decoded
image. A Go RGBA destination is filled directly, with one C scanline buffer.
Cancellation uses a C atomic flag checked in scanline loops and the native
progress callback. Errors and cancellation unwind inside C before returning to Go.

JPEG region access still traverses preceding compressed data. Regions near the
bottom therefore cost more than regions near the top. This is not constant-time
random access. See the upstream [partial decompression documentation](https://github.com/libjpeg-turbo/libjpeg-turbo/blob/main/doc/libjpeg.txt).

This first experiment supports sequential grayscale/RGB JPEG only. Progressive,
arithmetic and CMYK JPEGs are rejected before starting decompression; in particular,
progressive full-image coefficient storage needs a separate bounded strategy.
EXIF orientation is not yet mapped: request coordinates are encoded source pixels.
The existing JPEG orientation transform must be accounted for in viewport mapping.
ICC color management and other image formats have not been evaluated.

Source admission is capped at 300 megapixels; each output is capped at 8 million
pixels (32 MB RGBA). The caller owns immutable compressed bytes. The 1/8 scale
alone may still exceed the output budget for extreme dimensions or be insufficient
for a very small fitted viewport: further bounded downsampling remains necessary.
This is not a process-wide memory cap or a portable packaging solution.

Fancy chroma upsampling is disabled so independent regions match a full native
decode exactly, including crop boundaries. Final quality and overlap margins need
desktop review before adopting this choice in the viewer.

## Scheduling contract

Interaction must update view geometry immediately and continue showing the fitted
preview. A small delay before sharper detail appears is explicitly acceptable.

- One decoder is active per worker. One pending viewport replaces all older ones.
- Default probe settling time is 80 ms after the last event, using one timer.
- Submitting a new request cancels active work without waiting for it.
- A sequence number rejects completed results from obsolete requests.
- The output mailbox retains at most one result. Submission removes an obsolete
  completed frame immediately.
- Close signals cancellation and returns without joining native work. `Done`
  supports joining off the UI thread. Results after close are discarded.
- The UI must check `Current(sequence)` immediately before installing the image.

For GUI integration, also coalesce publication: at most one queued Fyne callback
should drain the newest completed frame. Moving an unbounded queue from decoding
to `fyne.Do` would defeat this design. Check source identity, viewport sequence and
window lifetime again inside that callback. Do not perform decode, resampling,
native joins or region composition on the UI thread.

## Measurements

Single local runs, Linux amd64, Intel Core i7-13700H. Input was the user's local
JPEG, 13,583 × 5,417, 30,789,588 compressed bytes. It is not a repository fixture.
Peak RSS was collected with `/usr/bin/time`; elapsed times exclude reading the
compressed file and optional PNG encoding. These are isolated processes, without
Fyne, GPU textures, overlays, OCR or the full document workspace.

| Operation | Decode time | Peak process RSS |
| --- | ---: | ---: |
| Full Go JPEG baseline | 884 ms | 141.1 MiB |
| Native 1/8 preview, 1698 × 678 | 206 ms | 41.2 MiB |
| Native 1600 × 1000 region at (5000, 0) | 46 ms | 41.6 MiB |
| Native 1600 × 1000 region at (5000, 4400) | 211 ms | 42.3 MiB |

The full Go decoder retains compact JPEG YCbCr pixels. The earlier 281 MiB estimate
was for a full RGBA copy, not the baseline JPEG raster. Baseline RSS also includes
the compressed data retained by the probe and the Go runtime. It is not a full
Proview comparison.

Stress: 300 viewport submissions spaced about 2 ms apart, 1600 × 1000 requests:

| Quiet period | Started / canceled decodes | Final detail after motion stopped | Maximum Submit time | Peak RSS |
| --- | ---: | ---: | ---: | ---: |
| 80 ms | 1 / 0 | 273 ms | 4.9 µs | 41.5 MiB |
| 0 ms, deliberate cancellation stress | 167 / 166 | 190 ms | 17.5 µs | 79.6 MiB |

Both runs delivered sequence 300. The no-delay run allocated about 1.10 GB
cumulatively despite bounded residency: repeatedly allocating an output before
cancellation is expensive. Keep the settling delay in the GUI. Consider reuse of
canceled, unpublished scratch buffers if later measurements justify it; published
image buffers must remain immutable. These runs do not prove native UI frame times
or a global latency bound.

The preview and bottom region PNGs were visually inspected. Raw measurements and
derived images live in ignored `build/image-region-experiment/`.

## Reproduction and verification

Requires Go, CGO, pkg-config and libjpeg-turbo development headers/library. The
experiment build tag keeps this dependency out of ordinary application builds.

```sh
go test -race -tags largeimageexperiment ./internal/imageregion ./cmd/imageprobe
go vet -tags largeimageexperiment ./internal/imageregion ./cmd/imageprobe
go build -tags largeimageexperiment -o build/imageprobe ./cmd/imageprobe

/usr/bin/time -f '%M KiB peak RSS' build/imageprobe -file "$HOME/Downloads/large-image.jpg" -mode full
/usr/bin/time -f '%M KiB peak RSS' build/imageprobe -file "$HOME/Downloads/large-image.jpg" -mode preview
/usr/bin/time -f '%M KiB peak RSS' build/imageprobe -file "$HOME/Downloads/large-image.jpg" -mode region -x 5000 -y 4400
/usr/bin/time -f '%M KiB peak RSS' build/imageprobe -file "$HOME/Downloads/large-image.jpg" -mode stress
/usr/bin/time -f '%M KiB peak RSS' build/imageprobe -file "$HOME/Downloads/large-image.jpg" -mode stress -settle 0s
```

`-out` optionally writes a PNG. Region coordinates and width/height are original
pixels; `-scale` selects the denominator. The JSON output records dimensions,
timings, Go heap statistics and stress sequence/decode counts. RSS comes from the
external measurement and includes native allocations.

Tests cover exact region/full native pixel correspondence at all four scales,
unaligned origins, edge clipping, cancellation, malformed/truncated streams,
unsupported progressive headers, a 10,000-request burst during an active decoder,
stale-success rejection and nonblocking close with a late native completion.
The worker suite also passed 30 repetitions under Go's race detector. This does
not instrument C atomics/native library internals. Ordinary `go test ./...` passed.

## Next integration decisions

### Next session: implementation order

Continue on `experiment/large-image-regions`, after reading `agent.md` and this
document. The user approved a viewer prototype following the successful isolated
experiment. Delayed sharpness after zoom/pan is acceptable; blocked interaction or
growing queues during rapid input is not. Do not package or release this work.

1. Separate original image geometry from display raster bounds. Start with
   `pkg/document/document.go`, `pkg/document/model.go`, `pkg/gui/menu.go`,
   `pkg/gui/zoom.go`, `pkg/gui/imageViewport.go` and the crop path in
   `pkg/gui/gui.go`. Preserve source bytes and full-resolution edit/export behavior.
2. Add an experimental large-JPEG opening path that prepares a bounded reduced
   preview in the opening worker. Keep unsupported formats on their existing
   paths; do not silently decode a large unsupported JPEG at full resolution.
3. Display one replaceable detail patch above the reduced preview, with a small
   overscan margin and a scale selected from zoom and device-pixel density.
   Retain the preview while detail is pending. Defer a multi-tile cache.
4. Connect viewport changes to the bounded worker with an initial 80 ms quiet
   period. Coalesce both decoding and UI publication. Invalidate requests on
   document switch, close, transform and viewport changes; reject stale results.
5. Verify the real desktop with rapid zoom/pan, resize, switching and close during
   decoding. Measure peak RSS and interaction responsiveness. Cover orientation,
   crop, annotations, thumbnails, recognition and original-resolution export before
   treating the integration as complete.

The experiment's native dependency is not part of the existing portable builds.
Choose an explicit prototype build configuration before connecting the GUI to it;
ordinary builds must remain functional. Progressive JPEG support and distribution
on Linux/macOS/Windows remain separate decisions before general adoption.

### Moving to another machine

Transfer the branch **including the experiment code and this document**. A branch
name alone does not transfer uncommitted files; commit/push or otherwise copy the
working tree before switching machines. The source photograph and ignored
`build/image-region-experiment/` artifacts are not tracked and need a separate
transfer if the same measurements are to be reproduced. Install the development
dependencies described above on the destination machine.

Use a reduced full-image preview beneath a viewport-sized detail patch, with a
small overscan margin. Pick the native scale from device-pixel density and zoom;
avoid requesting more detail than the display can show. First prototype one
replaceable patch; add a byte-bounded tile cache only if panning measurements
justify it. Do not retain many RGBA copies for previously visited areas.

Integrate source teardown, retained-document switching, rotation/flips/crop,
thumbnail generation, selection geometry, recognition and export separately from
display resolution. Existing code often derives image geometry from the preview's
pixel bounds; those sites need original-size geometry before opening large files.
Validate rapid zoom/pan/resize/close with the real desktop and GPU before raising
the opening limit. Choose a progressive JPEG strategy and cross-platform decoder
distribution before treating this as general large-image support.
