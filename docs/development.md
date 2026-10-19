# Development and packaging

Build, test, package and release Proview. For the product overview, see
[the README](../README.md).

## Supported images

Open PNG, JPEG, GIF, TIFF (`.tif` / `.tiff`), BMP and WebP, as well as PDF documents.
GIFs show their first frame and multipage TIFFs show their first page. Images keep
transparency when the source format supports it; TIFF and JPEG orientation is applied
when opening. Editing imported GIF/TIFF/BMP/WebP images uses Save As in PNG or JPEG
and leaves the original file unchanged.

## Requirements

- Go **1.26+**, Python **3.10+** for native builds, and [Task](https://taskfile.dev/).
- CGO enabled, C/C++ compilers, CMake **3.18+** and Make for the target platform.
- macOS: Xcode Command Line Tools (`xcode-select --install`).
- Linux: GCC and OpenGL/X11 development libraries. The CI installs
  `gcc g++ cmake make libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev` on Ubuntu.
- Windows: MinGW (including `g++` and `mingw32-make`) and CMake available in PATH.
- Cross compilation: Docker running; the legacy Linux development cross task also uses `fyne-cross`.

## Development commands

Run commands from the repository root.

| Command | Action |
| --- | --- |
| `task --list` | List available tasks |
| `task run` | Build and run Proview with bundled OCR |
| `task test` | Run `go test ./...` |
| `task vet` | Run `go vet ./...` |
| `task check` | Run tests and vet |
| `task redact:probe` | Run native PDF redaction security and geometry checks |
| `task build` | Build for the current OS into `build/proview` (`.exe` on Windows) |

## Command-line window and dialog options

```sh
proview --new-window --work-dir /path/to/fixtures
proview --new-window --work-dir /path/to/fixtures document.pdf image.png
```

`--new-window` opens one fresh editor window, including when Proview is already
running and when no files are supplied. Additional file arguments use the normal
workspace rules inside that window. Without a running application, the initial
window fulfills the request; it does not create a second empty window.

`--work-dir` must name an existing directory. Relative directory paths are resolved
against the caller's current directory before forwarding. Every new Open,
PDF collection, signature import and Save As dialog in the receiving window starts
there, even after selecting files elsewhere. It does not change the process
working directory, reinterpret file arguments, restrict browsing, or overwrite an
existing document's Save target. Save As still permits another destination.

This setting is local to the window and does not update the persistent last-save
folder. App-created child windows inherit it; an explicit `--new-window` launch
without `--work-dir` starts with the normal directory defaults. `--work-dir` alone
applies to the current editor if another instance is running. Preferences such as
theme and the signature library are still shared; this is not an isolated profile.
If the directory is removed during the session, dialogs use their normal fallback.

`--data-dir /path/to/profile` selects a separate process-wide profile. The folder
is created if needed, resolved against the caller's cwd and canonicalized through
symlinks. Signatures, bounded logs and desktop IPC live under `signatures/`,
`logs/` and `desktop/` inside it. Only launches using the same canonical root can
forward to one another; the ordinary installed instance remains separate.
File association registration is skipped for custom profiles.

Application preferences use a Fyne application ID derived from that root and
remain in Fyne's platform configuration directory (on macOS,
`~/Library/Preferences/fyne/com.emanuelelongo.proview.profile.<hash>/`). They are
separate from the normal profile, but are not moved into `--data-dir`. OS keychain
credentials use the same distinct identity; secrets are never stored in the data
folder. An empty profile does not restore the normal profile's cloud connection.
Fyne's global theme settings and the verified OCR runtime cache remain shared.
Without `--data-dir`, all existing storage locations and credentials are retained.
This option is independent of the per-window `--work-dir` and is never forwarded
as a change to a running process's profile.

```sh
proview --new-window --work-dir demo/fixtures --data-dir build/manual-profile demo/fixtures/showcase.pdf
```

