# Initial OCR prototype evaluation

Measured on 2026-10-08, Apple M4, macOS 26.5.1, Go 1.26.0 and Tesseract 5.5.3.
The OCR backend is the same external Tesseract implementation intended for Windows
and Linux; no Apple recognition framework was used. Models: pinned `tessdata_fast`
English and Italian, as specified in `packaging/ocr_models.py`.

## Recorded run

These are single-run measurements on generated fixtures, after engine setup.
They are not statistically representative latency estimates or desktop UX tests.
The reproducible command is `task ocr:probe`; results may vary across machines.

| Fixture | Recognition path time | Word accuracy | Recognized / expected words |
| --- | ---: | ---: | ---: |
| screenshot.png | 201 ms | 100.00% | 46/46 |
| scan-a4.png | 582 ms | 100.00% | 138/138 |
| simulated-photo.png | 229 ms | 89.13% | 46/46 |
| two-columns.png | 349 ms | 100.00% | 104/104 |
| blank.png | 91 ms | 100.00% | 0/0 |
| native.pdf | 3 ms | 100.00% | 8/8 |
| scanned.pdf | 740 ms | 100.00% | 138/138 |
| mixed.pdf | 2 ms | 5.48% | 8/146 |

The clean screenshot, A4 image and scanned PDF meet the preliminary 95% accuracy
and three-second recognition-path targets on this hardware. The two-column
fixture also preserves reading order. The blank image produces no words.

The simulated photograph reaches 89.13% accuracy, below the clean-document target.
It introduces slant and an illumination gradient, but it does not establish
quality on real photographs, perspective distortion, glare or handwriting.

The mixed PDF intentionally demonstrates the prototype boundary: its eight native
words are selectable, while its scanned text is not recognized. Its low full-page
score is expected, not a claim that mixed-page support is complete. The next
iteration needs region recognition and duplicate suppression before enabling
OCR alongside native text.

The generated JSON records Go allocations/heap, but excludes native allocations
and the OCR process. Whole-process peak memory and user-perceived selection
latency remain manual measurements. The viewer caches word layouts only (20 pages);
the test suite checks cache reuse and eviction without re-running recognition.

## Verification status

- `task check`: all Go tests and vet passed on macOS ARM64, including native PDF
  redaction regressions and the new text selection, clipboard and OCR tests.
- Real Tesseract recognition: all eight generated cases executed on macOS ARM64.
- `task build`: the macOS ARM64 application built successfully. Launching
  `task ocr:run -- build/ocr-corpus/screenshot.png` in the native app completed
  recognition of 46 words in 254 ms. Screen capture was unavailable, so this is
  an application startup/OCR smoke check, not a visual acceptance test.
- Standalone OCR tests cross-compiled successfully for Windows AMD64 and Linux
  AMD64; neither binary was run on its target OS.
- Desktop GUI gestures are exercised through Fyne's test driver. Copy/paste into
  an external desktop application, real photographs and visual interaction checks
  still require the manual checklist in `ocr-prototype.md`.
- Windows/Linux desktop runtime and native packaging are not verified in this
  environment. Docker was unavailable. Cross-compiling the standalone OCR test
  binary does not establish that the full application works on those desktops.

## Recommendation

Continue with Tesseract for the next evaluation: clean printed text already meets
the prototype targets and requires no OS-specific recognition code. Keep the
feature opt-in until real desktop interaction, real-world photographs and the
Windows/Linux runtime are evaluated. Prioritize mixed-page handling and tilted
text geometry before bundling binaries and models into release packages.

## Camera photograph follow-up

A real local camera JPEG exposed two weaknesses in the initial pipeline: EXIF
orientation was ignored on loading, and high-resolution paper texture/uneven
lighting confused page segmentation. JPEG orientation is now normalized when
loading, including all eight EXIF orientations. OCR inputs are downsampled to a
maximum long side of 2400 pixels and Tesseract uses Sauvola adaptive thresholding
(`thresholding_method=2`). Original source bytes and display/export resolution
are preserved; only the recognition input is reduced.

