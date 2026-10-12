# Next platform integration work

Linux file associations are implemented first. The items below describe the
remaining work required to offer the same PDF, PNG and JPEG opening experience
on macOS and Windows without changing the user's default applications.

## macOS

- [x] Add `CFBundleDocumentTypes` entries to the generated `Info.plist` for
      `com.adobe.pdf`, `public.png` and `public.jpeg`.
- [x] Declare Proview as an editor/viewer for those Uniform Type Identifiers,
      without claiming the default-handler role during installation.
- [x] Route Finder/Open With document-open events to the same GUI loading path
      used by Linux command-line arguments.
- [x] Verify that Fyne does not expose a public macOS document-open callback and
      add an Apple Event bridge for files delivered at launch or while running.
- [x] Open the first requested file in the current window when it is empty, and
      preserve existing documents while opening other files in separate windows.
- [x] Update `task package:macos` so document declarations are included in
      `Proview.app` before code signing.
- [x] Re-sign the final bundle after all metadata changes and verify it with
      `codesign --verify --deep --strict` and `plutil -lint`.
- [ ] Test Finder Open With, drag-and-drop onto the application icon, filenames
      containing spaces and Unicode, multiple selected files, and opening files
      while another Proview window is active.

## Windows

- [ ] Embed the application icon and version metadata in `proview.exe`.
- [ ] Create an installer or per-user registration step for PDF, PNG and JPEG
      Open With support.
- [ ] Register a Proview ProgID, `DefaultIcon`, supported MIME/extensions and a
      quoted shell command such as `"proview.exe" "%1"` under the appropriate
      per-user `HKCU\\Software\\Classes` keys.
- [ ] Register application capabilities so Windows Settings can offer Proview
      in the Default Apps UI, without changing defaults automatically.
- [ ] Remove every registry entry created by Proview during uninstall.
- [ ] Route paths received from Explorer to the shared startup-file loader.
- [ ] Decide whether each Explorer request starts a new process or forwards the
      path to an existing Proview process; implement IPC only if single-instance
      behavior is selected.
- [ ] Test Open With, filenames containing spaces and Unicode, multiple selected
      files, long paths, UNC paths, and opening files while Proview is already
      running.

## Shared acceptance criteria

- [ ] PDF, PNG and JPEG files list Proview in the platform Open With UI.
- [ ] Installation never changes the user's default applications implicitly.
- [ ] Unsupported or missing files show a clear error without closing existing
      editor windows.
- [ ] File opening reuses the same validated document loader on every platform.
- [ ] Platform packaging and association behavior are covered by automated
      checks where practical and by a documented manual release checklist.
