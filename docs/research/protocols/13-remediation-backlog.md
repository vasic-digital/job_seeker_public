# 13 - Remediation backlog

| | |
|---|---|
| Part | B of the ATS protocol research |
| Date | 2026-11-24 |
| Numbering | (APPLIED: registered as Phase 12A of tasks.md with FR-102..FR-108.) At proposal time `specs/001-job-seeker-mvp/tasks.md` ended at **JSK-T159** (verified: `grep -o 'JSK-T[0-9]*'` over `specs/` and `docs/`, last id T159). This backlog continues at **JSK-T160**. It is a PROPOSAL: nothing here has been appended to `tasks.md`; part C or the operator decides where and when. Parallel agents writing other docs may propose overlapping ids; reconcile at merge. |
| Roadmap epic | JSK-005 (Applications and tracker) for job_seeker work; tasks marked `[UMBRELLA]` change the career engine, which is a separate repository and needs its own PR and review there |
| Rules applied | test-first with a recorded RED on the broken artifact, evidence directory per task (`evidence/JSK-Txxx/`), no personal data in fixtures (synthetic persona), no generated prose (FR-029), fail closed, every refusal audited |
| Effort | S <= 0.5 day, M ~ 1-2 days, L ~ 3-5 days, XL > 1 week (single engineer, includes tests and evidence) |

## Contents

