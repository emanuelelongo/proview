# Instructions for agents generating README demos

Read [../agent.md](../agent.md), [README.md](README.md), the project
[README](../README.md), and [cases.json](cases.json).

## Scope

For now, keep the catalog aligned with the eight media files already in the
project README. Do not add form/search/drawing or other new cases unless requested.
When asked to regenerate demos, execute the requested cases (all if unspecified),
inspect the output, correct problems and repeat until the captures are suitable.

All eight cases have scripted recipes. Signature scenes use `johndoe.png` from
the Desktop, copied into fixtures. The crop scene uses Lenna; keep its provenance
and copyright notice in `fixtures/README.md`.

## Workflow

1. Read the selected definitions and inspect their existing README assets. Keep
   the same feature/purpose while showing the current app interface.
2. Run `task demo:check` and `task demo:plan`. Update missing or uncalibrated
   recipes as necessary. Fixtures are committed files: edit them directly only
   when their content needs to change, then update their SHA-256 entries in
   `fixtures/manifest.json`. Preserve native PDF text/forms and the image-only OCR
   page. No fixture generator or separate model source copy is needed.
3. Build the current tree with `task demo:build`, then run `task demo:doctor`.
   Prerequisite and macOS permission failures must be reported accurately; a dry
   run is not desktop validation. Follow existing user authorization for tools.
4. Run `task demo:record -- <case-id>` or the full catalog once its recipes work.
   It clears **build/demo at the start of each generation** and reuses that one
   workspace. Final PNG/GIF files automatically replace their declared targets in
   **docs/screenshots**. This is the authorized output location: no extra approval
   or promotion step is needed. Do not commit, push or release unless requested.
5. Inspect every generated image and animation. Use contact sheets and sample the
   full motion; check all `expected` conditions, loaded thumbnails, actual reorder,
   crop bounds, clipping and dialogs. A file or successful exit is insufficient.
   Prefer quick gestures and brief GIFs, with setup outside recording and only a
   short final hold. Never fabricate UI screenshots or speed up a slow gesture.
6. On failure, inspect `build/demo/report.json`, the case's `launch.log`,
   `ready.png`, `geometry.png` and raw movie. Fix and rerun. The next generation
   removes these diagnostics, so read them first. A failed case retains its old
   README asset. If visual review rejects a published capture, correct and rerun
   it before reporting completion; restore that asset from the prior working-tree
   contents if the task cannot be completed. Preserve unrelated existing edits.
7. **Whenever a case changes, update the project README in the same change:**
   image href/src, alt text, section placement and feature prose must match its
   target and actual behavior. Keep case `readme_alt` equal to the README alt text.
   Validate the catalog/README correspondence with `task demo:check`. In this
   phase retain the existing eight media slots, filenames and formats.
8. Review Git status/diff: final assets should appear as added/modified files in
   `docs/screenshots`, while intermediates remain ignored. Write brief visual
   results in `build/demo/review.json` if useful; it is disposable, not a history.
   Run `task demo:clean` after inspection if requested. It empties only build/demo,
   retaining final media, executable, OCR caches and other build directories.

## Desktop boundaries

- Capture real Proview windows only. The profile is a 1000 × 740 point window at
  (80,60); use normal UI scale and sufficient display space. Window coordinates
  include the title bar; normalized PDF coordinates use the detected paper edge.
- The runner uniquely identifies its own window and reuses the discovered PID.
  Do not touch unrelated windows or quit/kill a shared Proview application.
  Keep the desktop free of other input while recording.
- Each case opens working copies using the standard executable with
  `--new-window --work-dir ... --data-dir ...`. The last option selects an isolated
  signature store, application preferences and keychain/IPC identity. It must be
  supplied before files. Fyne stores the distinct profile's application preferences
  in its platform configuration directory; its global theme and the OCR cache are
  shared. Never add John Doe to the user's normal signature library or configure
  cloud sync in a demo profile.
- Signature cases acquire `johndoe.png` through the real UI and native file picker.
  `choose-file` is setup-only: verify an osascript child of the owned GUI, locate
  the fixture in that picker, then double-click its actual accessibility geometry.
  Do not send file paths to an unrelated frontmost dialog. Keep fixtures visible
  in the initial `--work-dir`; preserve native picker's calibrated icon view.
- Undo each completed edit before closing the owned window. A failed run leaves
  its window for inspection; Escape/Undo/discard only there. Never save into the
  fixture directory or use a personal document as demo input.
- Recording and cleanup share a workspace lock. After an ungraceful runner kill,
  remove `build/.demo-recording` only after confirming the runner has exited.
- Local desktop capture is documentation tooling, not a replacement for PDF,
  export or security tests. Do not add desktop recording to PR CI.

## Current validation

`pdf-page-drag` was visually verified on macOS at 1000 × 740 points: loaded
thumbnails, insertion feedback, page 02 moved before page 01, six pages retained,
roughly 0.7 s input and a 1.58 s final GIF. The transient Updating pages overlay is
normal UI. All eight README cases now have scripted automation. The crop uses
Lenna, and signing/import use the supplied John Doe example in an isolated native
application profile. Visual validation covers the complete catalog, including corrected
OCR hit coordinates and returning to page 05 after Fit before moving a signature.
GIF gestures are short and retain only a brief lead-in/final hold.