Options precede file arguments; use `--` before a filename starting with `-`.
Use `--help` for usage. `--check-ocr` remains a standalone check and cannot be
combined with window options or files. Invalid arguments exit before starting
logging, IPC or the GUI. These options are available on all desktop platforms;
automated macOS demo recording is a separate workflow.

## Signature cloud sync

The signature library supports optional Google Drive and S3 synchronization. See
[setup and behavior](signature-cloud-sync.md) for provider configuration,
local storage, offline behavior, and the two-device validation procedure.

## GitHub releases

The **Release** workflow accepts a **branch** and a **runner** choice:
`github-hosted` builds natively on GitHub's macOS, Windows and Linux runners;
`self-hosted` builds all five targets on an Apple Silicon Mac, using Docker
for Linux and Windows. Both paths publish the same five packages, with Google Drive available, and
use the same workflow run counter for release tags and package versions, minus
the repository variable `RELEASE_RUN_OFFSET` (default: zero). Bundle build numbers
retain the original workflow run number. Run titles show the selected runner and
branch. Release is manual-only; pull requests use the separate checks workflow.

Self-hosted jobs select the locally installed Homebrew Python (`brew install python`)
through `packaging/self_hosted_python.sh`. The script checks safe tar extraction
support and sets Python for subsequent steps and Task subprocesses, avoiding
macOS's older system Python. GitHub-hosted package jobs use `actions/setup-python`.

On Apple Silicon with Colima, enable Rosetta for AMD64 containers:
`colima start --vz-rosetta`. If Colima is already running, stop it before
restarting with that option, after any active containers and jobs finish.
QEMU user-mode emulation can crash the Go toolchain even for simple programs.
Self-hosted Linux packaging stages the AppDir under `/tmp` inside the container;
libstdc++ file copies can fail on Colima's macOS VirtioFS bind mounts.

This workflow does not run on pushes. Existing branches with the old workflow
must receive this update to stop their push-triggered runs.
To publish, open **Actions → Build → Run workflow**, use
`develop` in GitHub's **Use workflow from** selector, and enter the branch to
build in the **branch** input (default: `develop`). Select **runner**:
`github-hosted` (default) or `self-hosted`. Tests, static checks and
packaging run on that branch's resolved commit.

After all jobs succeed, branches other than `develop` publish a prerelease named
`v1-dev.<release-number>`. Choosing `develop` publishes a prerelease named
`v1.<release-number>`. Every package supports Google Drive, S3 and local signatures,
with no provider-specific filename markers.

Manual runs always publish a prerelease, without changing GitHub's latest release.

Release notes use a fixed template with a **What's new** section, platform
download links, installation tips, checksums and collapsible build details.
After publication, edit **What's new** on GitHub to replace the placeholder
with the release highlights. The workflow does not infer changes from commits.
To promote a tested prerelease, edit it on GitHub or run
`gh release edit <tag> --prerelease=false --latest`.

The workflow must be present on the default branch for the manual button to appear.
Every release points to the selected commit. Release numbers can have gaps.
All build jobs and the release use the same
resolved commit, even if the input branch advances during the run.
Native package versions are `1.<release-number>.0-dev`.

To restart release numbering, set `RELEASE_RUN_OFFSET` to the next Release workflow
run number in **Settings → Secrets and variables → Actions → Variables**.
The next run then uses release number 0 (`v1.0` on `develop`); GitHub Actions history remains intact.
The offset must be nonnegative and no greater than the current run number.
The repository uses `RELEASE_RUN_OFFSET=2`, so its next Release workflow run
produces `v1.0` on `develop` after the failed initial run. Subsequent runs
increment the release number.
Avoid rerunning workflow revisions with different numbering rules.

Download packages from the repository's **Releases** page:

- macOS ARM64 and AMD64: ZIP containing `Proview.app`, signed ad hoc, not notarized.
- Windows AMD64: ZIP containing the GUI executable.
- Linux AMD64 and ARM64: AppImage; run `chmod +x <file>.AppImage` before launching.
- Linux ARM64 packages target Ubuntu 24.04 or compatible systems with glibc 2.39
  or newer; bundled MuPDF libraries cannot link on Ubuntu 22.04 ARM64.
