# Platform integration backlog

Reviewed 2026-10-16 against the current implementation. This file tracks desktop
integration only; [the documentation map](docs/README.md) links feature-specific
plans and remaining work. Build/installation instructions belong in
[Development and packaging](docs/development.md).

## Implemented

- Linux per-user installation, launcher, icon and Open With associations for
  PDF, supported images and STL, without changing defaults.
- macOS bundle document declarations and Finder/Open With Apple Event handling;
  requests use the shared document-opening flow described in
  [Document workspace](docs/document-workspace.md).
  Packaging signs the final bundle and checks metadata/signature integrity.
- Windows embedded application icon, shared startup-file loading and automatic
  per-user Open With registration on launch. Moving the executable updates the
  registered path; no administrator privileges or implicit default changes.

## Remaining work

- [ ] macOS: manually test Finder Open With, dropping onto the app icon, multiple
      selected files, spaces/Unicode and opening while other windows are active.
- [ ] Windows: manually test Open With/Default apps, multiple files, spaces/Unicode,
      long/UNC paths, moved executable registration and already-running windows.
- [ ] Windows: evaluate native executable version resources separately from the
      embedded icon and in-app About metadata.
- [ ] Windows: decide whether to add an installer/uninstaller or a command to
      remove portable per-user registration. Document cleanup behavior.
- Local implementation: desktop startup requests now forward to an existing
  process; see [Document workspace](docs/document-workspace.md). Verify Explorer
  forwarding on Windows before recording native acceptance.
- [ ] All platforms: verify PDF, supported images and STL association coverage;
      missing/unsupported files must preserve other open documents and report errors.
- [ ] Record native desktop acceptance results; automated packaging checks and
      cross compilation alone do not establish runtime integration.
