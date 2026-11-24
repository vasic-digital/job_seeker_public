# 01 - Job-posting data standards

Part A of the protocols research. Index and method: [00-README-index.md](00-README-index.md).
Retrieval date for every fetched source: **2026-11-24**. Source tags: `[P]` primary/vendor document fetched this session, `[S]` secondary (blog, scraper listing, law-firm note), `[K]` known standard cited from background knowledge and NOT re-fetched (version/date UNCONFIRMED).

## Contents

1. [Why this matters for job_seeker](#1-why-this-matters-for-job_seeker)
2. [schema.org JobPosting and Google for Jobs](#2-schemaorg-jobposting-and-google-for-jobs)
3. [Feeds: RSS, Atom, JSON Feed, sitemaps](#3-feeds-rss-atom-json-feed-sitemaps)
4. [Aggregator feed specs: Indeed, LinkedIn, Glassdoor, ZipRecruiter](#4-aggregator-feed-specs)
5. [HR Open Standards / HR-XML / HR-JSON](#5-hr-open-standards--hr-xml--hr-json)
6. [Skills and occupation taxonomies: ESCO, O*NET, ISCO, Open Skills](#6-skills-and-occupation-taxonomies)
7. [Candidate-side formats: Europass, JSON Resume, hResume](#7-candidate-side-formats)
8. [Priority table](#8-priority-table)

## 1. Why this matters for job_seeker

job_seeker is a **private** tool (spec FR-010: postings are never published, exported to third parties, or rendered with structured JobPosting markup; guarded by `JSK-T046`). That shapes how every standard below is used:

- **Consume, never emit.** schema.org `JobPosting` is a *read* format for us (parse it out of employer pages and ATS-hosted pages). We must not generate JobPosting JSON-LD in any template. The existing test in `internal/templates` (templates emit none) stays the guard.
- **Candidate-side formats are the opposite**: JSON Resume / Europass are formats we may *emit* (our own user's CV), never third-party postings.
- Everything lands in the canonical `providers.Posting` (`internal/providers/providers.go`: `Title, Company, LocationRaw, SalaryMin/Max/Period/Cur, RemoteMode, TZHint, Countries, Tags, PostedAt, ExpiresAt, ApplyURL, Body, Lang`). Several standards below suggest **additive** fields; those are proposals for Part C (architecture), not duplicated here.

## 2. schema.org JobPosting and Google for Jobs

**What it is.** schema.org `JobPosting` is the vocabulary employers and ATS pages embed (JSON-LD, Microdata, RDFa) so that Google for Jobs and other crawlers can index a job. Google's structured-data documentation is the de-facto profile of it.

**Authoritative specs.**
- schema.org type page: <https://schema.org/JobPosting> `[P]` - the fetched copy reports **schema.org V30.1, dated 2026-09-16**.
- Google Search Central, JobPosting structured data: <https://developers.google.com/search/docs/appearance/structured-data/job-posting> `[P]` (page shows no "last updated" date in the fetched text: UNCONFIRMED date).

**Google requirements as fetched `[P]`.**

| Property | Status | Rule that matters to us |
|---|---|---|
| `title`, `description`, `datePosted`, `hiringOrganization`, `jobLocation` | required | `description` is HTML and must not equal the title; `hiringOrganization` may be `confidential`; `jobLocation` needs `addressCountry` but is not required for fully remote jobs that carry `applicantLocationRequirements` |
| `directApply` | recommended | "URL allows direct application"; applicants should not need extra steps such as repeated sign-ins or multiple forms |
| `validThrough` | recommended | ISO 8601; omit if no expiry/unknown. Google warns that not acting on expired jobs may lead to a manual action |
| `jobLocationType` | recommended | `TELECOMMUTE` only for **100% remote**; "occasional or negotiable remote work does not qualify" |
| `applicantLocationRequirements` | recommended, required for geo-limited 100% remote | at least one country for remote jobs; description must state the restriction |
| `baseSalary` | recommended | must be the employer's actual salary, not an estimate; `unitText` in `HOUR, DAY, WEEK, MONTH, YEAR` (case-sensitive); `minValue`/`maxValue` for ranges |
| `employmentType`, `identifier` | recommended | |
| `educationRequirements`, `experienceRequirements`, `experienceInPlaceOfEducation` | beta | may not show in results |

Additional guideline facts: markup only on single-job pages; applicants must be able to apply without paying and view details without login; the Indexing API is recommended to notify of new/updated/removed job URLs.

**schema.org properties of interest `[P]`** (from the V30.1 type page): `applicantLocationRequirements` (explicitly *not* for visa/citizenship rules), `directApply`, `estimatedSalary` (third-party estimate, **distinct from** `baseSalary`), `incentiveCompensation` (supersedes `incentives`), `jobBenefits` (supersedes `benefits`), `occupationalCategory` (ideally O*NET-SOC or ISCO-08), `relevantOccupation`, `skills`, `qualifications`, `responsibilities`, `workHours`, `eligibilityToWorkRequirement`, `securityClearanceRequirement`, `totalJobOpenings`, `jobStartDate`, `jobImmediateStart`, `employmentUnit`. `eligibleRegion` did **not** appear on the JobPosting page: UNCONFIRMED as a JobPosting property (it belongs to Offer/Demand types).

**Who uses it.** Virtually every ATS-hosted careers page (Greenhouse, Lever, Ashby, Workable, SmartRecruiters, Teamtailor, ...) injects it; Google for Jobs, Bing, Indeed (crawl), Adzuna and many aggregators read it. **UNCONFIRMED:** per-vendor completeness of the markup; it must be measured per tenant (see [08](08-observability-and-conformance-testing.md)).

**What it means for job_seeker.**

| Stage | Use |
|---|---|
| Finding | Universal fallback adapter: for any employer careers URL (and any ATS page with no API), fetch the page through the existing egress client and parse `<script type="application/ld+json">` for `@type: JobPosting` (also inside `@graph`). This yields `title, company, datePosted, validThrough, jobLocationType, applicantLocationRequirements, baseSalary, directApply` with no vendor-specific code. |
| Matching | `jobLocationType=TELECOMMUTE` + `applicantLocationRequirements` is the cleanest machine-readable remote-eligibility signal in the ecosystem; feeds the eligibility gate in [06](06-remote-work-eligibility-standards.md). `baseSalary.value.{minValue,maxValue,unitText}` + `currency` map onto `SalaryMin/Max/Period/Cur`. |
| Drafting | `directApply=true` signals a one-step application; `applicationContact` may hold a recruiter email for the email channel ([04](04-email-and-transport-protocols.md)). |
| Tracking | `validThrough` drives `ExpiresAt` and the freshness/ghost-job score ([07](07-matching-ranking-and-quality-signals.md)); a past `validThrough` or HTTP 404/410 means "closed". |

**Concrete integration.**

Package: `internal/providers/jsonld` (new), file `jobposting.go`.

```go
// Package jsonld extracts schema.org JobPosting from HTML. Read-only: job_seeker never emits it (FR-010).
type JobPosting struct {
    Title, Description, DatePosted, ValidThrough string
    HiringOrg                                   Org
    JobLocationType                             string   // "TELECOMMUTE" or ""
    ApplicantCountries                          []string // from applicantLocationRequirements (Country names or ISO codes)
    Locations                                   []Place
    BaseSalary                                  *MonetaryAmount
    EmploymentType                              []string
    DirectApply                                 *bool
    Identifier                                  string
    OccupationalCategory                        []string // O*NET-SOC / ISCO codes if present
    Raw                                         json.RawMessage
}

// Extract returns every JobPosting node found in the document (handles @graph, arrays, nested "mainEntity").
func Extract(htmlBody []byte, pageURL string) ([]JobPosting, []Warning, error)

// ToPosting maps to the canonical record. Never invents salary: missing -> zero values.
func (j JobPosting) ToPosting(provider, pageURL string) providers.Posting
```

Implementation notes (lines of code, not a blob):
1. Use `golang.org/x/net/html` (already in `go.mod`) tokenizer; collect `script[type="application/ld+json"]` text; `json.Unmarshal` into `any`; walk for `@type == "JobPosting"` (string or array). Tolerate trailing commas/HTML entities: on failure append a `Warning`, do not abort the batch.
2. `baseSalary.value` may be a number, a `QuantitativeValue` or `MonetaryAmount` object; `unitText` must be normalised case-insensitively (`YEAR|ANNUAL|HOUR|...`) to our `SalaryPeriod`; `currency` ISO 4217.
3. `applicantLocationRequirements` may be `Country`, `AdministrativeArea`, or an array; resolve names to ISO 3166-1 alpha-2 via the table in [06](06-remote-work-eligibility-standards.md). Keep unresolved strings in `Raw`.
4. Body goes through the existing allow-list sanitizer (`internal/providers/sanitize.go`) - JSON-LD `description` is untrusted HTML.
5. Egress and rate: fetch through `egress.Client`, record a `CallProof`, honour `robots.txt` ([05](05-identity-trust-privacy-and-legal.md)); one fetch per URL per refresh interval via `internal/limiter`.

Test plan: golden fixtures captured from real ATS pages (committed under `internal/providers/jsonld/testdata/` with a provenance manifest per `JSK-T057` style, no synthetic gold); table tests for salary units, `@graph`, remote+country cases, malformed JSON; a live contract test (`JS_LIVE=1`) that fetches three real careers pages and requires a non-empty extraction; a mutation proof that deleting the `@graph` walker fails a fixture. Gate: extend `gates/egressban` expectations so the package cannot import a JobPosting *writer*.

**Risks.** Markup can be wrong or SEO-stuffed (Google itself bans misleading markup); `baseSalary` may be placeholder values; `validThrough` is often far-future; markup may be added client-side (JS) and invisible to a plain fetch. Never treat JSON-LD as ground truth for eligibility - show it with provenance.

**Priority: MUST** (universal fallback; also the reference vocabulary for the whole canonical model).

### 2.1 Google Indexing API (not for us)

The Indexing API is an *emitter-side* tool for employers/aggregators `[P]`. job_seeker is private and does not publish; **do not implement**. Priority: **WON'T**, recorded so nobody adds it by mistake.

## 3. Feeds: RSS, Atom, JSON Feed, sitemaps

| Standard | Spec | Status | Relevance |
|---|---|---|---|
| RSS 2.0 | <https://www.rssboard.org/rss-specification> `[K]` | frozen | Teamtailor `/jobs.rss` is documented by the vendor `[S: search summary of support.teamtailor.com/en/articles/11171756]`; Pinpoint offers RSS/JSON; many remote boards |
| Atom | RFC 4287 `[K]` | stable | some ATS and boards |
| JSON Feed | <https://www.jsonfeed.org/version/1.1/> `[K]` | 1.1 | rare for jobs; cheap to support |
| Sitemap | <https://www.sitemaps.org/protocol.html> `[K]` | stable | discovery of job URLs per employer; combine with JSON-LD adapter |
| HTTP conditional GET | RFC 9110/9111 `[K]` | stable | `ETag`/`If-Modified-Since` to cut polling cost |

**Integration.** Package `internal/providers/feed` with one parser that normalises RSS/Atom/JSON Feed into `[]FeedItem{ID, URL, Title, Updated, ContentHTML, Extensions map[string]string}`; per-vendor mappers (Teamtailor, Pinpoint) turn items into `Posting`. Use `ETag`/`Last-Modified` stored in `provider_call` rows; honour `Retry-After`. Test plan: real feeds saved as fixtures + live smoke; fuzz the XML decoder with entity-bomb input (cap size with `http.MaxBytesReader`). **Priority: SHOULD.** Risk: feeds are lossy (no structured salary/remote flag); treat `ContentHTML` as untrusted.

## 4. Aggregator feed specs

These are **employer/ATS-to-board** *push* formats. job_seeker is a *consumer* of postings and is not an employer, so we do not implement them as emitters. They matter in two ways: (a) they explain which fields upstream boards carry (so our canonical model does not lose them), (b) their terms forbid scraping, which constrains where we may read from.

- **Indeed Job Sync XML feed** `[P: search result of docs.indeed.com/job-sync-xml/xml-feed and docs.indeed.com/indeed-apply/xml-feed]`: one `<job>` per posting; job-level elements include `title, date, referencenumber, url, company, city, state, country, postalcode, description, salary, jobtype`; duplicate `referencenumber` keeps only the first; the feed must contain every job published on the web; ATS partners are steered to the **GraphQL Job Sync API** (`docs.indeed.com/job-sync-api`), partner-gated. **Indeed Apply** is enabled by `<indeed-apply-data>` metadata with `indeed-apply-*` parameters and posts candidate data to the employer/ATS (`docs.indeed.com/indeed-apply/ats`). All partner/employer side: no public consumer API is documented in what we fetched.
- **ZipRecruiter** partner toolkit: Jobs API, XML feed import and an *Apply webhook*; commercial ATS must apply for partnership `[S: search result www.ziprecruiter.com/partner/documentation/]`.
- **LinkedIn Job Posting / Talent APIs, Glassdoor feeds:** **UNCONFIRMED** - searches in this session returned no primary document. Both are partner/employer programmes. LinkedIn's User Agreement section 8.2 forbids bots/automated access per secondary sources `[S: applygoat.com, lastroundai.com]`; primary text not fetched: UNCONFIRMED. Policy for job_seeker: **no scraping or automation against LinkedIn, Indeed or Glassdoor** (see [05](05-identity-trust-privacy-and-legal.md)); they can appear only as *human-opened links* the user pastes in (`imported_by_user` source), never as crawled providers.

**What it means for job_seeker.** Keep the canonical posting model able to hold `referencenumber`/requisition id, `jobtype`, and an `apply_method` enum (`hosted_form | indeed_apply | email | external`) so imports from user-pasted URLs are not lossy. **Priority: MAY** (field-level compatibility only); emitter implementations are **WON'T**.

## 5. HR Open Standards / HR-XML / HR-JSON

- **What.** HR Open Standards (formerly HR-XML Consortium) publishes recruiting schemas (SEP - Staffing Exchange Protocol; "Recruiting" in HR-JSON) as XSD and JSON Schema.
- **Spec/status.** Current version could **not** be confirmed: search found HR-JSON 4.1 (2018) material and a **4.3.0-rc1** candidate announcement with no final release date `[S]`; the 2021 recruiting/JDX presentation says recruiting would ship JSON Schema + XSD `[S]`. Treat the current version as **UNCONFIRMED**; check <https://www.hropenstandards.org> `[K]` before building.
- **Who uses it.** Enterprise HCM/ATS vendors and staffing exchange; very little *public* data is exposed in this format. No evidence found that any remote-job source we use publishes it.
- **Meaning for us.** Reference model for field naming only (candidate, position, application). Do not build an importer until a provider that emits it is identified.
- **Priority: MAY** (design reference). Risk: low; effort wasted if built speculatively.

## 6. Skills and occupation taxonomies

| Taxonomy | Authoritative source | Version found | Licence/format |
|---|---|---|---|
| **ESCO** | <https://esco.ec.europa.eu> `[S: portal summary via search]` | **v1.2.1 (Dec 2025)** minor release; previous major May 2024 (v1.2.0); translation/duplicate-label cleanup across 28 languages; no v1.3 found | CSV, JSON-LD, ODS, RDF, TTL, XML; Delta File of changes; EU open reuse (UNCONFIRMED exact licence text) |
| **O*NET** | <https://www.onetcenter.org/database.html> `[S]` | **30.2 (Feb 2026)** confirmed; a **30.3** page exists, release date UNCONFIRMED; taxonomy **O*NET-SOC 2019**, 923 data-collection occupations | Creative Commons (per page; confirm CC-BY 4.0 on the licence page) |
| **ISCO-08** | ILO <https://www.ilo.org/public/english/bureau/stat/isco/isco08/> `[K]` | ISCO-08 (ISCO-25 revision in progress: UNCONFIRMED) | reference classification |
| **Open Skills / Lightcast** | <https://lightcast.io/open-skills> `[K]` | UNCONFIRMED | free API with registration; commercial terms: UNCONFIRMED |
| **SFIA / DISCO / ISCED / EQF** | <https://sfia-online.org> `[K]` | UNCONFIRMED | SFIA is licensed |

**What it means for job_seeker.**
- *Matching.* Today the classifier uses `rules/area_rules.yaml` tokens for 10 areas in EN/RU/SR (`internal/match`). ESCO gives **multilingual alt-labels** (28 languages, including `sr` / `ru` variants where present; coverage per language UNCONFIRMED) usable to *expand the token lists with provenance*, and an ESCO URI is a language-independent skill key for dedup and for drafting ("skill X appears as Y in this posting's language").
- *Occupation code mapping.* `occupationalCategory` (JobPosting) in O*NET-SOC or ISCO-08 lets us cross-check the area classifier: disagreement -> review queue (`matchstore.ReviewQueue`).
- *Drafting.* Use skill labels in the posting's language when quoting the user's own skills; never add a skill the user's `career_snapshot` does not hold (honesty rule, [07](07-matching-ranking-and-quality-signals.md)).

**Concrete integration.** Package `internal/taxonomy`:

```go
type Concept struct{ URI, Scheme, PrefLabel string; AltLabels map[string][]string /*bcp47->labels*/ }
type Taxonomy interface {
    Version() string                       // recorded in every matchstore row that used it
    Lookup(lang, label string) []Concept   // exact + normalised (match.normalize)
    Broader(uri string) []string
}
// Loader reads a pinned, checksummed ESCO CSV snapshot from the private data dir; never downloaded at runtime.
func LoadESCO(dir string) (Taxonomy, error)
```

Rules: snapshot is **pinned and sha256-recorded** (no runtime network fetch; host disk is limited so load only `skills_<lang>.csv` + `occupations_<lang>.csv` for `en,ru,sr,de`, a few tens of MB - **UNCONFIRMED size, measure before committing**); versions go into `rules/score.yaml` metadata so scores stay reproducible (FR-022). Test plan: determinism test (same snapshot -> same expansion), a drift test comparing area tokens vs ESCO-expanded superset like the existing career-snapshot superset test, and a measured precision/recall delta on the gold set via `gates/verify_gold.sh` - adopt expansion **only if SC-003 does not regress** (gold set currently < 30 per area, so the gate is PARTIAL; record that honestly). **Priority: SHOULD** (ESCO), **MAY** (O*NET/ISCO cross-check). Risk: label expansion raises false positives; ESCO skill granularity is coarse for niche tech.

## 7. Candidate-side formats

### 7.1 JSON Resume
- Spec: <https://jsonresume.org/schema> `[P]` - **schema version 1.11.0**, JSON Schema **Draft-07**, sections `basics, work, volunteer, education, awards, certificates, publications, skills, languages, interests, references, projects, meta` plus `objective` and `custom` (extension point tools must preserve). Dates ISO 8601, countries ISO 3166-1 alpha-2, `meta.language` BCP 47, `meta.translations[]` for per-language documents.
- Use: canonical *export/interchange* shape for the user's CV master data; one file per language (matches our EN/RU/SR need) and the input to template renderers (PDF/DOCX, see [03](03-resume-cv-format-and-parsing.md)). Package `internal/cvdata`, `Resume` struct tagged to the schema, `Validate()` against the embedded Draft-07 schema (use a Go JSON-Schema validator; library choice open, Part C). **Priority: SHOULD.** Risk: schema has no field for work authorisation, notice period, or salary expectation - keep those in an `x-jobseeker` object inside `custom`.

### 7.2 Europass
- Europass moved credentials from XML to **JSON-LD** in EDC release 1.8 (June 2023); European Learning Model v3 launched May 2023 `[S: europass.europa.eu pages via search]`. Europass CV *document* generation is via the Europass web editor; whether a public CV JSON/XML API is still maintained in 2026 is **UNCONFIRMED** (the ECV schema documentation found is v3.0.0, 2020-06-02 `[S]`).
- Use: **export-only** option ("Europass-style" PDF/layout and ELM-aligned skill labels) for EU applications; no importer. **Priority: MAY.**

### 7.3 hResume / hRecruit microformats
Legacy microformats (`[K]`, UNCONFIRMED current use). Ignore on read unless a source needs it. **Priority: WON'T.**

## 8. Priority table

| Standard | Direction | Package | Priority |
|---|---|---|---|
| schema.org JobPosting + Google profile | read | `internal/providers/jsonld` | MUST |
| RSS/Atom/JSON Feed + sitemap + conditional GET | read | `internal/providers/feed` | SHOULD |
| ESCO | read (pinned snapshot) | `internal/taxonomy` | SHOULD |
| O*NET-SOC / ISCO-08 cross-check | read | `internal/taxonomy` | MAY |
| JSON Resume 1.11.0 | read+write (own CV) | `internal/cvdata` | SHOULD |
| Europass | write (export) | `internal/cvdata/europass` | MAY |
| HR Open / HR-JSON | reference | n/a | MAY |
| Indeed/ZipRecruiter/LinkedIn/Glassdoor feeds | field compatibility only | n/a | MAY / emitters WON'T |
| Google Indexing API | emit | n/a | WON'T |

Next: ATS and application APIs in [02-ats-and-application-apis.md](02-ats-and-application-apis.md).
