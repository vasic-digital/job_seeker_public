# 0005 WebAuthn library selection

| | |
|---|---|
| Status | ACCEPTED |
| Task | JSK-T026 (FR-044, R-06) |
| Date | 2026-11-24 |
| Evidence | `evidence/JSK-T026/probe.txt` (read-only `gh api` capture, same day) |

**Decision: `github.com/go-webauthn/webauthn`, pinned at v0.18.2 (2026-09-19).**

Measured, not assumed:

- Maintenance: not archived, last push 2026-11-20, twelve releases in the last eight months (v0.16.2 on 2026-04-02 through v0.18.2 on 2026-09-19), 1368 stars, BSD-3-Clause.
- Vulnerability record: the GitHub global advisory database returned **0** advisories for the Go module `github.com/go-webauthn/webauthn` (query in the evidence file). Honest limit: this is the GitHub advisory feed only; `govulncheck` is not installed on this host, so the Go vulnerability database was not consulted. Re-check with `govulncheck` when available (tracked as a follow-up in JSK-T105 security work).
- Lineage: it is the maintained successor of `duo-labs/webauthn`, which is archived (last push 2022-12-05); the archived one is therefore rejected.
- Build: UNCONFIRMED here (no build capture in evidence/JSK-T026/probe.txt); the module is added and build-checked at T027 under toolchain 1.26.8 with GOTOOLCHAIN=local (since raised to 1.26.11, see evidence/JSK-GOTC/). It is pure Go, so CGO_ENABLED=0 is expected to hold.
- Pre-1.0: API may change between minors; the version will be pinned in go.mod at T027 and the auth package is the only importer.

Rejected: hand-rolling CBOR/COSE/attestation parsing (security-critical code with a maintained alternative; section 11.4.74).
