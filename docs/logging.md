# Local diagnostic logs

Implemented on `improvements/editor-workflow`, 2026-10-17. Packaging is separate.

Help > Open logs folder flushes pending records and opens the directory:

| Platform | Directory |
| --- | --- |
| macOS | `~/Library/Logs/Proview` |
| Windows | `%LOCALAPPDATA%\Proview\logs` |
| Linux | `$XDG_STATE_HOME/proview/logs`, default `~/.local/state/proview/logs` |

`proview.log` retains ordinary Go application and Fyne log output, alongside the
existing terminal output. Rotation keeps three numbered backups with a 2 MiB
limit per file (about 8 MiB total). An OS file lock coordinates batches and
rotation across multiple application processes. Each batch reopens the current
file, so another process can rotate it safely.

The disk worker accepts a bounded queue of 256 records, truncates individual
records at 8 KiB and flushes every two seconds or approximately 64 KiB. Idle
flush ticks perform no filesystem operations. Normal logging never waits for
file I/O; overload drops records and reports their count. Shutdown and opening
the logs folder explicitly drain/flush the queue. File creation/rotation and
flush errors do not stop document operations. There is no per-record fsync.
An abrupt termination may lose the last buffered ordinary records.

`crash.log` receives unrecovered Go panic/runtime crash reports directly through
`runtime/debug.SetCrashOutput`, even for crashes in background goroutines.
It is separate from the buffered logger and is rotated at startup if it has
exceeded 2 MiB, retaining three backups. A crash report can exceed that size;
rotation occurs on the next launch. The runtime descriptor remains available
through deferred main cleanup. This does not promise recovery from crashes or
capture of native-library stderr, external subprocess output or OS crash reports.

Startup records include version, commit, platform, architecture, Go version,
PID and OCR availability. Operation-boundary records cover document opening,
switching/closing, PDF append/window decisions, saves, native PDF text editing,
file work and recognition/search failures. Errors include window/document IDs,
format, page count, revision, dirty/redaction state and page/operation context
where available. Normal fast recognition, mouse motion, zoom, scrolling and
per-frame rendering are not logged. Search summaries omit the query and text.

No document text, image data, clipboard content, private keys, passwords or
OAuth tokens are intentionally logged. Raw underlying error messages and crash
stacks may include file names or local paths; review logs before sharing them.
Logs remain local. `--check-ocr` retains its standalone terminal-only behavior.

Error dialogs use operation-specific plain language. Full underlying details go
to diagnostic logs, including drawing page, kind, style, sample count, width and
opacity, without stroke coordinates or document text. File, clipboard, page,
form, object, signature and cloud failures use the same reporting boundary.
Locally authored input-validation messages remain specific to the entered value.
Inline signature import/cloud errors also avoid raw engine/server responses;
cloud status and signature tracing suppress repeated identical diagnostics.
URLs in error details omit user information and queries; common credential/token
fields are masked. Reports remain asynchronous and bounded through the existing
logger, with no mouse-motion or per-frame logging.

Native text-edit errors retain specialized plain-language messages.
Engine diagnostics use "text editing" rather
than "correction"; historical internal API/type names remain unchanged.

Verification: `go test ./...`, `go vet ./...`, and
`go test -race ./internal/applog`. Tests cover rotation/retention, concurrent
writers, slow-disk overload, truncation, flush/close and a real crash subprocess.
GUI tests verify the Help entry, operation-specific dialogs, drawing context,
credential masking and cloud diagnostic deduplication. An AST check keeps direct
error dialogs inside the reporting boundary. Text-edit tests verify that dialogs
omit engine details while logs retain the technical reason and page number. Windows logging code
is cross-compiled; native Windows folder opening/rotation needs desktop acceptance.
