# ADR 0011: No DOCX export; the shipped career documents stay PDF-only (JSK-T171, Q-ATS-5)

* Status: accepted (2026-11-24, operator decision: WON'T-DO for DOCX)

## Decision

* JSK-T171 (DOCX and plain-text export from the resolved model, gap G-09 of
  `docs/research/protocols/11-ats-gap-analysis.md`) is closed as **WON'T-DO**. No DOCX renderer is built, in the
  umbrella `render/` package or anywhere else, and no DOCX is shipped or uploaded.
* Question Q-ATS-5 ("DOCX export: worth an XL task, or PDF-only until a provider demands DOCX") is answered:
  **PDF-only**. The answer is conditional only in the sense of the Consequences below.
* The plain-text export is **not** part of this decision. It stays under JSK-T243 (the TXT `DocumentRenderer`
  skeleton in `internal/ext/render/txt`, with its round-trip equality gate). T243 states that T171 would own wiring a
  real resolved-model source; that wiring is no longer owed by a T171 task, and whether and when the TXT renderer is
  connected to a real resolved model is decided under T243 and its own roadmap item, not here.

## Why

* The cost is an XL engine (a third renderer next to LuaLaTeX and WeasyPrint, plus a DOCX-to-PDF token round-trip
  gate), listed as XL in the remediation backlog (`docs/research/protocols/13-remediation-backlog.md`, row T171).
* The gates that protect the PDFs the product ships (extractability, word boundaries, RTL order, hyphenation,
  headings, OCR) already measure the artifact an ATS reads. A DOCX would be a second artifact needing every one of
  them again.
* No provider the product supports has been recorded as demanding DOCX.

## Consequences

* Reversing this needs a new ADR that names the provider (or other concrete demand) requiring DOCX, which restores
  the T171 scope and its XL estimate.
* Gap G-09 is closed by decision, not by implementation; the ATS gap table must not be read as "DOCX works".
