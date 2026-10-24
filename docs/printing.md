# Printing

Implemented on `feature/native-printing`; not released.

**File → Print…** / **Command+P** on macOS or **Ctrl+P** on Linux/Windows
prints the active PDF, image or Markdown document. Images created by Crop or New
from clipboard can be printed before saving. STL printing is not implemented.

For multipage PDFs, choose **All pages**, **Current page**, or **Page range**.
Ranges such as `1-3, 5` are one-based, deduplicated and printed in document
order. Continue opens the system print dialog for printer, paper, copies,
orientation, duplex and other available driver options. Page numbers in that
dialog refer to the prepared selection. Single-page PDFs and images go directly
to preparation and the system dialog.

## Markdown layout

Print the complete document, independently of scroll position, window size,
reading zoom and application theme. A native Go renderer generates searchable
PDF text on white A4 pages with 18 mm margins and page numbers, then passes the
PDF through the same native print backend. Other paper sizes can be selected in
the system dialog and fitted there. Markdown does not have a reading page index;
native page ranges, where available, refer to the generated print pages.

The layout includes headings, emphasis, inline code, links, nested lists, task
checkboxes, quotes, rules, syntax-colored code, local images and Mermaid diagrams.
Long prose and code lines wrap within the margins. Code blocks that fit on one
page stay together; longer blocks continue at line boundaries. Images and
diagrams stay together and scale down to fit a page. Mermaid is rendered offline
with a white background, independently of pending reading-view jobs; unsupported
or limited diagrams retain their source and a short fallback message.

Tables wrap cells and repeat their header when continued. Long rows can span
pages. Tables with more columns than fit legibly, or unusually tall headers, become
labeled records to keep every value readable. Images inside table cells appear
below the table, with alt-text references in their source cells.

Use the bundled Go Sans and Go Mono font families, including bold/italic faces.
Glyph coverage follows these fonts; supplementary Unicode characters use a
replacement glyph because the PDF font subsetter supports BMP characters.
HTML and formulas remain literal source, as in the reader. Only preloaded local
images are used; printing never follows remote image URLs. Repeated images share
one PDF resource. Print layout is bounded to 1,000 pages, 16 Mermaid previews,
48 MiB of encoded images and 64 MiB of output PDF.

## Document preparation

- Commit pending annotation and field edits, then capture an immutable rendering
  snapshot. Preparation and printing run outside the Fyne event loop.
- Include current page order, rotations, image crop, overlays, filled forms,
  native text edits and redactions. PDF preparation uses the normal export
  pipeline; errors never fall back to the original, unsanitized PDF.
- Selecting PDF pages preserves their native text and vector artwork. Existing
  PDF pages are never replaced by screenshots for the prepared PDF.
- Render images with their edits, flatten transparency onto white paper, and
  embed the result in a one-page PDF. Fit to the printable area while retaining
  proportions; actual paper settings are selected in the system dialog.
- Printing does not save, mark the document saved, change its destination or
  alter Undo/Redo. The editor remains reserved during the print dialog/job to
  keep save, close and external file-opening requests from replacing its source.
- A private temporary directory holds the prepared PDF until the backend has
  consumed it. Remove it on success, cancellation and errors. Normal completion
  means the system accepted the job, not that a physical printer finished it.

## Platform integration

- **macOS:** PDFKit's print operation and AppKit's native print panel. The PDF
  is submitted directly, with fit scaling and automatic rotation. Retain the
  PDFDocument through the synchronous native dialog and spooling operation.
- **Linux:** D-Bus `org.freedesktop.portal.Print`, using **PreparePrint** and its
  token for **Print**. Pass an open PDF file descriptor; no GTK/WebKit/WPE build
  dependencies or external document viewer are required. CUPS scaling hints
  request fit without clipping. This depends on the desktop's print portal and
  configured printing service; unavailable portals produce an error.
  X11/XWayland dialogs use the window's X11 identifier. Native Wayland falls
  back to an unparented portal dialog because Fyne exposes a raw surface, not
  an exported xdg-foreign window token.
- **Windows:** `PrintDlgExW`, followed by a printer device context on the same
  COM-initialized thread. The driver handles copies and collation. Render one
  selected page at a time at the printer's resolution, capped at 600 DPI and
  about 32 million pixels, then draw it centered in the printable area. This
  rasterization is only for the printer device, not PDF export. MuPDF's Print
  usage honors annotation print flags, including print-only appearances.

## Verification

Automated coverage checks native annotation print flags, bounded rendering,
vector page selection, unsaved artwork, cropped transparent images, export
failure without a source fallback, range validation, GUI cancellation/errors,
save-state preservation and temporary-file cleanup.

Local macOS arm64 verification opened the AppKit print panel from Command+P
and canceled it successfully, returning to the document. No physical print job
was sent. The printing backend also cross-compiles for Linux amd64 and Windows
amd64; desktop/printer runtime checks on those platforms remain pending.

Manual checks before release:

- Open/cancel the native panel on each supported desktop.
- Print a changed PDF, a filled form, an unsaved crop, a landscape image and a
  transparent image; check selected pages, margins, proportions and output colors.
- Check paper size, multiple copies and duplex on actual printer drivers.
- On Ubuntu 24.04 GNOME/Wayland, verify the portal and dialog placement.
- Check virtual PDF printers separately from physical printers.
- Review Markdown pagination, repeated table headers, long code lines, Unicode,
  local images and light Mermaid output. The Markdown extension has been
  compile-checked; its print/layout tests and desktop review are pending.

References: [Apple PDFKit](https://developer.apple.com/documentation/pdfkit/write-operations),
[XDG Print portal](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.Print.html),
[Windows printing](https://learn.microsoft.com/en-us/windows/win32/printdocs/how-to--print-from-a-windows-application).
