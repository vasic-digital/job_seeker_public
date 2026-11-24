# 25 - Conformance and test strategy

| | |
|---|---|
| Part | C of the protocols research |
| Date | 2026-11-24 |
| Principle | A plugin that cannot be shown to fail on a defect is not tested (anti-bluff, constitution section 11.4). Every plugin ships fixtures, a conformance run and a mutation pair; the gates run **locally** (no CI/CD, section 11.4.156) |
| Evidence | The sketch proves the mechanism: [appendix-sketch-output.txt](appendix-sketch-output.txt) (`TestMutantIsCaught` reports the mutant produced 1 conformance failure; `TestSubstringMutantWouldFail` pins the Django/Go defect) |

## Contents

1. [Test pyramid per plugin](#1-test-pyramid-per-plugin)
2. [Golden corpora](#2-golden-corpora)
3. [Real-parser measurements](#3-real-parser-measurements)
4. [Mutation pairing (`--prove-failure`)](#4-mutation-pairing)
5. [Kernel conformance suites](#5-kernel-conformance-suites)
6. [Gates register (`gates/`)](#6-gates-register)
7. [Evidence layout](#7-evidence-layout)
8. [What is not testable offline](#8-what-is-not-testable-offline)

## 1. Test pyramid per plugin

| Level | Count guide | What | Runs |
|---|---|---|---|
| L0 compile/vet | all | `go vet ./...`, `gofmt -l` (`gates/fmt_check.sh`) | every change |
| L1 unit | many | pure functions: tokeniser, mapper, parser | `go test ./...` |
| L2 conformance (kernel suite) | 1 per plugin | determinism, golden table, reason present on non-Yes, reason codes in the vocabulary, no private value in `Refs`, `Info` complete | `go test ./...` |
| L3 mutation pair | 1+ per plugin | the same suite on a weakened copy must **fail** | `go test ./...` + gate |
| L4 integration | few | registry + settings + policy + real SQLite (`internal/store`) + egress client against a local TLS test server | `go test ./...` |
| L5 recorded-sample | per source/reader | adapter against the captured real response (`testdata/*_sample.json`, `PROVENANCE.md`) | `go test ./...` |
| L6 live | 1 per source | one real call, complete `CallProof`; skipped with reason when the network is absent (`internal/providers/live_test.go` pattern) | on demand |
| L7 end-to-end | few | console flow with synthetic persona: ingest -> score -> draft -> approve (dry-run) -> handoff | `gates/verify_*.sh` and the existing console tests |

Per-plugin minimum: L1-L3 and L5 where input comes from a vendor. Section 11.4.289 applies: stubs prove only the plugin's own logic; a vendor claim needs a recorded real sample or a live call.

## 2. Golden corpora

| Corpus | Contents | Location | Provenance rule |
|---|---|---|---|
| Posting samples per source | one real response per source (single read) | `internal/providers/testdata/` (exists: `remotive_sample.json` etc.), new `internal/ext/source/<id>/testdata/` | `PROVENANCE.md` states date, URL, how captured |
| JSON-LD / HR Open documents | good, bad, ambiguous, wrapper shapes | `internal/ext/reader/<id>/testdata/` | real captures preferred; synthetic marked as such |
| Eligibility truth tables | rows `(posting fields, profile accessors) -> verdict, code` | table in the plugin test, mirrored in `evidence/JSK-Txxx/` | includes the Unknown row for every input |
| Classifier gold | exists: `internal/match/gold`, private corpus `JS_GOLD_CORPUS`, gate `gates/verify_gold.sh` | existing | false-positive rows (Django/Go) added |
| Document set | the umbrella's built PDFs (`manifest.json`), 300 files, golden-bad PDFs (GoToR, tag-less, label mismatch) | umbrella cache + tiny synthetic PDFs under `testdata/` | no personal data: synthetic persona only (`gates/check_no_personal_data.sh`) |
| ATS profile cases | one good, one over-limit, one wrong type per profile | `rules/ats/testdata/<id>/` | |

Determinism: corpora are read-only; a test never rewrites a fixture; a changed fixture changes its recorded hash in `evidence/` (same as `evidence/JSK-T057/manifest.tsv`).

## 3. Real-parser measurements

Plugins that claim "an ATS can read this" must measure with real extractors, not assertions about the file:

| Claim | Instrument | Pass criterion | Not measurable offline |
|---|---|---|---|
| link annotations are `/URI` with an allowed scheme | `pdfinfo`/`pdftotext` + a minimal annotation scan | 0 GoToR/Launch; label equals target | - |
| text order and word boundaries | `pdftotext` (poppler) and `mutool` if present; compare token ratios across extractors (T168) | min/max >= 0.9 for non-CJK | proprietary ATS parsers (Workday, Taleo): UNCONFIRMED by construction, recorded in the claim ledger as "not measurable" |
| tagged PDF / XMP | catalog dump | `/MarkInfo`, `/StructTreeRoot`, XMP `dc:language` present | PDF/UA conformance needs a validator that is not installed (Part B Q-ATS-6) |
| OCR consistency | `tesseract` at 300 dpi | recall >= 0.95 for en/de/ru/sr (T179) | language models other than installed |
| keyword matching | the Go matcher over the gold table | 0 false positives on the table | recruiter-side semantic ranking |

Each measurement records tool name and version in the evidence file (`tool --version` output) so a different host does not silently change the reading. A missing tool is exit 2 (cannot determine), never a pass (`gates/verify_gold.sh` convention).

## 4. Mutation pairing

Every plugin package has `mutant_test.go`:

1. `type mutantX struct{ Real }` overriding exactly the line that matters (Unknown -> Yes; `>=` -> `>`; skip the scheme check; accept `direct_send`).
2. Run the **same** fixture table and kernel suite through a `Reporter` that counts failures (`ext.Reporter`, sketch `ext/conformance.go`).
3. Assert the count is > 0; log it (the pasted line in the appendix is the proof the pairing works).

Gate-level pairing follows the existing convention: a gate script advertising `--prove-failure` has the proof executed by `gates/run_all.sh:14-17`; the proof builds a weakened throwaway copy of the artifact (a registry with a gate plugin removed, a policy file with a locked gate switched off by a provider layer, a channel declaring `direct_send`) and requires rc 1, a tampered input requires rc 2, the control requires rc 0.

## 5. Kernel conformance suites

One function per port, in `internal/ext/conformance.go` (the sketch holds `ConformEligibility`):

| Port | Suite asserts |
|---|---|
| `StandardReader` | golden-good parses; every other reader's golden-good is `ok=false`; broken input is an error not `ok=false`; absent fields stay empty; no mutation of input bytes |
| `SourceAdapter` | against a local TLS server (egress client with test allow-list): one request per `Fetch`; no `Authorization` header unless a key is configured; malformed record -> `Skipped` by class; malformed envelope -> `Error{Class: schema}`; `Limits` equal the documented values; `LegalNotice` re-read from `Raw` |
| `EligibilityRule` | determinism, three-valued golden table, reason on non-Yes, closed codes, no private value, **purity** (two calls equal; clock only through `now`) |
| `ScoreFeature` | value in `[0,1]`, deterministic, monotone where documented, reasons optional |
| `ApplicationChannel` | zero `Approved` rejected; non-https rejected; `Info.Caps["direct_send"]==false`; `Prepare` performs no network I/O (empty allow-list client untouched); output contains only ids/URLs |
| `ComplianceGate` | missing settings -> `Unknown`; each `Stage` exercised |
| `ATSProfile` | good/over-limit/wrong-type cases; severities; `Applies` exact-host (no suffix match) |
| `FormatProfile`/`Renderer` | round-trip text equality for derived formats (T171); filename template output |
| `DeliverabilityCheck` | DNS fixtures through a fake `Fetcher`; missing record -> `Unknown`/`No` as specified |
| `TrackerHook` | error does not propagate to the status write |
| `EvidenceSink` | non-blocking under a full buffer (timing bound), counts drops |

## 6. Gates register

New scripts in `gates/`, each three-valued (0 pass, 1 finding, 2 cannot determine) with `--prove-failure`, each picked up by `gates/run_all.sh` automatically (`gates/verify_*.sh` glob):

| Gate | Asserts | Paired proof |
|---|---|---|
| `verify_plugins.sh` | every plugin registered in `cmd/*/main.go` has a package with `testdata/`, `conformance_test.go`, `mutant_test.go`; every reason code used exists in `reasons.tsv`; ids are unique and lower_snake; `Info.Version` matches the version pinned in the plugin's fixtures manifest | remove a plugin's `mutant_test.go` in a throwaway copy -> rc 1; unreadable tree -> rc 2 |
| `verify_policy.sh` | `rules/policy.yaml` loads; unknown keys rejected; scope matrix (provider x ats x country) leaves every locked gate on; every gate port has >= 1 active plugin in the default scope | allow a provider layer to switch off a locked gate -> rc 1 |
| `verify_channels_nosend.sh` (or an extension of `verify_nosend.sh`) | enumerates `Registry[ApplicationChannel].IDs()` and callers of `NewApproved` (`go list`/grep over the tree); only `draft.Service.Approve` may call it; every channel rejects a zero `Approved` | add a second caller in a throwaway copy -> rc 1 |
| `verify_migrations_ext.sh` | applies `0003`/`0004` on a copy of a `0002` DB with backup; schema test; forged-reason refusal | drop the backup step -> `ErrBackupFailed` path -> rc 1 |
| `verify_parser_measure.sh` | the real-parser measurements of section 3 over the golden-bad/good PDFs; tool versions printed | missing `pdftotext` -> rc 2 |
| `verify_ledger.sh` extension (exists) | the advertised capability list (`jsctl ext list`) equals what conformance proved (section 11.4.266) | a plugin listed without a conformance run -> rc 1 |

All of them are **local**; nothing here is a GitHub Actions or GitLab pipeline.

## 7. Evidence layout

`evidence/JSK-Txxx/` per task (as today): `red.txt` (the failing run on the broken artifact), `green.txt`, `mutation.txt` (mutant failure count), `tools.txt` (tool versions), `corpus.tsv` (fixture hashes). A plugin claim in a spec table cites its directory.

## 8. What is not testable offline

| Item | Status |
|---|---|
| Proprietary ATS parser behaviour (Workday, Taleo, iCIMS) | UNCONFIRMED: only simulated; stated in every `ATSProfile` as `simulated: true` |
| Live vendor rate limits not documented | measured only by polite single reads; never load-tested |
| Mailbox providers' spam placement | not measurable; `DeliverabilityCheck` verifies DNS records only |
| Real submission to an employer | never exercised (FR-027); the dry-run `would_send` payload is the tested artifact |