1. [Order and dependencies](#1-order-and-dependencies)
2. [Tasks in tasks.md style](#2-tasks-in-tasksmd-style)
3. [Task detail: test-first, evidence, files, effort](#3-task-detail)
4. [Definition of done for the whole programme](#4-definition-of-done)
5. [Open operator decisions](#5-open-operator-decisions)

## 1. Order and dependencies

```
Wave 1 (P0, no dependencies; start together)
  T162 link annotations [UMBRELLA]   T163 placeholder gate [UMBRELLA]   T166 exact matcher [UMBRELLA]
  T161 send-time date                T182 re-check hard gates at approve  T186 pin posting hash
  T181 company-level duplicate       T172 manifest freshness
Wave 2 (P0 core, depends on Wave 1)
  T160 cover slots (needs T161, T163)  T164 posting terms (needs T166)  T165 coverage + honesty (needs T164)
  T167 language detector (independent but P0)
Wave 3 (P1)
  T168 T169 extractor variance [UMBRELLA]   T170 tagged PDF/XMP [UMBRELLA]   T174 headings
  T177 knockout checklist   T185 scam flags   T187 approval report (needs T165)
Wave 4 (P2 / decisions)
  T171 DOCX+TXT   T173 page budget   T175 filenames   T176 provider profiles   T178 header/footer   T179 OCR
  T180 Hebrew   T183 hours   T184 mailto
```

## 2. Tasks in tasks.md style

```
- [ ] JSK-T160 [US3] Cover-letter slots: deterministic substitution of company, role and send date into a fixed per-language template; no generated prose (FR-029; JSK-005; gap G-01, F-05)
- [ ] JSK-T161 [US3] Send-time date: the draft hash covers the date; a cover whose date differs from the day of live approval is refused (G-01, F-07)
- [ ] JSK-T162 [P] [UMBRELLA] [US3] Link-annotation gate and renderer fix: every /Link is /URI with mailto: or https: and equals the visible label; fix `render/latex.py:137` so 260 LuaTeX PDFs stop emitting GoToR (G-02, F-08)
- [ ] JSK-T163 [P] [UMBRELLA] [US3] Placeholder-leakage gate over the extracted text, per language, with a reserved-token registry (F-06)
- [ ] JSK-T164 [US3] Posting term extractor: tech-term dictionary, synonym table (Golang/Go, k8s/Kubernetes, AWS/Amazon Web Services), token-exact matching (G-03, F-22)
- [ ] JSK-T165 [US3] Per-application coverage report with an honesty allow-list (only profile-held terms may be emphasized); area-overlap runner-up margin (G-03, F-02, F-23)
- [ ] JSK-T166 [P] [UMBRELLA] [US3] Replace squash-substring `_has_keyword` with token-aware matching; golden-bad: Django must not match Go (G-04)
- [ ] JSK-T167 [US3] Language detector for all 15 template languages; confirmation on every fallback_en with non-English body; unify minLangConf with LangMinConfidence; Serbian Latin vs Cyrillic decision (G-05, G-13, F-01)
- [ ] JSK-T168 [P] [UMBRELLA] [US3] Word-boundary variance gate (hi, ko in MuPDF) and renderer remedy (G-06, F-25)
- [ ] JSK-T169 [P] [UMBRELLA] [US3] RTL logical-order gate across extractors for ar and fa; renderer remedy (G-07, G-21, F-25)
- [ ] JSK-T170 [UMBRELLA] [US3] Tagged PDF + XMP (+ PDF/UA attempt) for both engines, gated by /MarkInfo, /StructTreeRoot, /Metadata (G-08, G-22, F-26)
- [ ] JSK-T171 [US3] DOCX and plain-text export from the same resolved model, gated by round-trip text equality (G-09)
- [ ] JSK-T172 [US3] Manifest freshness: build epoch + overlay hash recorded; Resolve refuses stale or foreign sets; gate-verdict stamp per file (G-10, G-24, F-03)
- [ ] JSK-T173 [P] [UMBRELLA] [US3] CV page budget headroom (target <= 2 pages for 3-page content classes) - operator decision first (G-11)
- [ ] JSK-T174 [P] [UMBRELLA] [US3] Heading-recognition test across 15 locales with an alias table (G-12, F-24)
- [ ] JSK-T175 [US3] Download filename policy `<Candidate>_<Kind>_<Role>_<yyyymmdd>.pdf` (G-15, F-21)
- [ ] JSK-T176 [US3] Provider upload profiles (types, size, count, character limits) + pre-handoff check (G-16, F-10)
- [ ] JSK-T177 [US3] Knockout-question pre-flight checklist; confirmed answers kept in the private overlay; approval blocked until acknowledged (G-17, F-11)
- [ ] JSK-T178 [P] [UMBRELLA] [US3] Running header/footer and compound-word hyphenation extraction hygiene (G-18, G-19)
- [ ] JSK-T179 [P] [UMBRELLA] [US3] OCR consistency check; resolve the ru/sr 67-77% recall (UNCONFIRMED cause) (G-20)
- [ ] JSK-T180 [UMBRELLA] [US3] Hebrew locale decision and, if accepted, RTL build via the WeasyPrint engine (G-23)
- [ ] JSK-T181 [US3] Company-level duplicate and re-application cool-down guard with audited override (F-04, F-28)
- [ ] JSK-T182 [US3] Re-evaluate hard scoring gates (blocked company, salary floor, tz, location) at live approval and at handoff-complete (F-12, F-13, F-14)
- [ ] JSK-T183 [P] [US3] Local-business-hours warning at approval (F-15)
- [ ] JSK-T184 [P] [US3] `mailto:` apply URLs flagged manual-only until a mail path exists (F-16)
- [ ] JSK-T185 [US3] Scam/ghost-posting risk flags recorded on the posting and shown at approval (F-17)
- [ ] JSK-T186 [US3] Pin the posting raw hash in the approval; refuse when the posting changed or closed (F-18)
- [ ] JSK-T187 [REVIEW] [US3] Approval screen shows link table, extracted-text diff vs last send, date, company and coverage report (F-27)
```
(`[US3]` is a placeholder for the draft/apply user story; use the id `tasks.md` assigns to it.)

## 3. Task detail

| ID | Pri | Wave | Test-first (RED before code) | Evidence required | Files to touch | Effort |
|---|---|---|---|---|---|---|
| T162 | P0 | 1 | Add golden-bad PDF with a `GoToR` link and a scheme-less URI; gate must return 1. Run the new gate over the 300 current files: expect **260 FAIL** (RED on the broken artifact), 40 pass. After the renderer fix, rebuild and expect 300 pass | pasted `pypdf` annotation dump before/after; gate output; build log | umbrella `_tools/career/gates.py` (new `_check_link_annotations`), `render/latex.py:128-137` (emit `\href{https://...}` / `mailto:` target, keep visible label), `test_pdf_pipeline.py`, `latex/miloscv.cls` if `hyperref` options change | M |
| T163 | P0 | 1 | fixtures containing `[Company]`, `{{role}}`, `TODO`, `XXX`, `<date>` per language; RED today (no check); mutation: remove check -> RED | gate output per fixture; registry file listing reserved tokens | umbrella `gates.py` (`_check_placeholders`), `test_pdf_pipeline.py` | S |
| T166 | P0 | 1 | table: (`We use Django`,`Go`)->no, (`JavaScript`,`Java`)->no, (`Python3`,`Python`)->no/yes by decided rule, (`C++ and C#`,`C`)->no, (`k8s`,`Kubernetes`)->yes via synonym; RED today (4 wrong answers measured) | table output | umbrella `screen.py:92-113`, new tests | S |
| T161 | P0 | 1 | two drafts on different mock-clock days -> different `draft_hash` and cover date; a cover dated yesterday refused at live `Approve` (RED: today all covers show the build date) | go test output; extracted-date samples (counts, no names) | `internal/draft/service.go` (`assemble`, `DraftHash` `:191`), `internal/templates` (date slot), render path via T160 | M |
| T182 | P0 | 1 | fixture: shortlist with passing score, then change overlay floor/blocked company; `Approve(live)` must return `ErrGateRegressed` (RED: currently approves); mutation per gate | test output for 4 gates, audit rows | `internal/draft/service.go:510-529` (`checkLive`), `internal/match/score.go` (export pure `Evaluate`), `errors.go` | M |
| T186 | P0 | 1 | change posting body/closed flag after `Prepare`; approval must be superseded (RED: hash does not cover posting) | test output, audit rows | `service.go:191` (`DraftHash` input), `application` migration (`approved_posting_hash`), `HandoffComplete` | M |
| T181 | P0 | 1 | two postings same company other provider -> `CreateApplication` refused; override with reason audited; cool-down after `rejected` | test output | `internal/draft/service.go:131`, new `internal/draft/company.go`, migration `company_norm` index | M |
| T172 | P0 | 1 | manifest with epoch > N days or overlay hash mismatch -> `ErrTemplatesStale` at `Resolve` (RED: no such check); unknown schema still rejected | test output | `internal/templates/templates.go:57-137`, umbrella `buildlib.py:178-192` (manifest fields: epoch exists; add `overlay_sha`, `gate_stamp`) | M |
| T160 | P0 | 2 | company A fixture -> extracted cover contains A exactly once and no other company; swap -> RED; PDF gate green; hash changes with company | extracted-text counts (company present: yes/no only), gate output | umbrella `render/*`, locale bundles, `internal/templates` (slot API), `internal/draft`; FR-029 stays true: fixed template, typed slots | L |
| T164 | P0 | 2 | synthetic 20-term posting: extractor returns the 20 terms; synonyms normalized; false-positive table from T166 | extractor output | new `internal/match/terms.go` (+ dictionary file, reuse the dictionary named in `docs/plans/job-seeker-research/04-matching-and-area-mapping.md:71`) | M |
| T165 | P0 | 2 | report on the measured sample must say 12/20 matched, list 8 missing, mark 5 as unclaimable; an attempt to add `Terraform` to emphasis fails the honesty allow-list | report JSON with counts | new `internal/draft/coverage.go`, approval payload, `docs/` | L |
| T167 | P0 | 2 | 15 synthetic postings (one per language, 80+ words) -> expected code with confidence >= 0.8; French posting no longer silent `fallback_en`; Serbian Latin vs Cyrillic cases; assert `minLangConf == match.LangMinConfidence` | go test table | `internal/match/lang.go:27-96` (stop words, script tables), `internal/draft/language.go:22,177-206` | M |
| T168 | P1 | 3 | gate: token-count ratio (min extractor / max extractor) >= 0.9 for non-CJK; RED today for `hi` (0.55) and `ko` (0.57) | per-language token table (the one in `11`, section G-06) | umbrella `gates.py`, `render/latex.py` / `miloscv.cls` (emit explicit spaces: UNCONFIRMED root cause, investigate `\pdfinterwordspaceon` or equivalent first) | L |
| T169 | P1 | 3 | for ar and fa: phrases from the HTML source must appear forward and never reversed in mutool and pypdf and not reversed in the primary extractor; RED today on `pdftotext -raw` (16/59 reversed) | counts table | umbrella `gates.py:140-152`, `render/html.py` (bdi/dir handling, logical text order) | L |
| T170 | P1 | 3 | gate asserts `/MarkInfo`, `/StructTreeRoot`, XMP with `dc:language` and `dc:title`; RED: 0/300 | `pypdf` catalog dump | umbrella `render/latex.py` (tagpdf), `render/html.py` (WeasyPrint `pdf_variant`) | XL (tagpdf under LuaLaTeX on 13 locales is the risk; spike first) |
| T174 | P1 | 3 | alias table per locale; each of 15 CVs yields Education/Experience/Skills headings | table | umbrella `gates.py` or `screen.py`, locale bundles | M |
| T177 | P1 | 3 | posting with "authorized to work in <country>" -> checklist row; approval blocked until ack | UI/API test output | `internal/draft`, `internal/console`, overlay schema (private values stay private) | L |
| T185 | P1 | 3 | 10 synthetic scam bodies flagged, 10 clean not; hard flags block live approval | confusion table | new `internal/ingest/risk.go`, approval view | M |
| T187 | P1 | 3 | approve button disabled until report rendered (needs T162, T165) | e2e screenshot (synthetic persona vault) | `internal/console`, `internal/draft` | M |
| T171 | P2 | 4 | DOCX and TXT from the same model; extract text from DOCX and compare to PDF text token-for-token | round-trip diff counts | umbrella `render/` (new engine), `internal/templates/templates.go:34` slot regex | XL |
| T173 | P2 | 4 | decision, then a gate that fails when a CV exceeds the agreed page budget | page table | umbrella `gates.py:81`, content | M |
| T175 | P2 | 4 | `Content-Disposition` test for the three kinds | test output | `internal/templates/templates.go:197` | S |
| T176 | P2 | 4 | table of provider profiles (types, bytes, count); oversize refused | test output | new `internal/draft/uploadprofile.go`, `docs/providers/` | M |
| T178 | P2 | 4 | extract: running footer not duplicated in body order; hyphen-at-EOL count budget | counts | umbrella `miloscv.cls` | M |
| T179 | P2 | 4 | OCR at 300 dpi for en/de/ru/sr; recall >= 0.95 or documented cause; models for other languages are an operator install | recall table | umbrella new script, `docs/` | M |
| T180 | P2 | 4 | decision first | - | umbrella locales | M |
| T183 | P2 | 4 | tz_hint window warning recorded in the audit row | test output | `internal/draft/service.go` | S |
| T184 | P2 | 4 | `mailto:` apply URL -> `manual_only` flag | test output | `internal/draft`, `internal/ingest` | S |

### Cross-repository note

Tasks marked `[UMBRELLA]` modify files under `vasic/_tools/career/`. They need a branch and PR in the umbrella repository with its own independent review and its own evidence; the job_seeker side consumes the result only through `manifest.json` (T172 adds the fields). Do **not** edit generated PDFs in `.cache-career/`; rebuild them with the engine.

## 4. Definition of done

1. `python3 -I` gate run over the full rebuilt set returns `{0: 300}` with the **new** checks included (links, placeholders, extractor variance, structure), pasted.
2. The `Django`/`Go` golden-bad passes as "not matched"; the synthetic posting report reproduces 12/20 with 8 listed missing.
3. A live approval dry-run for an application with a stale date, a changed posting, a regressed salary gate, a duplicate company and a non-confirmed language is **refused five times**, each with an audit row.
4. 15-language detector table green; no silent `fallback_en` on a non-English body.
5. Independent review per PR; evidence directories `evidence/JSK-T1xx/`.

## 5. Open operator decisions

| # | Question | Needed by |
|---|---|---|
| Q-ATS-1 | Cover personalization: is deterministic substitution of company, role and date acceptable under D8/FR-029 ("templates only")? (Recommended: yes; no prose is generated.) | T160 |
| Q-ATS-2 | Serbian: keep Cyrillic only, or also build Latin-script `sr` templates? | T167 |
| Q-ATS-3 | Bilingual headings (`Education / Ausbildung`) vs purely localized headings | T174 |
| Q-ATS-4 | CV length target: stay at 3 pages or compress to 2 | T173 |
| Q-ATS-5 | DOCX export: worth an XL task, or PDF-only until a provider demands DOCX | T171 |
| Q-ATS-6 | Install tesseract language models and a PDF/UA validator on the host (package installs are operator actions) | T170, T179 |
| Q-ATS-7 | Hebrew locale: add or drop from the directive's RTL list | T180 |
