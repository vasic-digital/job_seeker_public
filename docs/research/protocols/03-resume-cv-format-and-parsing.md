# 03 - Resume/CV format and ATS parsing

Index: [00-README-index.md](00-README-index.md). Data-model side (JSON Resume/Europass): [01](01-job-posting-data-standards.md). Retrieval date **2026-11-24**. Tags: `[S]` secondary (career/vendor blogs, search summaries), `[K]` background knowledge not re-fetched, UNCONFIRMED where stated.

**Evidence quality warning.** No ATS vendor publishes its parser rules in what we could fetch. Everything about "what parsers do" below is consensus from career/vendor blogs, several of which sell resume tools and quote unverifiable statistics (e.g. "61% of ATS skip a right sidebar", attributed to a 2024 study we could not find). We therefore **do not adopt any rule from this document without measuring it** with the sandbox in [08](08-observability-and-conformance-testing.md). Rules marked *measure first* stay off by default.

## Contents

1. [How ATS parsing works](#1-how-ats-parsing-works)
2. [File format rules](#2-file-format-rules)
3. [Layout, fonts, sections, dates, keywords](#3-layout-fonts-sections-dates-keywords)
4. [Language and locale](#4-language-and-locale)
5. [Country conventions: US, UK, DE, FR, RU, RS](#5-country-conventions)
6. [Accessibility](#6-accessibility)
7. [Integration for job_seeker](#7-integration-for-job_seeker)
8. [Priority table](#8-priority-table)

## 1. How ATS parsing works

Pipeline (generic, `[S]` consensus, vendor-specific details UNCONFIRMED): file -> text extraction (PDF text layer or DOCX XML; OCR only in some products) -> segmentation into sections via heading heuristics -> entity extraction (name, contact, employers, titles, dates, schools, skills) -> optional taxonomy normalisation -> structured profile + the original file kept for recruiters. Failure modes reported consistently across sources `[S]`: scanned/image-only PDFs (no text layer), multi-column reading order merged across columns, text boxes and tables dropped or scrambled, contact data in page headers/footers skipped, non-standard section names, icons/graphics ignored.

Important consequence: **a human sees the original file; the parser's output drives search and filters.** Optimise for both: one column, real text, standard headings, plus a visually clean layout.

## 2. File format rules

| Question | Consensus `[S]` | Our rule |
|---|---|---|
| PDF vs DOCX | both parse when text-based; DOCX is the predictable default when the form accepts it; PDF exported from a word processor (not "printed" or scanned) is fine; deciding factor is internal structure | default **PDF (text layer) + DOCX** both generated from the same source; send what the form asks for; if no preference, DOCX for portals with autofill, PDF for email |
| Scanned/image PDF | fails without OCR | never produce; gate rejects PDFs with no extractable text |
| PDF/A (ISO 19005) | archival; embeds fonts, forbids some features; helpful for stable text extraction but no vendor requirement found | *measure first*; MAY |
| Tagged PDF / PDF/UA (ISO 14289) | accessibility structure tree; search did not cover its effect on ATS: UNCONFIRMED | produce tagged PDF for accessibility (section 6), verify extraction order in sandbox |
| File size | Greenhouse accepts pdf/doc/docx/txt/rtf `[P: docs.greenhouse.io/job-board.html]`; SmartRecruiters <= 2 MB per attachment, PDF/DOC(X)/RTF/JPG/PNG `[P: developers.smartrecruiters.com/docs/post-an-application.md]` | keep every artefact **<= 1 MB**, which satisfies the smallest limit found |
| File name | no standard | `Firstname-Lastname-CV-<lang>.pdf`, ASCII transliteration for portal uploads, UTF-8 kept inside the document |
| Metadata | PDF Info/XMP often carries tool names and author | set `Title`, `Author`, `Lang`; strip tool-identifying noise; never embed private fields (compliance with the content-boundary rule) |

## 3. Layout, fonts, sections, dates, keywords

- **Layout:** single column, no tables for layout, no text boxes, no headers/footers for contact data, no images of text, no icons for meaning (phone/email as text) `[S]`.
- **Fonts:** common system fonts, embedded subset in PDF; standard sizes (body 10-12 pt). Sources claim heading-size mismatches can confuse parsers: weak claim, *measure first*.
- **Headings:** conventional English names (Experience/Work Experience, Education, Skills, Summary, Projects, Certifications, Languages) and their localised equivalents (see section 4). Use real heading styles in DOCX.
- **Dates:** consistent, machine-friendly forms such as `MMM YYYY - MMM YYYY` or `YYYY-MM`; the word "Present" for current roles; no ranges split over lines. JSON Resume uses ISO 8601 ([01](01-job-posting-data-standards.md)).
- **Keywords:** mirror the posting's exact terms **only where true for the user**. job_seeker rule: the drafter may re-order and surface existing facts from `career_snapshot`; it must not add skills, titles or employers the user does not hold. Keyword stuffing and hidden white text are both rejected by modern parsers/recruiters and are dishonest (list under Risks).
- **Length:** 1-2 pages for most markets; longer academic CVs are a separate template.

## 4. Language and locale

- Produce the CV **in the posting's language** (`Posting.Lang`, confidence >= 0.8 rule from `internal/match/lang.go`); keep per-language masters (JSON Resume `meta.translations`).
- Locale rules to encode in templates: date format, decimal separator, phone format E.164 (`+381...`, `+49...`), address order, paper size A4 (EU/RS/RU) vs US Letter (US/CA).
- Cyrillic: PDF fonts must embed Cyrillic glyphs (Serbian Cyrillic and Latin both occur; DejaVu/Noto-family fonts are common choices `[K]`); DOCX should declare `w:lang`. Whether specific ATS parsers handle Cyrillic names reliably is **UNCONFIRMED**; for Cyrillic-language CVs also supply a Latin transliteration of the name in the header when applying to non-RU/RS employers (measure).
- Right-to-left scripts: not in scope.

## 5. Country conventions

All claims below come from consumer career blogs `[S]` (sources in the log) except where noted, and are **orientation, not rules**. Photo and personal-data fields interact with anti-discrimination law and with GDPR data minimisation ([05](05-identity-trust-privacy-and-legal.md)); the safe default is *omit*, and enable per-country templates only with explicit user opt-in.

| Market | Photo | Age/birth date/marital status | Notes |
|---|---|---|---|
| US / Canada | omit (discrimination-risk, strongly discouraged) | omit | resume, 1-2 pages, Letter size, no references line |
| UK | omit (norm) | omit | "CV", A4, personal statement; no age/marital status |
| Germany | still usual in many industries but no longer required `[S]` | birth date traditionally listed; optional | tabular Lebenslauf, reverse chronological, signed/dated historically; AGG (anti-discrimination law) makes photo/age optional: UNCONFIRMED legal text not fetched |
| France | common but declining/mixed `[S]` | optional | |
| Serbia (RS) | no authoritative source found | no authoritative source found | **UNCONFIRMED**: this research found nothing on RS or RU conventions. Do not assert; ask the operator for 2-3 real examples of RS/RU employer expectations (no synthetic gold) |
| Russia (RU) | no authoritative source found | no authoritative source found | UNCONFIRMED as above; RU job sites (hh.ru-style forms) hold structured resumes: a separate integration question |
| EU generic / Europass | optional | optional | Europass CV layout is a recognised option for public-sector and EU-institution employers |

## 6. Accessibility

- Output PDFs as **tagged PDF** with document language and title; DOCX uses real styles and alt text for any image. Standards: PDF/UA (ISO 14289), WCAG 2.2 (W3C Recommendation) `[K]`, UNCONFIRMED current editions. Accessibility and ATS parseability point in the same direction (semantic structure, no text in images).
- Candidate-facing console accessibility is outside this file.

## 7. Integration for job_seeker

Packages: `internal/cvrender` (render), `internal/cvlint` (pre-flight lint), reuse `internal/cvdata` from [01](01-job-posting-data-standards.md).

```go
// internal/cvrender
type Format string // "pdf", "docx", "txt"
type Artifact struct {
    Format  Format
    Lang    string // BCP 47
    Bytes   []byte
    SHA256  string
    Profile string // template id: "single-column-en", "de-tabular", "europass"
}
type Renderer interface {
    Render(ctx context.Context, r cvdata.Resume, profile string, f Format) (Artifact, error)
}

// internal/cvlint: fail closed before any artefact reaches a draft
type Finding struct{ Rule, Severity, Detail string }
func Lint(a Artifact) ([]Finding, error)
// rules: HAS_TEXT_LAYER, SIZE_LE_1MB, SINGLE_COLUMN_ORDER, NO_TEXTBOX_TABLE_LAYOUT,
//        CONTACT_IN_BODY, LANG_TAG_SET, FONTS_EMBEDDED, DATES_CONSISTENT, NO_PRIVATE_FIELDS
```

Implementation lines worth fixing now:
1. Round-trip check in `Lint`: extract text from the produced PDF (library choice for Part C; `pdftotext` exists on the dev host but must not become a hard runtime dependency) and assert that section headings appear in the intended order and that the email and phone strings are found in the first 30% of text.
2. DOCX: build with real heading styles; assert no `w:txbxContent`, no `w:tbl` used for layout.
3. `Profile` selection by `Posting.Countries` and `Posting.Lang`; the user chooses photo/age fields per profile in settings (default off); changing defaults requires a settings audit entry.
4. The drafter (`internal/draft`) takes only fields from `cvdata` with source pointers; a unit test fails if an output skill/employer is absent from the master.

Test plan: golden render tests for EN/RU/SR/DE (fixtures hold synthetic *structure* only, since a CV is personal data: use the operator's own CV in the private data dir for live runs, never in git); lint mutation proofs (inject a text box -> `NO_TEXTBOX_TABLE_LAYOUT` must fire); parser-sandbox comparison in [08](08-observability-and-conformance-testing.md).

**Risks.** Over-tailoring that misrepresents the candidate; relying on unverified blog rules; template proliferation. Cyrillic font licensing (embed only open-licence fonts).

## 8. Priority table

| Item | Priority |
|---|---|
| Text-layer single-column PDF + DOCX from one source; lint gate | MUST |
| <= 1 MB artefacts, ASCII file names, language tag | MUST |
| Honest keyword alignment from user's own facts | MUST |
| Tagged PDF output | SHOULD |
| Per-country profiles (US/UK/DE/EU) with opt-in photo/age | SHOULD |
| RS/RU conventions | MAY, blocked on operator-supplied real examples |
| PDF/A, Europass layout | MAY |
