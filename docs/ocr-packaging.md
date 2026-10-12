# Bundled OCR distribution

Proview packages include the engine, English and Italian models and third-party
licenses. Recognition runs offline without a separate Tesseract installation.
Normal Task builds use `packaging/build.py --bundle-ocr`; direct Go builds remain
available without native OCR build prerequisites for ordinary development/tests.

## Runtime layout

All five targets embed one compressed OCR archive in the application executable.
Windows releases remain a ZIP containing only `proview.exe`; macOS retains its
`.app` bundle and Linux its single AppImage. The runtime includes a minimal
Tesseract helper, the two models, licenses and a manifest containing source
versions and per-file SHA-256 hashes. No document pixels or recognized text are
included in the runtime cache.

On first image recognition, the worker extracts the archive under the current
user's cache directory, in `Proview/ocr/<archive-sha256>`. Windows uses
`%LocalAppData%`, macOS `~/Library/Caches` and Linux `$XDG_CACHE_HOME` or
`~/.cache`. Files are extracted into a private temporary directory, checked
against the manifest and published by atomic rename. Concurrent application
processes reuse a completed directory. Existing cache files are verified once
per process before use; deleted cache files are regenerated on the next launch.
If an existing runtime is damaged or locked, a fresh verified private directory
is used instead of overwriting files that another process may be using.

Runtime versions coexist to avoid breaking open applications after an update.
Old versions are not aggressively deleted; this cache can be removed while
Proview is closed. Extraction needs a writable user cache; on Linux it also needs
an executable filesystem. Recognition never extracts beside the downloaded app
and does not require administrator privileges. Native PDF text selection does
not need runtime extraction.

The bundled helper is launched by absolute path with an explicit model directory;
it ignores external `TESSDATA_PREFIX`. The helper runs from its model directory
with an ASCII relative model argument, preserving non-ASCII Windows user paths. Windows OCR processes create no console
window. The existing cancellation, 30-second deadline and local word-layout cache
remain in effect.

## Build inputs

`packaging/ocr_runtime.py` downloads checksum-pinned source archives and builds
static OCR libraries with CMake. Inputs are Tesseract 5.5.3, Leptonica 1.85.0,
libpng 1.6.50 and zlib 1.3.1. Model revision/hashes are in
`packaging/ocr_models.py`. The pinned model license is also checksum-verified.
Windows archives also include MinGW/winpthreads notices and the GNU runtime
license/exception texts, fetched from pinned references with SHA-256 checks.
Models, downloaded sources, native builds and embedded archives are ignored by
Git. Build configuration and archive checksums allow unchanged runtimes to be
reused locally; first builds need network access, end users do not.

Only PNG input is required because Proview decodes/normalizes source documents
before OCR. JPEG/TIFF/WebP/OpenJPEG codecs, training tools, legacy recognition,
network/archive support and OpenMP are excluded. Tesseract's Windows system-library
name is normalized from `Ws2_32` to `ws2_32` for GNU cross-linkers on case-sensitive
filesystems. No perspective correction is introduced.

The helper dependency check rejects non-system macOS libraries and external
Windows DLLs. Windows links the GNU/C++ runtimes statically. Linux retains its
platform standard runtime libraries and existing AppImage/glibc baseline.
macOS helpers receive an ad hoc signature before embedding; the enclosing app
is signed after the application binary is produced.

Build prerequisites are Python 3.10+, CMake 3.18+, Make and C/C++ compilers, in
addition to the existing Go/Fyne requirements. Native Windows uses MinGW Makefiles;
the Docker Windows build uses MinGW POSIX for the OCR helper and retains the
existing Zig compiler for the Go/Fyne application. `PROVIEW_OCR_CC` and
`PROVIEW_OCR_CXX` can select OCR compilers separately from CGO. Linux builds are native
to each container architecture; macOS explicitly selects ARM64 or AMD64.

## Verification

Bundled native builds run the resulting executable's `--check-ocr` command before
reporting success. It exercises extraction and bilingual recognition without
creating a GUI or processing private files. Cross builds perform dependency and
target checks but must also be run on their destination OS.

```sh
task package:macos ARCH=arm64
build/macos-arm64/Proview.app/Contents/MacOS/proview --check-ocr

task build:windows:native ARCH=amd64
# On Windows: build/windows-amd64/proview.exe --check-ocr

task package:linux ARCH=arm64
# On Linux: APPIMAGE_EXTRACT_AND_RUN=1 <built.AppImage> --check-ocr

PROVIEW_OCR_SMOKE=1 go test -tags proview_ocr_bundle ./internal/ocr \
  -run TestBundledRecognitionWithoutSystemEngine -count=1
```

The last test deliberately hides external engines/models. Unit tests cover
concurrent extraction, corrupt cache handling, cancellation, archive traversal,
wrong architecture, missing models and checksum failures without real OCR assets.
Python tooling tests verify source hashes, DLL rejection and build failure on a
failed packaged OCR self-test. Release checks keep the existing five-target
matrix with Google Drive available in every package; publishing remains a separate workflow action.

`PROVIEW_OCR=0` disables recognition. For explicit development experiments,
`PROVIEW_TESSERACT_PATH` selects a system engine and its environment-based models.
The release self-test always exercises the embedded helper even when an override
is set. `task ocr:probe` remains a system-engine quality evaluation; legacy
`cross:linux` is a development Fyne cross build rather than the AppImage release
path. Use `package:linux` or the self-hosted release workflow for bundled Linux
packages.

Both Build workflow runner choices produce bundled OCR packages for all five
targets. GitHub-hosted jobs build natively and run the embedded OCR smoke test
on each target, including Windows. The Apple Silicon self-hosted runner builds
macOS locally and Linux/Windows through Docker; Windows is cross-compiled and
still needs execution on a Windows machine. The self-hosted Mac needs CMake,
Make and Xcode C/C++ tools; the quality job checks these prerequisites early.
Linux/Windows container prerequisites are installed in their Docker images.
Both paths copy the Taskfile and all OCR build scripts from the workflow's
checkout so a selected branch uses consistent packaging tooling.
