# QR recognition

Images and PDF source pages are scanned automatically in the background, even
when OCR is unavailable. In the Select tool, click a QR code to highlight it and
show its decoded content in the lower status bar. Copy works through the usual
keyboard shortcut and the selection context menu. Escape clears the selection.

HTTP/HTTPS addresses and bare web domains become clickable in the status bar.
Clicking the QR itself selects its content; clicking the status link opens the
browser. Other payloads, including Wi-Fi configuration and contact cards, remain
copyable text. Proview does not send images or decoded payloads to a service.

Recognition uses [gozxing v0.1.1](https://github.com/makiuchi-d/gozxing/tree/v0.1.1),
a pure Go QR decoder with MIT and original ZXing Apache License 2.0 notices. Multiple QR codes,
Unicode payloads, rotation and mirrored codes are supported. Finder/alignment
geometry maps each result to a normalized quadrilateral for selection.

Each editor has a separate worker and a cache of up to 20 pages. Visible/active
PDF pages are prioritized. Snapshots isolate recognition from editing; source
keys invalidate results after cropping, rotation, page replacement or redaction.
PDFs with redactions are sanitized before recognition, including affected shared
image reuses. A sanitation failure never falls back to the original source.
Recognition does not change history or saved files.

Raster input is limited to a 2400-pixel long side. Small, blurry or heavily
distorted codes may be missed. Recognition targets the underlying document;
editable overlay objects are excluded. GIF and TIFF use the displayed first
frame/page. Other barcode formats are not recognized.
