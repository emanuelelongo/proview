# PDF digital signatures: implementation plan

Recorded 2026-10-16 on `prototype/pdf-forms`. Planning only: no certificate-based
signing, signature verification or signed-document protection is implemented by
this plan. Start with detection and validation before adding signing.

## Current behavior and standards

Proview's reusable signatures are visual artwork. They do not establish a
certificate-backed identity or cryptographic document integrity. PDF signature
fields are not activated by the form-filling prototype.

Existing PDF export paths rebuild documents; native form and text-edit writes
also perform full rewrites. They are not designed to preserve existing digital
signatures. Keeping a signature's visible appearance does not preserve its
cryptographic validity.

- ISO 32000 defines PDF signature structures, signed byte ranges, incremental
  revisions and certification permissions.
- PAdES (ETSI EN 319 142-1) profiles signatures embedded in PDFs. Baseline levels:
  B-B is the base signature; B-T adds a trusted signature timestamp; B-LT embeds
  validation material; B-LTA adds archive timestamps for long-term validation.
  Long-term validation also needs appropriate renewal and validation policies.
- CAdES can wrap a PDF in a `.p7m` container. Opening or signing such containers
  is a separate feature; PAdES keeps the document as a PDF.
- PAdES conformance does not by itself make a signature qualified under eIDAS.
  Qualified status depends on the certificate, signing device/service and the
  applicable requirements. Do not label a local or self-signed certificate as
  a qualified signature merely because its cryptographic checks succeed.

## Proposed sequence

### 1. Detect existing signatures and protect the original

- [ ] Inspect signature dictionaries and distinguish empty fields, signed fields
      and document timestamps, including signatures with no visible widget.
- [ ] Show that signatures are present without claiming that they are valid.
- [ ] Identify certification (`DocMDP`) and field-locking (`FieldMDP`) permissions.
- [ ] Before edits or exports that cannot preserve signatures, explain the effect
      and offer an explicit edited-copy path that preserves the original file.
      Define behavior for Save/Save As and every PDF transformation/export path.
- [ ] Keep malformed or unsupported signatures distinguishable from unsigned PDFs.

Acceptance: signed, unsigned, invisible-signature and empty-field fixtures are
classified correctly; unsupported verification never produces a valid badge;
the original signed file is preserved when producing an edited copy.

### 2. Read-only verification panel

- [ ] List each signer, certificate issuer, signed revision, signing time and
      trusted timestamp when available. Distinguish claimed time from trusted time.
- [ ] Verify signed byte ranges, digest/signature, certificate chain, trust anchors,
      revocation and timestamps using an explicit validation policy.
- [ ] Report integrity, trust and qualification separately, with overall states
      such as valid, invalid and indeterminate/incomplete, plus useful reasons.
- [ ] Detect later revisions and evaluate permitted changes. A valid signature
      over an earlier revision does not prove approval of all current content.
- [ ] Choose trust-store/list updates, OCSP/CRL retrieval, offline behavior,
      caching, timeouts and cancellation before implementing online validation.
      Keep parsing/network work off the UI thread and reject stale results.

Acceptance: fixtures cover tampering, untrusted/revoked certificates, incomplete
revocation data, multiple signatures, timestamps and later permitted/forbidden
changes. Cross-check results with an independent mature validator.

### 3. Local-certificate PAdES signing

- [ ] Choose and evaluate the signing/validation engine, licensing, supported
      algorithms and native-platform dependencies before adding a dependency.
- [ ] Support `.p12`/`.pfx` certificate/key selection with password entry and
      optional visible appearance, separate from signature artwork management.
- [ ] Start with PAdES B-B; make B-T available when a timestamp authority is
      configured. Confirm the interoperability target with real PDF viewers.
- [ ] Finish pending edits before signing; write atomically and verify the output.
- [ ] Use incremental signing when adding a signature to an already signed PDF;
      preserve earlier signed bytes and obey certification/field permissions.
- [ ] Keep private keys/passwords out of logs and signature-library cloud sync.

Acceptance: independent verification succeeds, canceled/failed signing leaves
files unchanged, multiple signatures remain verifiable and qualified status is
never inferred from the file format alone.

### 4. Further integrations and long-term validation

- [ ] Hardware tokens/smart cards (for example PKCS#11 or platform providers),
      with platform-specific installation and PIN workflows evaluated separately.
- [ ] Remote signing via explicitly selected providers and user authorization.
- [ ] B-LT/B-LTA augmentation, validation-material embedding, document timestamps
      and renewal policy. Do not equate a timestamp with an archival service.
- [ ] Decide separately whether CAdES `.p7m` viewing/verification is useful.

## Entry points to assess

- `internal/pdfengine`: existing MuPDF bridge, native signature APIs and ABI;
  inspect capabilities rather than assuming they satisfy PAdES/eIDAS validation.
- `pkg/document`: immutable sources, PDF composition, save/export and history;
  current rewriting paths need explicit signed-document behavior.
- `pkg/gui`: document status, signature details, save/copy flow and asynchronous
  work. Keep certificate-based actions distinct from the artwork Signature tool.
- [PDF forms](pdf-forms.md): signature fields remain unsupported by filling.
- [Native text editing](pdf-text-editing.md): source replacement is not a
  signature-preserving edit path.

## References

- [ETSI EN 319 142-1 V1.2.1, PAdES baseline signatures](https://www.etsi.org/deliver/etsi_en/319100_319199/31914201/01.02.01_60/en_31914201v010201p.pdf).
- [Adobe: Digital Signatures in a PDF](https://www.adobe.com/devnet-docs/acrobatetk/tools/DigSig/Acrobat_DigitalSignatures_in_PDF.pdf).
- [AgID: CAdES/PAdES, multiple signatures and text fields](https://www.agid.gov.it/it/agenzia/stampa-e-comunicazione/notizie/2014/04/07/pubblicato-il-documento-firme-multiple-campi-testo).
- [European Commission: eSignature](https://eidas.ec.europa.eu/).