- `SHA256SUMS.txt`: checksums for all five packages.

Build versions are applied only in the runners' working copies. CI creates a
draft, uploads all assets, then publishes it. Failed runs do not publish a
release if checks or packaging fail. Rerunning a successful run replaces its published assets and checksums.
Pull requests run `.github/workflows/checks.yml` on GitHub-hosted macOS
ARM64/Intel, Windows x64 and Linux x64/ARM64 runners. They execute all Go tests
(including native PDF security tests), vet and logging race tests. Linux x64 also
checks Go formatting and runs the Python build/release tooling tests. The tests
compile their temporary test executables; no application binary, OCR bundle,
ZIP, AppImage or release artifact is produced.

GitHub automatically displays each job as a check in the pull request. The stable
**PR checks** check succeeds only when the entire test matrix succeeds; failures,
cancellations and skipped matrices do not pass. Each target has a Markdown
verification summary alongside its logs. New commits cancel obsolete PR runs.
No bot comments, badges, secrets or write permissions are needed. To require
these checks before merging, add **PR checks** as a required status check in the
branch rules for `develop`. This workflow does not change repository rules.

The same **Checks** workflow supports manual runs: open **Actions → Checks →
Run workflow**, keep **Use workflow from = develop** and set **Branch to check**
to the target branch (default `develop`). The first selector chooses the workflow
definition; the separate input chooses the code to test. The target branch does
not need to contain the updated workflow. From the CLI:

```sh
gh workflow run checks.yml --ref develop -f branch=feature/example
```

Manual runs resolve the requested branch once and pin all five runners to that
commit. PR runs pin GitHub's proposed merge commit. Each target's summary records
the checked SHA. Both execute the same five-target matrix without creating release
assets. Runs are grouped by event and PR number or checked branch: a newer run
replaces an older one for that same target, while unrelated targets remain
independent, even when the workflow definition comes from the same branch.
The aggregate check keeps its **PR checks** name for branch-rule compatibility.

Every target first runs the complete native PDF package in release order, then
in a separate process three times with `-count=3 -shuffle=on -v`, matching the
GitHub-hosted native release gates. The first pass uses `-count=1`. Each
failure fails the job; this is not a retry-until-success policy. The full Go suite
then runs with `-count=1`, so restored Go caches cannot replace test execution.
Repeated runs help expose intermittent native faults and order-dependent state;
the verbose test names and printed shuffle seed make failures reproducible.
The target summary reports both native gates separately. No application binaries
or OCR bundles are built by Checks; packaging-specific failures remain release
checks.

Keeping PR checks in a separate workflow also prevents them from consuming the
Release workflow's release numbering counter. Promotion from prerelease to release
remains a manual action on GitHub.
The workflow does not require additional release credentials: only the publishing
job receives `contents: write` through `GITHUB_TOKEN`. Releases inherit repository
visibility. GitHub-hosted builds use GitHub Actions resources according to the repository's plan and visibility.
Each manually requested build runs five package jobs. Published packages require the OAuth client settings described in the
[cloud sync guide](signature-cloud-sync.md).

### Self-hosted builds

Register a repository runner under **Settings → Actions → Runners → New
self-hosted runner**, choosing **macOS / ARM64**, and add the custom label
`macos-self-hosted`. With **runner = self-hosted**, all jobs, including version
selection, checks and publication, require this runner; none use GitHub-hosted
compute. Build matrix jobs run one at a time.