On the same local camera sample, the normalized-orientation baseline returned
12 words. The updated application recognition path returned 73 words in 1.872
seconds, with 62 words reporting confidence of at least 80 and a mean confidence
of 88.7. Counts and confidence are not transcription accuracy. The private photo
and its recognized text are not included in the corpus or committed documentation.

The synthetic corpus was rerun with these settings:

| Fixture | Recognition path time | Word accuracy | Recognized / expected words |
| --- | ---: | ---: | ---: |
| screenshot.png | 197 ms | 100.00% | 46/46 |
| scan-a4.png | 1773 ms | 100.00% | 138/138 |
| simulated-photo.png | 346 ms | 89.13% | 46/46 |
| two-columns.png | 390 ms | 100.00% | 104/104 |
| blank.png | 99 ms | 100.00% | 0/0 |
| native.pdf | 3 ms | 100.00% | 8/8 |
| scanned.pdf | 780 ms | 100.00% | 138/138 |
| mixed.pdf | 2 ms | 5.48% | 8/146 |

Clean printed-text cases retain 100% word accuracy. The synthetic tilted photo
remains at 89.13%, so the update does not eliminate perspective/skew limitations.
The A4 PNG path includes downsampling from its original size; timings are single
runs and can vary with concurrent work. JPEG orientation, malformed metadata,
view/OCR/export alignment and downsampling geometry have regression coverage.

## Automatic rotation follow-up

Recognition now estimates and corrects a dominant text angle on a temporary
expanded canvas, then maps word quadrilaterals back to the source coordinates.
No perspective correction is applied. A local tilted camera sample that produced
zero words with the previous path now produces 33 words in 1.988 seconds, with
26 reporting confidence of at least 80 and mean confidence 85.8. The previous
camera sample retains 73 words, 62 with confidence of at least 80, and mean
confidence 88.7 (1.951 seconds). Counts and engine confidence do not establish
transcription accuracy; neither private photo nor recognized text is stored here.

The generated simulated photograph improves from 89.13% to 97.83% word accuracy
(46/46 words returned, 349 ms). The screenshot, A4 scan, two-column image and
scanned PDF retain 100% word accuracy; blank input retains zero words. Native and
mixed PDF behavior is unchanged. These are single-run recognition-path timings,
not a desktop UX or cross-platform runtime validation.

## Bundled distribution verification

All five application targets were built locally with embedded OCR assets:
macOS ARM64/AMD64 app bundles, Linux ARM64/AMD64 AppImages and a Windows AMD64
GUI executable. Both macOS app signatures verified; both macOS bundles and both
Linux AppImages passed the bilingual `--check-ocr` recognition test. The macOS
checks hid system Tesseract from PATH and set an invalid external model directory.
The AMD64 Linux AppImage was checked in an Ubuntu 22.04 container, using the
existing Rosetta-compatible copy of the AppImage header; the distributed file
was not patched.

The Windows dependency audit rejected non-system DLLs, and the generated ZIP
contains exactly one `proview.exe`. The executable is approximately 84 MiB and
the ZIP approximately 38 MiB. Its OCR payload includes engine/model licenses and
GNU/MinGW runtime notices. Windows execution is not verified on a native Windows
host here: a Wine attempt failed in Rosetta with an invalid GDT selector before
the Go test program could run. Native CI now tests bundled extraction and
recognition on each target before uploading release artifacts, including a
Windows cache path containing non-ASCII characters. These workflow changes have
not been run on GitHub during this session.

Ordinary Go tests/vet, all 28 Python packaging tests, and OCR race checks passed.
Bundled recognition on the two local camera photos retained 33 and 73 detected
words respectively, with the previous confidence counts unchanged. No private
photo or recognized text was added to the source tree. No release was published
as part of these local packaging checks.
