# Licensing and dependency review

Reviewed on 2026-10-11 against the dependencies pinned in `go.mod` and the
OCR inputs pinned in `packaging/ocr_runtime.py` and `packaging/ocr_models.py`.
This is a source review, not a statement that release distribution compliance
is complete. New dependency versions require another review.

## Project license

Proview's original code is licensed under **AGPL-3.0-or-later**. The unmodified
license text is in [LICENSE](../LICENSE); the project grant and copyright notice
are in [NOTICE](../NOTICE). Third-party code and assets retain their own terms.
The application license does not license users' documents or signatures.

MuPDF 1.24.9 is linked into the executable by go-fitz and the native PDF bridge.
Its [version-specific license declaration](https://github.com/ArtifexSoftware/mupdf/blob/1.24.9/README)
explicitly permits AGPL v3 or any later version. Its full license text is also
retained in `internal/pdfengine/COPYING`. This is the principal reason for
choosing AGPL for Proview's combined application.

## Review scope and findings

`go list -deps -e -json ./cmd/gui`, with `CGO_ENABLED=1`, resolved 76 external
runtime modules across macOS ARM64/AMD64, Linux ARM64/AMD64 and Windows AMD64
without package resolution errors. This checks imports and license texts, not
runtime support on those platforms. The module graph also contains development
and test dependencies that are not all linked into the application.

Most runtime dependencies use MIT, BSD, Apache-2.0 or ISC terms. Their notices
must still be retained when distributing binaries. Noteworthy exceptions:

- `github.com/benoitkugler/textprocessing` includes LGPL-2.1 and separate Pango,
  FriBidi and Fontconfig notices. Retain those texts and the corresponding source.
  LGPL-2.1 section 3 permits conversion of a copy to GPL version 2 or later;
  AGPL v3 section 13 permits combination with GPL v3. Record the selected route
  and include the relevant license texts before binary distribution.
- Both FreeType Go implementations offer the FreeType License or GPL-2.0-or-later.
  The FreeType route requires preservation of its notices and attribution.
  Their separately licensed Luxi test fonts are outside the runtime import audit.
- `github.com/srwiley/scanx` has no root license file in the pinned revision
  ([source](https://github.com/srwiley/scanx/tree/e94503791388)).
  `scan.go` has a FreeType/GPL-2.0-or-later header, but `span.go` has no explicit
  grant. Do not infer that the whole module is BSD from nearby dependencies.
  Clarify the terms for the complete module or replace the dependency before
  treating the binary license audit as complete.
- `github.com/BurntSushi/xgbutil` uses WTFPL v2.
- Latin Modern font data uses the GUST Font License, based on LPPL 1.3c-or-later;
  its Go resource wrappers use BSD. Keep font data unmodified and retain both
  sets of notices. Go Sans/Mono use BSD notices.
- MuPDF's prebuilt `libmupdfthird` libraries include native dependencies outside
  the Go module inventory. Their actual compiled components, notices and
  corresponding sources still need an explicit inventory.

## Bundled OCR and assets

| Component | Pinned input | License |
| --- | --- | --- |
| Tesseract | 5.5.3 | Apache-2.0 |
| English/Italian tessdata_fast models | Revision and hashes in `packaging/ocr_models.py` | Apache-2.0 |
| Leptonica | 1.85.0 | BSD-2-Clause |
| libpng | 1.6.50 | PNG Reference Library License; retain complete upstream text |
| zlib | 1.3.1 | zlib |
| Windows GCC runtime | Toolchain runtime; notices pinned to GCC 15.2.0 | GPL v3 with GCC Runtime Library Exception 3.1; verify actual toolchain version |
| MinGW-w64 / winpthreads | Notices pinned to v12.0.0 | Multiple upstream terms; verify actual toolchain version |
| Tabler Icons | Revision recorded in `agent.md` | MIT |
| Proview application icon | `assets/app-icon.png` | Project grant; provenance in `assets/README.md` |

The OCR packaging code embeds upstream license texts and a manifest with source
versions and hashes in each runtime archive, including the model license and
Windows runtime notices. The local pinned source license texts were reviewed.
The runtime archives are extracted into the user cache when OCR is first used.
The GNU runtime exception must be considered together with its GPL text, rather
than treating the Windows runtime as unqualified GPL code.

## Work remaining for distributed packages

Adding the root license establishes the project grant; it does not finish the
binary distribution work. Before declaring distribution compliance complete:

1. Resolve the incomplete license declaration in the pinned `scanx` module.
2. Collect the complete notices for linked Go and native components, including
   nested notices, MuPDF's native dependencies, OCR and font terms. The current
   **Help → Third-Party Licenses** screen covers a subset, not all 76 modules.
3. Include the project `LICENSE`, `NOTICE` and complete third-party notices with
   every platform package, accessible without requiring OCR extraction.
4. Provide the corresponding source for the exact released build, including
   linked dependencies and build scripts. A GitHub source archive of Proview
   alone omits downloaded Go modules and native OCR/MuPDF sources. Use an
   appropriate AGPL section 6 distribution method; a private repository link
   does not provide source access to public binary recipients.
5. Verify each distributed artifact and the source-access links, and reconcile
   runtime notices with the compiler/toolchain versions actually used.

## Runtime Go module inventory

Labels below summarize top-level license texts; they do not replace upstream
copyright notices or file-specific terms. The exact versions identify the
reviewed source in the Go module cache and upstream repositories.

| Module | Version | License summary |
| --- | --- | --- |
| `al.essio.dev/pkg/shellescape` | `v1.5.1` | MIT |
| `codeberg.org/go-pdf/fpdf` | `v0.11.1` | MIT |
| `fyne.io/fyne/v2` | `v2.7.4` | BSD (see exact upstream text) |
| `fyne.io/systray` | `v1.12.1` | Apache-2.0 |
| `github.com/BurntSushi/freetype-go` | `v0.0.0-20160129220410-b763ddbfe298` | FreeType License OR GPL-2.0-or-later |
| `github.com/BurntSushi/graphics-go` | `v0.0.0-20160129215708-b43f31a4a966` | BSD (see exact upstream text) |
| `github.com/BurntSushi/toml` | `v1.5.0` | MIT |
| `github.com/BurntSushi/xgb` | `v0.0.0-20210121224620-deaf085860bc` | BSD (see exact upstream text) |
| `github.com/BurntSushi/xgbutil` | `v0.0.0-20190907113008-ad855c713046` | WTFPL-2.0 |
| `github.com/ByteArena/poly2tri-go` | `v0.0.0-20170716161910-d102ad91854f` | BSD (see exact upstream text) |
| `github.com/andybalholm/brotli` | `v1.1.1` | MIT |
| `github.com/aws/aws-sdk-go-v2` | `v1.47.1` | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream` | `v1.7.20` | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/credentials` | `v1.20.6` | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/internal/configsources` | `v1.5.4` | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2` | `v2.8.4` | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/internal/v4a` | `v1.5.4` | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding` | `v1.13.19` | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/internal/checksum` | `v1.11.5` | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url` | `v1.14.4` | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/internal/s3shared` | `v1.20.4` | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/s3` | `v1.114.0` | Apache-2.0 |
| `github.com/aws/smithy-go` | `v1.28.1` | Apache-2.0 |
| `github.com/benoitkugler/textlayout` | `v0.3.1` | MIT |
| `github.com/benoitkugler/textprocessing` | `v0.0.3` | LGPL-2.1; nested notices also apply |
| `github.com/clipperhouse/uax29/v2` | `v2.7.0` | MIT |
| `github.com/danieljoos/wincred` | `v1.2.2` | MIT |
| `github.com/davecgh/go-spew` | `v1.1.1` | ISC |
| `github.com/ebitengine/purego` | `v0.10.1` | Apache-2.0 |
| `github.com/fredbi/uri` | `v1.1.1` | MIT |
| `github.com/fsnotify/fsnotify` | `v1.9.0` | BSD (see exact upstream text) |
| `github.com/fyne-io/image` | `v0.1.1` | BSD (see exact upstream text) |
| `github.com/fyne-io/oksvg` | `v0.2.0` | BSD (see exact upstream text) |
| `github.com/gen2brain/go-fitz` | `v1.24.15` | AGPL-3.0 (MuPDF: or-later) |
| `github.com/go-fonts/latin-modern` | `v0.3.3` | BSD for Go resources; GUST/LPPL for fonts |
| `github.com/go-gl/gl` | `v0.0.0-20231021071112-07e5d0ea2e71` | MIT |
| `github.com/go-gl/glfw/v3.3/glfw` | `v0.0.0-20250301202403-da16c1255728` | BSD (see exact upstream text) |
| `github.com/go-text/render` | `v0.2.1` | BSD (see exact upstream text) |
| `github.com/go-text/typesetting` | `v0.3.4` | BSD (see exact upstream text) |
| `github.com/godbus/dbus/v5` | `v5.1.0` | BSD (see exact upstream text) |
| `github.com/golang/freetype` | `v0.0.0-20170609003504-e2365dfdc4a0` | FreeType License OR GPL-2.0-or-later |
| `github.com/hhrutter/tiff` | `v1.0.6` | BSD (see exact upstream text) |
| `github.com/jeandeaual/go-locale` | `v0.0.0-20250612000132-0ef82f21eade` | MIT |
| `github.com/jsummers/gobmp` | `v0.0.0-20230614200233-a9de23ed2e25` | MIT |
| `github.com/makiuchi-d/gozxing` | `v0.1.1` | MIT |
| `github.com/mattn/go-runewidth` | `v0.0.30` | MIT |
| `github.com/ncruces/zenity` | `v0.10.15` | MIT |
| `github.com/nfnt/resize` | `v0.0.0-20180221191011-83c6a9932646` | ISC |
| `github.com/nicksnyder/go-i18n/v2` | `v2.6.0` | MIT |
| `github.com/pdfcpu/pdfcpu` | `v0.16.0` | Apache-2.0 |
| `github.com/pmezard/go-difflib` | `v1.0.0` | BSD (see exact upstream text) |
| `github.com/rymdport/portal` | `v0.4.2` | Apache-2.0 |
| `github.com/srwiley/oksvg` | `v0.0.0-20221011165216-be6e8873101c` | BSD (see exact upstream text) |
| `github.com/srwiley/rasterx` | `v0.0.0-20220730225603-2ab79fcdd4ef` | BSD (see exact upstream text) |
| `github.com/srwiley/scanx` | `v0.0.0-20190309010443-e94503791388` | Partial FreeType/GPL header; clarification needed |
| `github.com/stretchr/testify` | `v1.11.1` | MIT |
| `github.com/tdewolff/canvas` | `v0.0.0-20250728095813-50d4cb1eee71` | MIT |
| `github.com/tdewolff/font` | `v0.0.0-20250430140153-b654fd8acba3` | MIT |
| `github.com/tdewolff/minify/v2` | `v2.23.4` | MIT |
| `github.com/tdewolff/parse/v2` | `v2.8.0` | MIT |
| `github.com/yuin/goldmark` | `v1.7.11` | MIT |
| `github.com/zalando/go-keyring` | `v0.2.6` | MIT |
| `go.yaml.in/yaml/v3` | `v3.0.5` | MIT and Apache-2.0 notices |
| `golang.design/x/clipboard` | `v0.9.0` | MIT |
| `golang.design/x/x11` | `v0.2.0` | MIT |
| `golang.org/x/crypto` | `v0.57.0` | BSD (see exact upstream text) |
| `golang.org/x/image` | `v0.46.0` | BSD (see exact upstream text) |
| `golang.org/x/net` | `v0.58.0` | BSD (see exact upstream text) |
| `golang.org/x/oauth2` | `v0.36.0` | BSD (see exact upstream text) |
| `golang.org/x/sys` | `v0.48.0` | BSD (see exact upstream text) |
| `golang.org/x/text` | `v0.42.0` | BSD (see exact upstream text) |
| `golang.org/x/xerrors` | `v0.0.0-20200804184101-5ec99f83aff1` | BSD (see exact upstream text) |
| `gopkg.in/yaml.v3` | `v3.0.1` | MIT and Apache-2.0 notices |
| `modernc.org/knuth` | `v0.5.5` | BSD (see exact upstream text) |
| `modernc.org/token` | `v1.1.0` | BSD (see exact upstream text) |
| `star-tex.org/x/tex` | `v0.7.1` | BSD (see exact upstream text) |

## Native Markdown dependencies

The native Markdown viewer adds Chroma v2.20.0 and its regexp2 v1.11.5 dependency,
and directly uses Goldmark v1.7.11, already present through Fyne. All three use
MIT licenses. Their pinned license texts are retained under
`internal/markdown/licenses` and included in Help → Third-Party Licenses.
This addition does not close the distribution work listed above.

The Mermaid adapter imports sketch core and raster from
`v0.3.2-0.20261024080457-fed94a1a2d22`. The MIT notice identifies Pele and the
Go port. Its rasterizer bundles subsets of Arimo under OFL-1.1 and Go Mono under
BSD terms. The unmodified source license and complete font notice are copied to
`internal/markdown/licenses/Sketch-Pele-MIT.txt` and `Sketch-Fonts.txt`, embedded
in the existing Third-Party Licenses screen. Tuios CLI/terminal packages and
its plot library are not imported by the rendering adapter.