Install the Xcode Command Line Tools, CMake, Make, Python 3 and GitHub CLI (`gh`), and have Docker
Desktop running. Docker must support both `linux/arm64` and `linux/amd64`
containers. Python must be available as `python3` in the runner's PATH. The
workflow installs Go and Task. macOS ARM64/AMD64 apps
are built and signed ad hoc on the Mac. Linux AMD64 AppImages use Ubuntu 22.04;
ARM64 AppImages use Ubuntu 24.04 to satisfy MuPDF's glibc requirement. Windows
AMD64 uses the existing Fyne cross image and its Windows GNU toolchain.
All targets use the same build helper for Google Sync credentials and About
metadata; credentials are passed to containers by environment variable name,
not embedded into Docker images.

Start the runner's `./run.sh` only when needed, then run **Release**
from GitHub with the desired **branch** and **runner = self-hosted**. Keep the
Mac awake and the runner running until the workflow finishes; afterwards stop
it with Ctrl+C.
An offline runner leaves jobs queued until it becomes available (up to 24 hours).
The runner can stay registered between sessions. A second registered runner or
GitHub-hosted fallback is not required.

Checks run on the Mac and the native PDF engine is also tested inside each Linux
container. Windows PDF tests are cross-compiled but not executed; run the ZIP
on Windows before treating that target as runtime-verified. Cross builds do not
replace native CI testing on the destination operating systems.

## Native builds

