# Documentation map

Use this index to find the maintained source for each topic. Update the topic's
own document when behavior changes; keep summaries and links elsewhere brief.
Feature documentation on a prototype branch does not imply a published release.

## Project and operations

| Document | Purpose |
| --- | --- |
| [Project README](../README.md) | Product overview, downloads and contribution entry points |
| [Repository instructions](../AGENTS.md) | Entry point for coding agents |
| [Project conventions](../agent.md) | Architectural invariants and regression constraints |
| [Development and packaging](development.md) | Setup, commands, platform builds and releases |
| [Platform integration backlog](../next-to-do.md) | Remaining desktop integration and manual verification |
| [Website and OAuth branding](github-pages-setup.md) | Pages, domain and Google project setup |
| [Demo fixtures and media automation](../demo/README.md) | Synthetic inputs, screenshot/GIF recipes and agent regeneration workflow |
| [Diagnostic logs](logging.md) | Local rotating logs, crash reports, retention and troubleshooting |
| [Licensing review](licensing.md) | Dependencies, distribution obligations and open licensing work |

## Features and current prototype status

| Document | Purpose |
| --- | --- |
| [Document workspace](document-workspace.md) | Multiple documents per window, PDF append choices and desktop request forwarding |
| [Drawing, highlighting and shapes](annotations.md) | Text-following highlights, freehand nibs, opacity, shape resizing and vector export |
| [Editable PDF annotations](pdf-annotations.md) | Standard save/reopen editing, appearance preservation and interoperability limits |
| [PDF forms](pdf-forms.md) | Implemented behavior, accepted decisions, limits and verification |
| [Document search](text-search.md) | Whole-document native text search and OCR reuse |
| [Native PDF text editing](pdf-text-editing.md) | Implemented prototype, constraints, tests and remaining review |
| [PDF digital signatures](pdf-digital-signatures.md) | Future plan only: detection, verification, signing and long-term validation |
| [OCR selection](ocr-prototype.md) | Current recognition/selection behavior and local evaluation |
| [OCR packaging](ocr-packaging.md) | Bundled engine/models and runtime verification |
| [QR recognition](qr-recognition.md) | Recognition, selection and caching behavior |
| [Signature acquisition](signature-vectorization.md) | Artwork import/vectorization and evaluation |
| [Signature cloud sync](signature-cloud-sync.md) | Provider setup, synchronization and acceptance checks |
| [STL viewer](stl-prototype.md) | Current viewer, controls, renderer and limitations |
| [Native PDF redaction](../internal/pdfengine/README.md) | Engine scope, security checks and ABI provenance |

## Historical evidence and asset provenance

These records retain the assumptions and measurements of their original runs;
use the feature guides above for current implementation status.

- [OCR evaluation results](ocr-prototype-results.md): dated runs and follow-ups.
- [STL feasibility study](stl-viewer-feasibility.md): initial design and benchmarks,
  superseded for implementation status by the STL viewer guide.
- [Application icon](../assets/README.md): design and generation provenance.
- [Tabler assets](../pkg/gui/icons/tabler/README.md): pinned revision and license.
