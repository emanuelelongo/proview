# README demo media

`fixtures/` contains ready-to-use input files. `cases.json` defines exactly the
screenshots and GIFs currently embedded in the project README. `scripts/` controls
the real macOS app and captures its window. Agents start with [agent.md](agent.md).

## Output

Every generation clears and reuses **`build/demo/`**. It contains only the current
run's working copies, raw recordings, readiness images, contact sheets and report.
There are no timestamped archives. `task demo:clean` empties this workspace.

Final media is written directly to **`docs/screenshots/<target>`**, so added or
changed images appear in `git status` and the README uses the new interface
immediately. A case replaces its previous asset only after capture, encoding,
media probing and window cleanup succeed. A failure keeps the previous final
asset and leaves current diagnostics in `build/demo/`.

Review the resulting media and Git diff. There is no separate promotion command.
Recording never commits, pushes or publishes a release.

## Commands

```sh
task demo:list                       # cases, automation status and README targets
task demo:check                      # fixture, recipe and README-alignment checks
task demo:plan                       # inspect definitions without desktop access
task demo:build                      # standard application, with bundled OCR
task demo:doctor                     # recording tools and executable checks
task demo:record -- pdf-page-drag     # replace docs/screenshots/pdf.gif
task demo:record                     # run the catalog in README order
task demo:clean                      # empty build/demo; final assets stay intact
task demo:clean -- --dry-run          # preview cleanup
```

Python 3.9+, macOS, the project executable, `osascript`, `screencapture`, `cliclick`,
`ffmpeg` and `ffprobe` are needed. Install external recording tools with
`brew install cliclick ffmpeg`. Give the invoking terminal/agent Accessibility,
System Events Automation and Screen Recording access in macOS System Settings.
`task demo:record` builds the standard application before capturing. Keep the desktop free while recording. The window profile is 1000 × 740 points.
An alternative executable can be selected with `--binary /absolute/path`.

## Cases matching the README

| Case | Final asset | Automation |
| --- | --- | --- |
| `overview` | `overview.png` | Scripted |
| `image-crop` | `image-editing.png` | Scripted; uses Lenna |
| `image-ocr` | `ocr.png` | Scripted |
| `pdf-highlight` | `highlight.gif` | Scripted; multiline selection and recoloring |
| `pdf-page-drag` | `pdf.gif` | Scripted; short drag visually verified |
| `pdf-forms` | `forms.png` | Scripted |
| `sign-pdf` | `sign-pdf.gif` | Scripted; uses John Doe |
| `import-signature` | `import-signature.png` | Scripted; uses John Doe |
| `pdf-text-edit` | `pdf-text-edit.gif` | Scripted |
| `pdf-redact` | `redaction.gif` | Scripted |
| `pdf-print` | `printing.png` | Scripted; page selection, canceled before printing |
| `create-pdf` | `create-pdf.png` | Scripted; layout selection, canceled before creating a document |
| `markdown-read` | `markdown.png` | Scripted; local Markdown, Mermaid and floating Contents |
| `stl-orbit` | `stl.gif` | Scripted; uses the supplied STL |

Every case uses the standard executable with `--new-window`, `--work-dir` and
`--data-dir`. Working documents and signature data live under the case's disposable
workspace. The profile has separate application preferences and keychain identity;
it does not restore personal cloud connections. Fyne keeps profile preferences in
its own platform configuration directory, and framework theme settings/OCR runtime
caches remain shared; see [launch options](../docs/development.md#command-line-window-and-dialog-options).

Both signature cases import `johndoe.png` through Signature → Manage signatures →
New signature and the real native file picker. The signing case saves only to its
isolated library, inserts through the signature picker and records a quick drag.
The import screenshot captures the actual vectorization editor and then cancels.
Preparation, native file selection and Fit happen outside the recording. No
separate executable, test launcher or direct GUI-state injection is used.

The additional PDF cases demonstrate text-following highlights, form filling and
small native text edits. Form filling and highlight creation run before capture;
the highlight GIF records only recoloring, and the text GIF records replacing a
word through the actual editor dialog.

## Editing definitions

Each case specifies `id`, `fixture`, `format`, `target`, `readme_section`,
`readme_alt`, `automation`, `setup`, `actions`, `cleanup_undo` and `expected`.
Optional `cleanup` actions dismiss dialogs through their Cancel buttons before
the normal Escape/Undo/window-close sequence. Printing demos never submit a job.
`target` is required and relative to `docs/screenshots/`. The catalog's targets
must exactly match README image references, and their alt text must agree.
**Update the README in the same change whenever a case's purpose, media name or
format changes.** Agents must also update nearby prose when the described
behavior changes. Add new cases only when requested.

`setup` runs before capture. Available actions are `key` (single character or
escape/return/tab/pageup/pagedown, with modifiers), `click`, `drag`, `text` and
`wait`, plus `menu` with a two-label `path` (menu and item) in the owned app,
`choose-color` for a visible hex-color swatch in the centered picker
and `choose-file` for a fixture displayed in the owned native Import
signature picker. It resolves only that GUI process's picker and selects the
matching fixture through accessibility geometry and a double-click. Points are fractions in `[0,1]`, relative to the window by default; use
`space: page` for the detected white PDF page. Each edit counts one Undo in
`cleanup_undo`. Inputs are copied; original fixtures are never edited by recording.
Clicks accept the same `modifiers` as keys, for example Shift-click to select
several thumbnails; the runner releases those modifiers even after an input failure.

GIFs should show a quick gesture with only a short initial/final hold. `seconds`
is the raw recording safety budget. `clip`, for example
`{"before": 0.25, "after": 0.65, "max_seconds": 3}`, trims around actual mouse/typing
input and rejects slow gestures instead of accelerating playback. The page-drag
case produces a roughly 1.6-second GIF from a roughly 0.7-second gesture.

## Tool references

- [cliclick](https://github.com/BlueM/cliclick): input commands and macOS access.
- [FFmpeg filters](https://www.ffmpeg.org/ffmpeg-filters.html): GIF palette encoding.