**Help → About** shows the app name, version, repository, full commit SHA and
**Date** (the commit's committer date in UTC). CI binaries show their release tag
as the version. Local builds made with `task build`, `task run` or
`packaging/build.py` use `DEV` and embed the current Git commit and its date.
Without Git, the commit and date are shown as `Unknown`. Direct `go build` or
`go run` uses `DEV` and reads Go's embedded VCS metadata when available.

Run each task on its corresponding OS. Architecture defaults to the host;
override it with `ARCH=arm64` or `ARCH=amd64` when supported by the C compiler.

| Command | Output |
| --- | --- |
| `task build:macos` | `build/macos-<arch>/proview` |
| `task build:linux` | `build/linux-<arch>/proview` |
| `task package:linux` | `build/linux-<arch>/Proview-<version>-<appimage-arch>.AppImage` |
| `task install:linux` | Install the binary, icon and launcher for the current Linux user |
| `task uninstall:linux` | Remove the current user's Linux installation |
| `task build:windows` | Cross builds `proview.exe` with Docker from any OS |
| `task build:windows:native` | `build/windows-<arch>/proview.exe` on Windows with MinGW |
| `task package:macos` | `build/macos-<arch>/Proview.app` |
| `task icon:macos` | `build/icons/Proview.icns` |

### Linux desktop integration

Run `task install:linux` to build and install Proview for the current user. It
places the executable in `~/.local/bin`, the application icon in the user icon
theme and the launcher in `~/.local/share/applications`. Proview will then
appear in the application launcher and can be pinned to the dock or taskbar.
Run `task uninstall:linux` to remove these installed files.
The installed launcher uses the executable's absolute path, so it also works
when the graphical session does not include `~/.local/bin` in `PATH`.
The launcher registers PDF, PNG, JPEG, GIF, TIFF, BMP, WebP and STL support, making Proview available in
the file manager's **Open With** list. Installation does not change the user's
default applications.

Run `task package:linux` to produce a single distributable AppImage containing
the application, icon, launcher metadata and non-system shared libraries. The
task supports native AMD64 and ARM64 Linux builds, downloads a pinned and
checksum-verified `linuxdeploy`, and does not require FUSE while packaging.
The resulting AppImage can be copied to another compatible Linux system and run
after setting its executable bit. Building on an older supported Linux release
provides the broadest glibc compatibility.

For example, on macOS:

```sh
task package:macos ARCH=arm64
task package:macos ARCH=amd64
```

The packaging task builds the executable, generates the icon, creates the app
bundle with PDF, PNG, JPEG, GIF, TIFF, BMP, WebP and STL **Open With** declarations, signs it ad hoc and
verifies the signature. It then deletes the standalone intermediate executable.
Copy `Proview.app` into `/Applications` to install it. macOS may take a short
while to refresh the Open With list. Opening one or more files through Finder
reuses an empty Proview window when possible and opens additional files in
the shared document workspace; existing documents are retained. PDF-to-PDF
opening offers a new window or appending pages. See
[Document workspace](document-workspace.md) for local implementation status. The package does not
change the user's default applications. After rebuilding, replace any previously
installed copy.

The app ID, icon source, version and build number are defined in `FyneApp.toml`.
macOS bundle metadata uses the numeric version (`1.0.0`) and keeps the full beta
label in `CFBundleGetInfoString`. Developer ID signing and notarization are not
part of this local packaging task.

### Windows file associations

The portable Windows executable registers Proview in **Open With** for PDF,
supported images and STL when launched. Registration is per user and requires
no administrator privileges. Launching the executable after moving or renaming
it updates the registered opening commands. Existing default applications remain
unchanged; choose Proview from **Open With** or Windows **Default apps** to make
it the default viewer.

## Cross compilation

The Taskfile pins [fyne-cross](https://github.com/fyne-io/fyne-cross) to **v1.6.3**.
The legacy Linux development cross task requires it in PATH. Start Docker. The Windows
cross task builds a local image with Go 1.26 because the upstream image uses an
older Go compiler:

```sh
task tools:cross
export PATH="$(go env GOPATH)/bin:$PATH"
task build:windows ARCH=amd64
task cross:linux ARCH=amd64
```

Cross tasks default to `amd64`. The Windows task produces the single bundled
`build/windows-amd64/proview.exe`; it does not require the fyne-cross CLI. The
legacy Linux development task writes under `fyne-cross/bin` and `fyne-cross/dist`
and omits bundled OCR; use `package:linux` or the self-hosted release workflow
for Linux distribution. The first run downloads container images and dependencies.
`task build:windows:native` builds directly on Windows with MinGW. Test cross-built
artifacts on their target OS before distributing them.

## Icons and fonts

- UI icons: [Tabler Icons](https://github.com/tabler/tabler-icons), outline set,
  revision `74929e50416e2b7c0abb8368cdc74bdcb2560ab6`, MIT license.
  Bundled SVGs, license and provenance are in `pkg/gui/icons/tabler`.
  Use `tablerIcon()` for Fyne theme adaptation.
- Application icon: `assets/app-icon.png`, 1024 × 1024 with transparency.
  The selected Arcade design uses a document and magnifying glass with
  violet/blue/cyan gradients. Generation details are in [assets/README.md](../assets/README.md).
  The PNG is embedded in the executable; macOS packaging converts it with
  `sips` and `iconutil` and installs the `.icns` resource in the bundle.
  Windows builds link `packaging/windows/proview.ico` as native EXE resources
  using MinGW `windres`, so Explorer and shortcuts display the application icon.
  Regenerate the ICO after changing the source PNG with
  `go run packaging/windows/icon.go assets/app-icon.png packaging/windows/proview.ico`.
- Fonts: Go Sans, Go Mono and Latin Modern Roman. Bundled font, icon and native
  PDF notices are accessible through **Help → Third-Party Licenses**.

## Main libraries

Versions are pinned in [go.mod](../go.mod); checksums are in `go.sum`.

Native redaction behavior, supported structures, security checks and MuPDF header
provenance are documented in [internal/pdfengine/README.md](../internal/pdfengine/README.md).
Redact selects native text or existing OCR; removal and document sanitation are
applied on PDF export. Affected shared image regions are cleaned in every reuse.
Unsupported structures fail without replacing the destination.

| Library | Purpose |
| --- | --- |
| [Fyne](https://github.com/fyne-io/fyne) v2.7.4 | Desktop GUI |
| [go-keyring](https://github.com/zalando/go-keyring) v0.2.6 | OS credential storage |
| [AWS SDK for Go v2](https://github.com/aws/aws-sdk-go-v2) | S3 signature sync |
| [Smithy Go](https://github.com/aws/smithy-go) v1.28.1 | AWS SDK runtime |
| [oauth2](https://pkg.go.dev/golang.org/x/oauth2) v0.36.0 | Google authorization |
| [clipboard](https://github.com/golang-design/clipboard) v0.9.0 | Native image clipboard (PNG) |
| [zenity](https://github.com/ncruces/zenity) v0.10.15 | System file selection dialogs |
| [go-fitz](https://github.com/gen2brain/go-fitz) v1.24.15 | PDF previews and native redaction via MuPDF |
| [pdfcpu](https://github.com/pdfcpu/pdfcpu) v0.16.0 | Native PDF page operations and export |
| [tdewolff/canvas](https://github.com/tdewolff/canvas) | Vector geometry and rendering |
| [latin-modern](https://github.com/go-fonts/latin-modern) v0.3.3 | Embedded text fonts |
| `golang.org/x/image`, `golang.org/x/text` | Image and text support |

## Local data and maintenance

Opening documents, importing signatures and adding PDFs use system file dialogs.
The PDF picker supports multiple selection; the collection form still controls
their order. The last selected folder is remembered per editor window for the session.
macOS uses the system `osascript` helper; Windows uses native dialogs. Linux uses
Zenity, Qarma or a compatible installed helper (for example `sudo apt install zenity`).
If the native picker is unavailable or fails, Proview uses a larger Fyne picker.
The Fyne fallback selects one file at a time; use **Add PDFs** repeatedly.
Cancellation leaves the current document unchanged. **Save** atomically overwrites
the current destination without a replacement prompt; **Save As** selects a new
local destination and asks before replacing an existing file. Successful saves are
silent. PDF redaction safeguards still require their separate security confirmation.

Signature storage:

- macOS: `~/Library/Application Support/Proview/signatures`.
- Windows: `%LOCALAPPDATA%/Proview/signatures`.
- Linux: `$XDG_DATA_HOME/proview/signatures`, default `~/.local/share/proview/signatures`.

Generated binaries, `build/`, `fyne-cross/`, `.task/` and `.DS_Store` are ignored.
Keep source assets and third-party licenses under version control.

CI is defined in `.github/workflows/ci.yml`: Linux tests/vet/build plus native
macOS and Windows builds. Development conventions, architectural constraints and
known regressions are documented in [agent.md](../agent.md). All code, UI strings
and project documentation use English.

## Editing tools

- Tool properties move next to the zoom controls, after a separator, whenever
  the complete block fits. On narrower windows the whole block moves to a second
  row, so individual controls are never split across rows.
- A slim, left-aligned bottom bar presents each startup tip for ten seconds,
  beginning ten seconds after launch, with a five-second pause between tips.
  The sequence runs once per application session and can be replayed immediately
  with **Help → Show Tips**.
- **Text** stays active after placement. The entry is cleared immediately after a
  text object is created, preventing a later page click from duplicating it. The
  font, style and color settings remain available for the next object.
- Hold **Shift** in **Text**, **Draw**, **Highlight** or **Shapes** (including **Magnifier**) to
  temporarily activate **Select** and move or resize objects. Its toolbar icon
  lights up; release Shift to return to the insertion tool. The current tool's
  property row stays in place but is disabled, avoiding document movement. Shift
  retains normal behavior while typing and during an ongoing drawing gesture.
- Select an inserted signature to edit its properties. Drawn signatures support
  color and stroke width; imported SVGs support a uniform color; PNG signatures
  retain the existing alpha thickening control. Library originals stay unchanged.
- In **Select**, drag on the page background to select an area, or hold **Alt**
  to start an area selection over an object. **Command+C / Command+V** on macOS
  (**Control+C / Control+V** elsewhere) copy the visible selection as PNG and paste
  an image as an editable object. Copying a selected object also preserves its
  editable type, properties and transforms in Proview, including between editor
  windows. Repeated pastes create independent copies with a small position offset.
  Other applications receive a transparent PNG representation. Text fields keep
  their normal clipboard behavior.
  PDF area copies use 144 DPI; image copies retain source resolution.
- Command/Control-click PDF thumbnails to select individual pages, or Shift-click
  to select a range. Drag the selection to move it together; right-click to
  duplicate or delete it in one Undo step. Copies retain objects/redaction marks
  and form an ordered group after the last selected original.
- **Shapes** (**Command/Control+Shift+S**) offers icon-only choices for rectangle, oval, callout, arrow,
  straight line, five-point star and magnifier. Drag to create; ordinary shapes support color,
  stroke width, solid/dashed/dotted lines and an icon toggle for outline/filled
  appearance. Draw supports round, dashed and dotted strokes with independent opacity. Hold **Control**
  to constrain an oval to a circle or preserve a shape's proportions while resizing;
  combine it with **Shift** to resize proportionally while Select is temporarily active.
- The magnifier stays circular and has a separate **1–8×** zoom slider. It follows
  its location and underlying content, excluding other magnifiers. Only the circular
  magnifier content becomes a 300 DPI raster overlay during PDF export; the source
  page and ordinary vector objects remain vector content.
- Rotate left/right in 90-degree steps or flip horizontally/vertically from the
  main toolbar. A selected object is transformed on either document type. With no
  object selected, the controls transform the entire image; on PDFs, rotation acts
  only on the current page and remains native vector PDF content. Whole-page PDF
  flipping is not supported, while selected PDF objects can still be flipped.
- Copy and magnifier rendering use sanitized content when redactions are present;
  temporary hover reveals never appear in clipboard images or magnifiers.

To probe native clipboard initialization without replacing clipboard contents:
`PROVIEW_CLIPBOARD_PROBE=1 go test ./pkg/gui -run TestNativeImageClipboardBackend`.
Actual clipboard interoperability should also be checked on each target desktop.

## Automatic text selection

Normal Task builds and release packages include Tesseract, all non-system OCR
libraries, and the English/Italian models. Recognition is automatic and offline;
users do not install Tesseract or download anything at startup. Windows ZIPs still
contain one `proview.exe`; the app extracts its embedded runtime into a private,
versioned user cache. macOS and Linux use the same embedded runtime inside their
app/AppImage. See [OCR packaging](ocr-packaging.md) for source pins, cache
behavior, release checks and developer overrides, and
[selection behavior](ocr-prototype.md) for interaction and current limits.

`task ocr:probe` evaluates a separately installed Tesseract 5 using the pinned
models. Direct `go build` and `packaging/build.py` omit OCR assets unless the
`proview_ocr_bundle` tag or `--bundle-ocr` option is requested; ordinary Go tests
require no OCR installation or downloaded assets.

### Raster signature prototype

**Signature → New signature → Import from file → OK** accepts SVG, JPEG and PNG. SVG stays
vector; raster images open **Import signature** and become transparent monochrome
SVG. The selection is always visible, and moving or resizing it updates the
preview automatically. Adjust **Sensitivity** to filter weak marks or include
fainter ink; smoothing uses its maximum setting. The conversion runs locally in
Go and requires no external tracing tool or network access. Existing stored PNG
signatures retain their original behavior.

The resulting asset uses the existing signature library, recoloring and vector
PDF export. It reconstructs ink outlines rather than editable pen strokes.
The current prototype handles light paper, modest shading and transparent PNGs;
blur, ruled paper and overlapping printed text still need careful manual cropping.
Details and evaluation commands are in [the prototype notes](signature-vectorization.md).

## Troubleshooting logs

Use **Help → Open logs folder** for local rotating diagnostic logs and Go crash
reports. See [Diagnostic logs](logging.md) for paths, retention and performance.
`task run` continues to show the same application logs in the terminal.

## Demonstration media

See [demo/README.md](../demo/README.md) for synthetic PDF/image/STL fixtures and
local macOS capture recipes. `task demo:check` validates fixtures and tooling
without desktop access; `task demo:plan` prints recipes. Agents regenerating
screenshots or animations must follow [demo/agent.md](../demo/agent.md).
