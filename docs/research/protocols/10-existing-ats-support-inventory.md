# 10 - Inventory of existing ATS-compliance support

| | |
|---|---|
| Part | B of the ATS protocol research (A writes 00-09, C writes 20-*) |
| Date | 2026-11-24 |
| Roadmap item | JSK-005 (Applications and tracker); backlog in `13-remediation-backlog.md` |
| Scope | Everything that today makes an application artifact ATS-friendly or checks it, across the umbrella career engine and `job_seeker` |
| Method | Source read with file:line citations; tools RUN on this host against the generated set (read-only, structure and counts only). Nothing private is quoted: no overlay or vault values, no PDF body text. |
| Operator typo | The directive says `docs/researc/protocols`; treated as a typo for `docs/research/protocols` (stated in the PR). |
| Honest boundary | Sibling umbrella paths below (`_tools/career/...`, `.cache-career/`) live in the umbrella checkout, not in this repository; line numbers are from the umbrella tree as read 2026-11-24. |

## Contents

1. [Architecture in one paragraph](#1-architecture-in-one-paragraph)
2. [Umbrella career engine](#2-umbrella-career-engine-_toolscareer)
3. [The generated set (measured)](#3-the-generated-set-measured)
4. [job_seeker](#4-job_seeker)
5. [Tests and evidence that exist](#5-tests-and-evidence-that-exist)
6. [What does NOT exist](#6-what-does-not-exist-pointer-to-11)
7. [Tool availability on this host](#7-tool-availability-on-this-host)

## 1. Architecture in one paragraph

The umbrella career engine renders **300 template PDFs** (10 areas x 15 languages x {cv, cover}) from one resolved document model
and gates each one. `job_seeker` never renders anything: it resolves a template by slot (`kind-area-lang.pdf`) from the engine's
`manifest.json`, pins the SHA-256 of the pair in a `draft_hash`, and hands the operator over to the provider's own apply URL.
So ATS compliance is decided **entirely at render time in the umbrella**; `job_seeker` only guarantees *which* file is sent and that it was not altered.

## 2. Umbrella career engine (`_tools/career/`)

### 2.1 Renderers

| Engine | Locales | Where | Notes |
|---|---|---|---|
| LuaLaTeX + class `miloscv.cls` | 13 (all but ar, fa) incl. CJK and hi | `render/latex.py:378-436` (`compile_pdf`, two passes, `-halt-on-error`, `SOURCE_DATE_EPOCH` pinned, atomic write at `:430-434`) | missing glyph (.notdef) fails the build (`:373`, `:422-428`) |
| WeasyPrint (HTML) | ar, fa only | `render/html.py:203-217`, locale set `render/__init__.py:32` (`HTML_LOCALES`) | chosen because LuaTeX RTL extracts as presentation forms in visual order (comment `render/__init__.py:27-31`) |
| Engine dispatch | | `render/__init__.py` `ENGINE_FORMATS = {"latex": (tex, pdf), "html": (html, pdf)}`, `FORMATS = (tex, html, pdf)` | `docx`, `md`, `json` outputs were declared earlier, never produced, and removed; a test pins this (`test_pdf_pipeline.py:222 test_formats_only_claim_what_is_produced`) |

Typography guards in the renderers: URLs are unbreakable `\mbox{\href{..}{..}}` with identical label and target (`render/latex.py:128-137`);
CJK line-break helper (`:97`); per-locale font requirement check (`render/fonts.py:35-58`, `FontMissing`).
Metadata is written for every LuaTeX PDF: `pdftitle`, `pdfsubject`, `pdfkeywords`, `pdfauthor`, `pdflang` (`render/latex.py:172-177`, `latex/miloscv.cls:315-321`); hyperref is loaded as `[hidelinks,unicode]` (`latex/miloscv.cls:93`).

### 2.2 The ATS artifact gate (`gates.py`, 445 lines)

`validate_pdf` (`gates.py:370-414`) returns 0 valid / 1 invalid / 2 indeterminate. What it checks, by line:

| Check | Lines | What it actually proves |
|---|---|---|
| Text layer non-empty, no U+FFFD or bidi-control codepoints in the *independent* extraction | `_check_extractable` 84-92 | extraction produced text; no replacement chars |
| No "ATS-poison" codepoints: ligatures FB00-FB06, CJK compat ideographs, fullwidth digits/ASCII, PUA, ZWSP, soft hyphen, C0/C1 controls | `_NO_ARTIFACT_RANGES` 28-38, `_check_no_artifacts` 95-110 | the text layer has none of those code points (zh/ja exempt fullwidth punctuation only, 53-62) |
| Arabic presentation forms FB50-FDFF, FE70-FEFF absent | 51, 106-109 | RTL text layer holds base letters |
| Locale's own script present in volume (>= 200 chars) | `_SCRIPT_RANGES` 66-77, `_check_script_coverage` 128-137 | a build that fell back to Latin font is caught (hi, ar, fa, ja, zh, ko, ru, be, kk, sr) |
| Known source phrases contiguous and in logical order, in either extractor | `_check_phrases` 140-152 | name/headings survive; reversed RTL runs fail |
| Single column (central-gutter heuristic on `pdftotext -bbox`) | `_gutter_pages` 172-213, `_check_single_column_pdf` 216-226 | no two-column layout |
| Contact URLs intact as one string in the text layer | `_check_contact_urls` 229-234, `_expected_contacts` 237-248 | the *visible text* of each link survived; empty list is a failure |
| Page budget cv <= 3, cover <= 2 | `_PAGE_LIMITS` 81, `_check_page_count` 270-283 | pages <= limit |
| Fonts all embedded and subset, no Type 3 | `_check_fonts_embedded` 286-322 | `pdffonts` columns parsed by dashed header |
| Second extractor: `mutool draw -F txt`, else pypdf | `_extract_independent` 348-367 | authority for control codepoints |

`gates.py` deliberately does not re-implement HR criteria (docstring 8-11).

### 2.3 The HR screen (`screen.py`, 315 lines)

Operates on the `DocumentModel`, not the PDF (docstring 1-11). Findings are FAIL/WARN/INFO in six families: structure (`_check_structure` 124), entry quality (144), **ATS keywords** (`_check_ats_keywords` 195-200), red flags (203), summary/headline (234), locale (`_check_locale` 248-274: untranslated labels, English leak, pronouns), cover (`_check_cover` 277-301: 250-350 words, 4-6 paragraphs, area title present, >= 2 project names, duplicate paragraphs).
Keyword matching is `_has_keyword` (`screen.py:92-113`): squash both sides to `[0-9a-z]`, substring test with a plural tolerance.
**The keyword list is `doc["ats"]["keywords"]` and `doc["area"]["keywords"]` (`:196-200`), i.e. derived from the profile/area (`resolve.py:113-118,437-445`), never from a job posting.**

### 2.4 Build, manifest and locale layer

| Item | Where |
|---|---|
| Matrix 10 x 15 x {cv,cover}; stems `kind-area-lang` | `buildlib.py:129-151` |
| `manifest.json` (`career.manifest.v1`; per-file bytes and sha256; epoch) | `buildlib.py:170-192`, `career.py:254` (manifest only after gates pass) |
| Single build lock, bounded jobs by free memory | `buildlib.py:52-126` |
| Key phrases fed to the phrase gate (`name`, headings, headline) | `buildlib.py:194-217` |
| Profile validation: `ats.base` required; only U+FFFD allowed as `ats.forbidden` token | `model.py:361-370` |
| Document date is a *build parameter* (epoch), localized | `resolve.py:355-359, 455` |
| Locale completeness/guards | `test_locale_guards.py` (15 tests), `test_locale_comp_items.py` |
| Public-site expertise pages (not PDFs) | `expertise_pages.py` and `test_expertise_*.py` (out of scope here, by reference) |

### 2.5 Research documents

`docs/plans/job-seeker-research/01..05-*.md` (keyless job APIs, jobops, Go stack, matching/area mapping, console privacy). Only `04-matching-and-area-mapping.md` touches ATS indirectly (skills = `|req ∩ have|/|req|` over tech tokens extracted from the posting, line 71; this is **scoring**, not CV tailoring).
`docs/plans/job-seeker-inputs-summary.md:11-17` records the operator requirement "the CV MUST be optimised for ATS", per-position variants, and "export to PDF and other major formats (HTML, Markdown, Docx)".
No document under `docs/plans/` specifies ATS acceptance criteria beyond that.

## 3. The generated set (measured)

Run 2026-11-24 over `.cache-career/` (umbrella). Scripts and raw rows kept outside git; the outputs below are verbatim aggregates.

```
$ ls .cache-career/*.pdf | wc -l   -> 300      (150 cv + 150 cover; 10 areas x 15 langs)
$ du -sh .cache-career             -> 27M      (manifest declares 600 entries: 300 pdf + 260 tex + 40 html)
```

`pdfinfo`/`pdffonts` over all 300 (`python3 scan.py`):

```
pages        {1: 150 (all covers), 3: 150 (all CVs)}
Producer     {'LuaTeX-1.22.0': 260, 'WeasyPrint 67.0': 40}
PDF version  {'1.7': 300}     Tagged {'no': 300}     PDF/A id: 0    PDF/UA id: 0
Title/Author/Subject/Keywords present: 300/300/300/300      CreationDate present: 300
Type 3 fonts: 0     fonts all embedded+subset: 300 (CID TrueType / CID Type 0C)
size bytes   min 25299  median 40388  max 120820 (zh)   (ja max 100192, ko 70753)
media box    [0 0 595 842] (A4) 300/300       Encrypted: no 300/300
```

`pypdf` catalog inspection over all 300:

```
/Lang present: 300/300 (20 per language)   /MarkInfo: 0   /StructTreeRoot: 0   XMP /Metadata: 0
Link annotations: 5 per PDF (cv and cover), 1500 total
Outline (bookmarks): 40 (the WeasyPrint set) / 260 none
```

The existing gate, run unmodified on all 300:

```
$ python3 -I -c "... gates.validate_pdf(f, kind) for all 300 ..."   (36.7 s)
{0: 300}        # 300 pass, 0 fail, 0 indeterminate; no failure messages
```

So **the gate is green on all 300 files**. `11-ats-gap-analysis.md` shows what it does not look at.

## 4. job_seeker

| Component | Where | ATS-relevant behaviour |
|---|---|---|
| Slot resolver | `internal/templates/templates.go:34` regex `^(cv\|cover)-<area>-<lang>.pdf$`; `Open` 57-88 (schema `career.manifest.v1` only, `:29`); `Resolve` 118-137 | slot by name; **re-hashes the file on disk against the manifest at every resolve**, `ErrHashMismatch` otherwise; non-PDF manifest entries ignored |
| Streaming | `templates.go:163-205` | reads once, hashes THE SAME bytes, sends them; `Content-Disposition: inline; filename="<slot-name>"` (`:197`), `no-store`, `nosniff`, `X-Content-SHA256` |
| Draft assembly | `internal/draft/service.go:196-228` (`assemble`), `Prepare` 245-295 | area comes from the current mapping (`:131-156`); language from `DecideLanguage`; missing slot -> `slot_missing`, never generates text (`:264-275`) |
| Draft hash | `service.go:191` `sha256("draft.v1", area, lang, cv sha, cover sha)` | pinned at approval; re-verified in `Approve` (`:432-480`, `verifyDraft` 349) and in `HandoffComplete` (665) |
| Language rule C6 | `internal/draft/language.go:61-76`; explicit phrase `:47-57` (English phrasing only); `SupportedLangs` :9 | detected-and-supported -> that language; `de` and any "apply in X" need human confirmation (`Approve` refuses `confirm_*`, `service.go:447`); else `en` |
| Language detector | `internal/match/lang.go:41-96`, `LangMinConfidence = 0.8` (`:17`), `AcceptLanguage` 110 | returns only `en`, `de`, `sr`, `ru`, `und` (comment `:40`); stop-word lists for 4 languages (`:27-31`) |
| Draft-side confidence | `language.go:22` `minLangConf = 0.6` | differs from the 0.8 of `match.LangMinConfidence` and FR-021 |
| Live-send gates | `service.go:510-529` (`checkLive`), caps 531-560, kill switch `gate.go:38-63` | mode, overlay, kill switch, step-up, typed company name, caps. **No posting freshness, no blocked-company/salary/tz re-check at this point** (those are recorded as scoring gates in `internal/match/score.go:176-201`) |
| Idempotency | `schema 0001_init.sql:69` `application.posting_id UNIQUE`; `:95` submission `idem_key UNIQUE`; `writeSubmission` 562 | one application per posting; no company-level duplicate guard |
| Approval TTL | `service.go:26` 14 days; `ExpireStale` 629 | stale approvals expire |
| Ingest filters | `internal/ingest/ingest.go:427-445` | expired provider postings skipped |
| `pdgate` | `internal/pdgate/pdgate.go` | **not an ATS gate**: it is the no-personal-data gate (FR-054, FR-077). It scans tracked files for overlay values and forbids tracked PDFs/images outside an allow-list. It never opens a template PDF. Listed here so nobody mistakes it for format validation. |
| `internal/templates` tests | `templates_test.go` (4 tests) | slot resolution and hash mismatch |
| `internal/draft` tests | `service_test.go` (12), `live_test.go` (8), `language_test.go` (2) | state machine, hash change, language confirmation |

### Spec items that bind this area (`specs/001-job-seeker-mvp/spec.md`)

| Ref | Line | Text (abridged) |
|---|---|---|
| D8 | 28 | Drafts are TEMPLATES ONLY: the existing 300 PDFs; no hosted generative drafting |
| C6 | 45 | language = detected posting language if one of the 15, else EN; German and "apply in X" need confirmation |
| C7 | 46 | PDFs resolved by slot through a manifest; the count 300 is inferred and MUST be verified (done: `evidence/JSK-T008/report.txt`) |
| FR-021 | 209 | language detected deterministically with confidence >= 0.8 |
| FR-029 | 220 | drafts assembled only from template PDFs by slot; missing slot -> `slot_missing` |
| FR-030 | 221 | language follows C6; confirmation rules |
| FR-031 | 222 | PDFs streamed unsealed to the operator only; approval pins the SHA-256; a differing file is refused |
| FR-071 | 277 | no machine translation of posting text |
| Edge cases | 171-178 | operator edits template between review and send (hash blocks); vault slot present for EN but not for the posting language |

There is **no FR, SC or task** that mentions ATS parsing, PDF structure, keyword coverage against a posting, link validity, DOCX, or upload-form behaviour.
`grep -rlw ATS specs/` returns no file; the only mentions in the repository are `README.md` and `docs/materials/mvp/inputs.md` (operator input).

### Evidence directories

`evidence/JSK-T008/report.txt` (manifest inspection: 300 slots, names, counts, hashes only; no content). `evidence/JSK-T060|T068|T070` and `JSK-GOTC` cover the draft/approval path. **No evidence directory contains a text-extraction, link, tagging or OCR measurement of the template PDFs** - this document set is the first.

## 5. Tests and evidence that exist

| Suite | Count | Covers |
|---|---|---|
| `_tools/career/test_pdf_pipeline.py` | 59 | gate unit and integration: Type 3 / unsubset / unembedded fonts, two-column rejection, URL intactness (incl. Hindi header), RTL rebuild byte-identical, reversed text fails the phrase gate, empty-contact failure |
| `_tools/career/test_gates_render.py` | 15 | rendering + gate wiring |
| `_tools/career/test_locale_guards.py` | 15 | locale completeness |
| `internal/draft/*_test.go` | 22 | see section 4 |
| `internal/templates/templates_test.go` | 4 | slot/hash |

(`def test_` counts by grep, 2026-11-24. Suites were not re-run for this document; the gate itself was.)

## 6. What does NOT exist (pointer to 11)

No link-annotation check; no tagged-PDF/XMP/PDF-A requirement; no word-boundary check across extractors; no posting-driven keyword coverage; no DOCX/plain-text output;
no posting-aware cover letter (company, role, date); no ATS-vendor profile (Greenhouse, Lever, Workday, ...); no knockout-question capture; no check that a PDF was gated *recently*; no filename policy;
no language detection beyond en/de/sr/ru. Each is ranked with evidence in `11-ats-gap-analysis.md`.

## 7. Tool availability on this host

| Tool | Present | Use here |
|---|---|---|
| `pdftotext`, `pdfinfo`, `pdffonts` (poppler) | yes | extraction (`-layout`, `-raw`, `-bbox`), metadata, fonts |
| `mutool` (MuPDF) | yes | second extractor (`mutool draw -F txt`) |
| `pypdf` 6.9.2 | yes | third extractor, catalog and annotation inspection |
| `pdfminer`, `PyMuPDF` (fitz) | **no** | not measured |
| `tesseract` 5.x | yes, langs `deu eng fra rus srp srp_latn osd` | OCR; **no** `ara`, `fas`, `hin`, `jpn`, `chi_*`, `kor`, `kaz`, `bel`, `tur`, `spa` models, so OCR was run only for en, de, ru, sr |
| `pdftoppm` | yes | rasterize for OCR |
| `qpdf`, `verapdf`, `pdfua` validators | no | PDF/A and PDF/UA conformance could not be machine-checked; only the presence of the markers was measured |
