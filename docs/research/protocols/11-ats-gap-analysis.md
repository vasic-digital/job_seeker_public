# 11 - ATS gap analysis (measured)

| | |
|---|---|
| Part | B of the ATS protocol research |
| Date | 2026-11-24 |
| Inputs | `10-existing-ats-support-inventory.md`; the 300 PDFs in the umbrella `.cache-career/` (read-only); host tools `pdftotext`, `pdfinfo`, `pdffonts`, `mutool`, `pypdf`, `tesseract`, `pdftoppm` |
| Privacy | Counts, structure and public template facts only. No PDF body text, no overlay or vault value is quoted; handles in link targets are written `<handle>`. |
| Severity scale | **S1** wrong or unreadable application can reach an employer / clickable defect in every file; **S2** a real parser is likely to mis-read or drop information; **S3** weakens chances, no corruption; **S4** hygiene. |
| Honest labels | MEASURED = a command was run and its output is pasted. CODE = read from source. UNCONFIRMED = plausible, not proven on this host. The ATS vendors' parsers themselves are not installed here; every "a real parser would ..." is an inference from the three extractors available and is labelled so. |

## Contents

1. [Headline](#1-headline)
2. [Ranked gap register](#2-ranked-gap-register)
3. [Evidence per gap](#3-evidence-per-gap)
4. [Coverage matrix: what real parsers do vs what is checked](#4-coverage-matrix)
5. [Locale matrix (15 languages)](#5-locale-matrix)
6. [Things that are fine (measured)](#6-things-that-are-fine-measured)
7. [Measurements not possible here](#7-measurements-not-possible-here)

## 1. Headline

The existing artifact gate is real and strict about **text-layer hygiene** (ligatures, PUA, fullwidth, fonts, single column, page budget) and all 300 PDFs pass it (`{0: 300}`).
But it validates a *generic* file. Three things decide whether an application is read and considered, and none of the three is gated:

1. whether the **content matches the posting** (no posting-driven tailoring; self-referential keyword check with substring false positives; cover letter has no company, no role and a **build-time date frozen at 19 Nov 2026 in all 150 covers**),
2. whether the **structure beyond text** is usable (260 of 300 files have **broken link annotations**; 300 of 300 untagged, no XMP),
3. whether the **text layer reads the same in every extractor** (Korean and Hindi lose word boundaries in MuPDF; Arabic and Persian word order depends on the extractor).

## 2. Ranked gap register

| ID | Sev | Gap | Basis | Fix task (13) |
|---|---|---|---|---|
| G-01 | S1 | Cover letter is a fixed template: "Dear Hiring Team", no company, no role, **date frozen at build time (19 Nov 2026) in 150/150 covers**, stale today | MEASURED | T160, T161 |
| G-02 | S1 | 260/300 LuaTeX PDFs: 3 of 5 contact links are `GoToR` remote-file links to `<host>/<handle>.pdf`, the other 2 are scheme-less URIs (`i@...`, bare domain) - not `mailto:`/`https:`; clicking is wrong or dead. Gate checks only visible link text, so it passed 300/300 | MEASURED | T162, T163 |
| G-03 | S1 | No posting-driven tailoring: the draft uses the posting body only to pick a language; keyword coverage is checked against the profile's own list, never against the posting. Synthetic 20-term posting vs a Go CV: **12/20 present**, misses `Terraform`, `GraphQL`, `RabbitMQ`, `event sourcing`, `site reliability` and every synonym (`Golang`, `k8s`, `Amazon Web Services`) | MEASURED + CODE | T164, T165 |
| G-04 | S1 | `_has_keyword` squashes to `[0-9a-z]` and does substring tests: `"We use Django"` contains `"Go"` -> **True**; `"JavaScript"` contains `"Java"` -> True; `"Python3"` contains `"Python"` -> True. The keyword gate gives false assurance and would hide a real missing term | MEASURED | T166 |
| G-05 | S2 | Language auto-detection knows only en/de/sr/ru: a French, Spanish, Turkish, Arabic ... posting returns `und` -> **EN fallback**, although CV/cover templates exist for 11 more languages. "apply in X" regex is English-phrasing only | CODE (`match/lang.go:41-96`, `draft/language.go:47-76`) | T167 |
| G-06 | S2 | Word boundaries collapse in some extractors for Hindi and Korean: MuPDF yields **554 tokens (hi) / 540 (ko)** where poppler and pypdf yield ~1000 / ~950; longest token 86 chars (hi). Keyword matching in a MuPDF-based parser fails on these CVs. Mechanism (no real space glyphs from LuaTeX) UNCONFIRMED | MEASURED | T168 |
| G-07 | S2 | RTL (ar, fa) word order is extractor-dependent: for 59 source items of `cv-go-ar`, the first 4 words appear in logical order in 22 (mutool) / 20 (pypdf) but **reversed in 16 and forward in only 3** with `pdftotext -raw`; `pdftotext -layout` inserts 690 U+202B in one Arabic CV. A poppler-based ATS gets reversed phrases. The gate dismisses pdftotext bidi controls as tool output (`gates.py:348-357`) and its phrase check passes if *either* extractor matches (`:140-152`) | MEASURED | T169 |
| G-08 | S2 | No tagged structure: `/MarkInfo` 0, `/StructTreeRoot` 0, `Tagged: no` in 300/300; no XMP packet (0/300); no PDF/A or PDF/UA marker. Parsers that follow the tag tree (and accessibility checkers) have only geometry to go on | MEASURED | T170 |
| G-09 | S2 | No DOCX, plain text, Markdown or HTML export despite the operator input listing "PDF and other major formats (HTML, Markdown, Docx)"; many ATS upload forms and some parsers prefer DOCX; `FORMATS` was cut back to what exists. `templates.go:34` accepts only `.pdf` slots | CODE (`render/__init__.py:22-25`, `docs/plans/job-seeker-inputs-summary.md:15`) | T171 |
| G-10 | S2 | No guard that a template PDF was **gated recently or at all** by job_seeker: `templates.Resolve` checks the SHA against the manifest (tamper) but not gate verdict, build age or content date; an old or hand-copied vault would pass | CODE (`templates.go:118-137`) | T172 |
| G-11 | S3 | CVs are exactly **3/3 pages in 150/150** (limit 3): zero headroom; a content addition fails the build, a pasted-in variant would exceed typical 2-page screening norms for 16 years of experience claims (UNCONFIRMED norm) | MEASURED | T173 |
| G-12 | S3 | Section headings are localized (`Ausbildung`, `Образование`, `学歴`, ...). English-trained parsers key on `Education`/`Experience`/`Skills`; whether the parser handles each locale's labels is UNCONFIRMED. No heading-recognition test exists in any language | CODE + MEASURED (headings grep) | T174 |
| G-13 | S3 | Serbian: CV/cover are Cyrillic (script gate range 0400-04FF), but a Serbian **Latin** posting is also detected `sr` and receives the Cyrillic CV; employer script preference UNCONFIRMED | CODE (`lang.go:88-96`, `gates.py:66-77`) | T167 |
| G-14 | S3 | Cover letter word/paragraph screen is a WARN on the model, not a gate on the PDF; covers carry no per-employer anything (company, role, hiring manager, source) - FR-029 forbids generation, so tailoring must be *deterministic substitution* of operator-confirmed fields, which does not exist | CODE (`screen.py:277-301`) | T160 |
| G-15 | S3 | Filenames are `cv-<area>-<lang>.pdf` / `cover-<area>-<lang>.pdf` and served as `inline; filename="<slot>"`: no candidate name, same name for every application; upload forms and recruiters see a generic name | CODE (`templates.go:197`) | T175 |
| G-16 | S3 | No upload-form profile: size limits, accepted types, "one file only", character limits, paste-vs-upload fields differ by ATS vendor; nothing records or checks them. Largest file 120 KB (fine for typical limits; limits UNKNOWN) | CODE | T176 |
| G-17 | S3 | No knockout-question capture (work authorization, notice period, salary, location, years, clearance): the system hands off to the provider URL and the operator answers ad hoc | CODE (no field in `0001_init.sql`, none in `draft/`) | T177 |
| G-18 | S3 | Running header/footer on every page repeats `name - title - Curriculum Vitae  n/3` as extractable body text (3 copies per CV) | MEASURED (`pdftotext -layout`) | T178 |
| G-19 | S3 | Hyphenation: 1-7 lines per CV end in a hyphen where a compound word breaks; extractors that join lines literally keep the stray hyphen and a space (not shown here; content not quoted). Cannot be called auto-hyphenation without the source; low impact | MEASURED (counts) | T178 |
| G-20 | S4 | OCR cross-check (visual vs text layer) was never part of the gate. Measured now: en 98.4%, de 99.2% word recall; **ru 71%, sr 67.5%, ru cover 77%**. Cause UNCONFIRMED (engine accuracy on Cyrillic at 200 dpi vs a real text/visual mismatch) | MEASURED | T179 |
| G-21 | S4 | Dates: all 4-digit years with localized month names; no ISO or numeric forms; Arabic cover date places the colon after the day in visual order (`19 : <month> 2026`), a logical-order concern tied to G-07 | MEASURED | T169 |
| G-22 | S4 | Metadata: `Keywords` carries ~33 comma-separated terms in every PDF, no XMP duplicate; harmless to most parsers, possible keyword-stuffing signal; `/ViewerPreferences/DisplayDocTitle` absent | MEASURED | T170 |
| G-23 | S4 | No Hebrew locale (the directive names AR/HE RTL; the 15 locales have ar and fa only) | CODE | T180 |
| G-24 | S4 | Gate cost/coverage: it runs after the build only; job_seeker never re-runs it; no CI (by policy) and no scheduled re-verification of the set | CODE | T172 |

Counts: S1 = 4, S2 = 6, S3 = 9, S4 = 5 (24 gaps).

## 3. Evidence per gap

### G-01 Cover letter content (MEASURED)

```
$ for f in cover-*-*.pdf; do pdftotext -l 1 $f - | grep -m1 2026; done | <strip label> | sort | uniq -c
  20 2026年11月19日    10 2026년 11월 19일   10 19 नव॰ 2026   10 19 қар. 2026   10 19 нояб. 2026
  10 19 нов 2026      10 19 ліс 2026       10 19 nov. 2026  10 19 nov 2026    10 19 Nov. 2026
  10 19 Nov 2026      10 19 Kas 2026       10 19 :‬نوفمبر ‪2026‬‬  10 19 :‬نوامبر ‪2026‬‬
```
All 150 covers carry 19 Nov 2026 (the build epoch; `resolve.py:355-359,455` makes the date a build parameter). Today is 2026-11-24.
`cover-go-en.pdf` lines 7 and 9: a `Date:` line and `Dear Hiring Team,`; the German one opens `Sehr geehrtes Recruiting-Team,`. A generic grep over the English cover for company/role placeholders (`dear|hiring|team|company|[|{`) finds only the salutation and the closing; there is no field for the employer or the position. FR-029 forbids generated text, so the only compliant remedy is deterministic substitution into reserved slots (company, role, date at send time) from operator-confirmed posting fields.

### G-02 Broken link annotations (MEASURED)

```
$ python3 -I (pypdf)  cv-go-en.pdf   [LuaTeX]
[('/URI','<email>'), ('/URI','<site>'), ('/GoToR','-'), ('/GoToR','-'), ('/GoToR','-')]
$ python3 -I (pypdf)  cv-go-ar.pdf   [WeasyPrint]
[('/URI','mailto:<email>'), ('/URI','https://<site>'), ('/URI','https://github.com/<handle>'), ('/URI','https://gitlab.com/<handle>'), ('/URI','https://t.me/<handle>')]
$ GoToR /F targets over all LuaTeX files (260 files x 3):
  github.com/<handle>.pdf: 260   gitlab.com/<handle>.pdf: 260   t.me/<handle>.pdf: 260
```
Cause (CODE): `render/latex.py:137` emits `\href{<text>}{<text>}` with the same string for label and target, and the visible label has no scheme; hyperref reads a scheme-less target as a relative file (`GoToR` with `.pdf` appended) or a bare string. The WeasyPrint set is correct, which proves the intended result. The gate's contact check compares visible text only (`gates.py:229-234`), hence 300/300 green. Recruiters clicking GitHub/GitLab/Telegram from a LuaTeX PDF reach a non-existent file; parsers that harvest link annotations (several do, for profile URLs) get nothing usable. The e-mail is a bare `i@...` URI, not `mailto:`.

### G-03 / G-04 Keyword coverage (MEASURED)

Synthetic posting (invented, public terms) against `cv-go-en` extracted text, using the production function `screen._has_keyword`:

```
Go True | gRPC True | Kafka True | PostgreSQL True | Kubernetes True | Terraform False | AWS True | Redis True
Docker True | Prometheus True | CI/CD True | microservices True | event sourcing False | GraphQL False
Golang False | k8s False | Amazon Web Services False | RabbitMQ False | distributed systems True | site reliability False
-> 12 / 20 present
_has_keyword("We use Django and Google","Go") -> True      _has_keyword("We use Django","Go") -> True
_has_keyword("Java","C") -> False   _has_keyword("JavaScript","Java") -> True   _has_keyword("Python3","Python") -> True
_has_keyword("C++ and C#","C") -> True
```
The three failure classes: terms the CV truthfully lacks (cannot and must not be added: honesty rule), **synonyms and abbreviations** the CV has under another spelling (`Golang`/`Go`, `k8s`/`Kubernetes`, `AWS`/`Amazon Web Services`) which a matcher must be told about, and **false positives** from the squash-substring test. `screen.py:195-200` takes its keyword list from the profile (`resolve.py:113-118`), so even a perfect matcher would compare the CV to itself.

### G-05 / G-13 Language handling (CODE)

`match/lang.go:41` documents the codes `en, de, sr, ru, und`; stop-word tables exist for en, de, sr, ru only (`:27-31`). Any other posting language yields `und` and `DecideLanguage` returns `en` with rule `fallback_en` (`draft/language.go:75`). `minLangConf = 0.6` (`language.go:22`) differs from FR-021's 0.8 (`lang.go:17`); it is harmless today because `und` is stored below 0.8 (`AcceptLanguage`, `lang.go:110`), but the two constants can drift. `reExplicit` (`language.go:47`) matches English sentences only ("apply in German"), not "Bewerbung auf Englisch".

### G-06 Word boundaries per extractor (MEASURED)

```
lang  tok_pdftotext tok_mutool tok_pypdf  longest_token(pdftotext/mutool/pypdf)       (cv-go-<lang>)
ar 1070 1022 918 | be 958 964 979 | de 866 872 889 | en 926 932 945 | es 1141 1147 1162 | fa 1077 1020 933
fr 1056 1065 1082 | hi 994 554 1011 (23/86/18) | ja 426 403 455 | kk 923 929 942 | ko 949 540 996 (23/40/23)
ru 951 957 971 | sr 997 1003 1016 | tr 950 956 973 | zh 522 442 541
```
Character sequences are identical once whitespace is removed (difflib ratio 1.0 for ko, hi, zh); only the word boundaries differ. ja and zh have no inter-word spaces by nature. For hi and ko, MuPDF drops about 45% of the spaces.

### G-07 RTL order (MEASURED)

First four words of each source `<li>/<p>` of the HTML source compared with each extractor's output, bidi controls stripped:

```
cv-go-ar    pdftotext-raw  items 59  forward 3   reversed 16
cv-go-ar    mutool         items 59  forward 22  reversed 0
cv-go-ar    pypdf          items 59  forward 20  reversed 0
cv-go-fa    pdftotext-raw  items 59  forward 3   reversed 9      mutool 12/0     pypdf 9/0
cover-go-ar pdftotext-raw  items 8   forward 0   reversed 1      mutool 5/0      pypdf 4/0
cv-desktop-fa: U+202B x100 in pdftotext -layout; cv-android-ar: 690 bidi controls
```
(Items not counted as either are wrapped across lines or contain mixed Latin runs.) The cover's Arabic `Date:` line extracts in `pdftotext -raw` as `2026 <month> 19 :<label>`: visual order.

### G-08 Structure (MEASURED)

```
/Lang present 300/300   /MarkInfo 0/300   /StructTreeRoot 0/300   XMP /Metadata 0/300   Tagged: no 300/300
pdfa/pdfua identifiers: 0/300     bookmarks: 40 (WeasyPrint) / 260 none
```
Metadata that does exist: Title, Author, Subject, Keywords, CreationDate, `/Lang` - 300/300 each. Not machine-checkable here: PDF/UA and PDF/A conformance (no veraPDF).

### G-11 Page budget (MEASURED)

`pages {cv: 3 x 150, cover: 1 x 150}`; gate limit cv 3 (`gates.py:81`).

### G-12 Headings (MEASURED)

Education heading lines found per language: en `Education`, de `Ausbildung`, ru `Образование`, ja `学歴`; `Languages`-type lines exist as `Programmiersprachen`/`Языки` (technical languages) and `Spoken languages` (en). No test asserts a parser-recognizable heading set.

### G-20 OCR cross-check (MEASURED)

`pdftoppm -r 200`, `tesseract -l <eng|deu|rus|srp>`, word recall of the PDF text layer found in the OCR text, page 1:
```
cv-go-en 0.984 (ocr-not-in-text 2.3%)  cv-web-frontend-de 0.992  cover-go-en 0.990
cv-flutter-ru 0.712 (35% of OCR words absent from text)  cv-mobile-sr 0.675 (37%)  cover-android-ru 0.773 (30%)
```
Latin pages agree with their text layer to >= 98%. Cyrillic pages do not; whether tesseract's `rus`/`srp` models at 200 dpi explain it or a glyph/text-layer mismatch exists is **UNCONFIRMED** and is the first sub-step of T179 (re-run at 300 dpi, compare with Cyrillic-only tokens, then with a second engine).

## 4. Coverage matrix

| Aspect real parsers care about | Checked today? | Where | Verdict |
|---|---|---|---|
| Text layer exists | yes | `gates.py:84` | OK |
| Ligatures, PUA, fullwidth, ZWSP, soft hyphen | yes | `:28-38` | OK (0 hits in 300; sample ligature count 0) |
| Reading order (single column) | yes (heuristic) | `:172-226` | OK for LTR; no RTL logical-order test across extractors (G-07) |
| Word boundaries | no | - | G-06 |
| Fonts embedded | yes | `:286` | OK 300/300 |
| Link annotations valid | **no** | - | G-02 |
| Tagged structure / PDF-UA / XMP | **no** | - | G-08 |
| Metadata title/author/lang | not asserted (present) | `render/latex.py:172-177` | no gate; regress-able |
| File size | no | - | all < 121 KB, no guard |
| Page count | yes | `:270` | OK |
| Filename convention | no | `templates.go:197` | G-15 |
| Section heading detection | no | - | G-12 |
| Dates in a parseable form | no | - | G-21 |
| Keyword coverage vs posting | **no** | `screen.py:195` is profile-only | G-03, G-04 |
| Cover personalization / date | **no** | - | G-01 |
| DOCX / TXT alternative | no | - | G-09 |
| Visual vs text consistency (OCR) | no | - | G-20 |
| Hidden / duplicate text | partly: cover duplicate paragraph (WARN, model-level) | `screen.py:297` | sample: 0-1 repeated lines per file; no PDF-level hidden-text check (white text, off-page) |
| Language of posting vs document | yes (4 languages) | `lang.go` | G-05 |

## 5. Locale matrix

Per language, `cv-go-<lang>` plus the sample set (`pdfinfo`, `pdffonts`, extractors). "LTR/RTL" = reading direction; "script" = gate script check present.

| Lang | Engine | Max file bytes | Fonts | Script gate | Extractor agreement | Notes |
|---|---|---|---|---|---|---|
| en, es, fr, tr | LuaTeX | <= 41.9 KB | Noto Sans CID TrueType | n/a (Latin) | tokens within 1.5% | links G-02 |
| de | LuaTeX | 42.5 KB | same | n/a | 866/872/889 | umlauts survive in all 3 extractors (sample: 0 PUA, 0 ligature) |
| ru, be, kk, sr | LuaTeX | <= 46.6 KB | same | Cyrillic >= 200 | tokens within 2% | OCR recall low (G-20); sr is Cyrillic only (G-13) |
| hi | LuaTeX | 52.2 KB | NotoSansDevanagari (Regular, Bold) | yes | **mutool 554 vs ~1000** | G-06 |
| ko | LuaTeX | 70.8 KB | NotoSansCJKkr | yes | **mutool 540 vs ~950** | G-06 |
| ja, zh | LuaTeX | 100.2 / 120.8 KB | NotoSansCJKjp / NotoSansCJKsc, CID Type 0C | yes | fewer tokens (no spaces) | fullwidth punctuation exempt only for zh/ja |
| ar, fa | WeasyPrint | <= 43 KB | Noto Naskh | yes | **reversed order in pdftotext -raw** | bidi controls in -layout; correct URIs; bookmarks present |

## 6. Things that are fine (measured)

- All 300: embedded + subset fonts, 0 Type 3, A4, unencrypted, `/Lang` set, Title/Author/Subject/Keywords/CreationDate set, reproducible build epoch.
- 20-file sample (all areas, 15 languages): 0 ligature code points (FB00-FB06), 0 PUA, 0 ZWSP/soft hyphen in `pdftotext`, `mutool` and `pypdf` outputs (20-file sample).
- Bullets are U+2022 only (plus U+00B7 as separator and U+00AB in ru): plain, extractable.
- Dates: month + 4-digit year ranges (`Mon yyyy`), en-dash ranges - parseable.
- Cover length ~300 words on one page (sample: en 301 OCR words / 300 text words).
- Three extractors agree at token-set Jaccard 1.0 for the 9 LTR Latin/Cyrillic CVs sampled (en, de, ru, sr, tr, kk, es, fr, be).
- job_seeker side: SHA-pinned draft, TOCTOU-safe single-read streaming, `slot_missing` instead of generation, language confirmation for `de`/override.

## 7. Measurements not possible here

| Wanted | Why not | Substitute |
|---|---|---|
| Real ATS parsers (Greenhouse, Lever, Workday, Taleo, iCIMS, Ashby, Teamtailor) | proprietary / network | three open extractors (poppler, MuPDF, pypdf); recorded as the proxy, vendor behavior UNCONFIRMED |
| OCR for ar, fa, hi, ja, zh, ko, kk, be, tr, es | no tesseract models installed | only en/de/ru/sr OCR; installing models is an operator action (package install) |
| veraPDF / PAC for PDF/A and PDF/UA | not installed | marker presence only |
| pdfminer.six, PyMuPDF | not installed | the three above |
| Word/DOCX parsers | no DOCX exists | - |
