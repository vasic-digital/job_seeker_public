# 02 - Generic profile and domain model

| | |
|---|---|
| Part | Product research, 2 of 9 |
| Date | 2026-11-24 |
| Status | PROPOSAL. Docs only; no production code changed. |
| Related | [01](01-vision-personas-and-market.md), [03](03-multi-tenant-architecture.md), [08](08-configurability-matrix.md), `../research/protocols/21-extension-interfaces.md`, `../research/protocols/22-strategy-and-policy-model.md`, `../research/providers/03-canonical-position-model.md` |

## Contents

1. [What is operator-specific today](#1-what-is-operator-specific-today)
2. [Target model in one picture](#2-target-model)
3. [Taxonomy mapping: ESCO, O*NET, ISCO](#3-taxonomy-mapping)
4. [Occupation Pack: replacing the 10 fixed areas](#4-occupation-pack)
5. [Profile schema and the Fact ledger (truthfulness)](#5-profile-schema-and-the-fact-ledger)
6. [Document engine decoupled from the operator's career engine](#6-document-engine)
7. [Template packs, country conventions, locale packs](#7-template-packs-country-conventions-locale-packs)
8. [The existing 10 areas become the `software-engineering` pack](#8-the-ten-areas-become-a-pack)
9. [Data model and migration sketch](#9-data-model-and-migration-sketch)
10. [Interfaces (Go)](#10-interfaces-go)
11. [Open questions](#11-open-questions)

## 1. What is operator-specific today

Read from `github/main` at the time of writing (2026-11-24). Every row is a coupling a generic product must break.

| Coupling | Where it lives | Why it is single-operator |
|---|---|---|
| 10 fixed areas, 1:1 mapping, "no new areas in v1" | `rules/area_rules.yaml`; spec D5, FR-013..FR-015 | The area set is the operator's career engine (`ai-engineering`, `android`, `android-aosp`, `android-tv`, `desktop`, `flutter`, `go`, `mobile`, `react-native`, `web-frontend`). Only software engineering. |
| Non-engineering titles force `unmapped` | `non_engineering_title_tokens` in `rules/area_rules.yaml`; FR-016 | Inverts for any other occupation: a recruiter or accountant is the target user. |
| Keyword snapshot of the operator's skills | `rules/career_snapshot.yaml`, `jsctl snapshot-career`; FR-019 | Derived from one person's `_content/career/areas/*.yaml` and `profile.yaml`. |
| Private overlay with a fixed key allow-list | `internal/match/overlay.go`, `CAREER_PRIVATE_DIR`; FR-055 | One directory on one disk; keys are the operator's (salary floor, tz window, blocked companies, allowed cities). |
| 300 PDF slots = 10 areas x 15 languages x {CV, cover} | spec C7, FR-029; vault output | Drafts are TEMPLATES ONLY (D8): no generation from a profile exists. |
| Cover letter = deterministic substitution of company/role/date | FR-102 | Fine as a mechanism; the letter body is the operator's. |
| Score weights (skills 0.35, area fit 0.20, ...) | FR-018, `rules/score.yaml` | Weights are global, not per user or per occupation. |
| 15 console locales | FR-068 | Console locales are good. Document locales (CV language, country convention) are a different axis (section 7). |
| Single identity in the generator | umbrella `_content/career/profile.yaml` (`schema: career.profile.v1`) | The profile is one person's verified record. |

What already generalises and must be kept: the Position/Listing model, the extension kernel (`internal/ext`, three-valued verdicts, `Resolve` with layers), the audit chain, envelope encryption, human approval, dry-run default, the egress allow-list.

## 2. Target model

```
Tenant (03)
 └─ Workspace
     ├─ Person (a job seeker; one per member, or client of a coach)
     │    ├─ Profile        (JSON Resume superset, versioned)
     │    │    └─ Fact      (atomic, evidenced claim; the only source of CV text)
     │    ├─ SearchFocus[]  (what this person is looking for; replaces "area")
     │    ├─ Constraint[]   (private: salary floor, tz, blocked employers; replaces overlay file)
     │    └─ Document[]     (rendered CV / cover; hash, template, locale)
     └─ PackSet            (enabled Occupation Packs, Template Packs, Locale Packs, Provider Packs)
```

Three principles:

1. **Data, not code.** An occupation is a pack (YAML + signed manifest), loaded the way `rules/*.yaml` are loaded today and hashed into `ruleset_sha`.
2. **Truth is a ledger.** Generated documents may contain only statements that trace to a Fact owned by the Person (section 5). This generalises the umbrella rule "NOTHING in this file may be invented" (header of `_content/career/profile.yaml`) from one file to a product invariant.
3. **Human approval is unchanged.** Dry-run default and no unattended send (spec D4, FR-025..FR-027) stay in every edition (see [06](06-compliance-privacy-and-ai-regulation.md) for why this also matters legally).

## 3. Taxonomy mapping

| Standard | What it gives us | Licence / reuse (retrieved 2026-11-24) | Use in the product |
|---|---|---|---|
| ESCO (EU) | Multilingual occupations and skills, mapped to ISCO | Commission FAQ: classification may be "downloaded, used, reproduced and reused for any purpose and by any interested party free of charge" under Decision 2011/833/EU; modified versions must be clearly indicated; ESCO API is EUPL 1.2. https://esco.ec.europa.eu/en/about-esco/faq . Exact current version/counts: UNCONFIRMED (not verified this session). | Primary skills vocabulary for EU markets and for synonyms across the 15 locales (ESCO labels are multilingual; which of our 15 are covered: UNCONFIRMED). |
| O*NET-SOC 2019 (US DoL) | 1,016 occupational titles, 923 with data; skills, tasks, technology, work activities | Database content CC BY 4.0, attribution must name the database version and US Department of Labor and indicate changes. https://onetcenter.org/license_db.html , https://www.onetcenter.org/taxonomy.html | Primary occupation + skills vocabulary for US/English markets; "technology skills" feed keyword packs. |
| ISCO-08 (ILO) | 436 unit groups; the common spine ESCO maps to | ISCO-08 pre-dates 2023-05-03; ILO says pre-2023 works do not automatically carry a CC licence: reuse terms UNCONFIRMED, check the copyright page. https://isco.ilo.org/en/isco-08 , https://www.ilo.org/fil/node/9321 | Store ISCO codes as foreign keys only (codes are facts); do not redistribute ISCO text until terms are confirmed by counsel. |
| JSON Resume | Open CV JSON: `basics, work, volunteer, education, awards, certificates, publications, skills, languages, interests, references, projects` | Schema repo is MIT-licensed on GitHub (https://github.com/jsonresume/resume-schema). The fetch of the schema file failed this session: section list is from the author's prior knowledge, UNCONFIRMED against the live schema. | Import/export format for Profile. |
| Europass | EU CV format; Europass Digital Credentials built on the European Learning Model (W3C Verifiable Credentials) | https://europass.europa.eu/en/information-developers (developer portal: data model, API, GitHub). The Europass CV XML schema was NOT confirmed from a primary source. | Export profile `europass`; credentials import later (UNCONFIRMED feasibility). |

Mapping rule: a Pack Taxon carries zero or more `esco_uri`, `onet_soc`, `isco08` codes. Codes are optional: a pack may be purely custom (a niche trade, a company-internal job family). Matching never depends on an external code being present.

## 4. Occupation Pack

An Occupation Pack replaces `area_rules.yaml` + `career_snapshot.yaml` + the non-engineering list. A pack is a directory with `pack.yaml`, optional `synonyms/<lang>.yaml`, and tests.

```yaml
schema: occupation_pack.v1
id: software-engineering          # lower_snake or kebab, unique per tenant
version: 1.0.0
title: { en: "Software engineering" }
licence: "proprietary-first-party"   # per-pack licence, shown in marketplace
taxa:                               # replaces "areas"
  - id: go
    title: { en: "Go backend" }
    codes: { onet_soc: ["15-1252.00"], isco08: ["2512"] }   # EXAMPLE values, to be verified against the registers
    tokens: { weight: 3, any: ["golang", "go developer", "gin", "grpc"] }
precedence:                         # same semantics as today's `precedence`
  - { winner: android-tv, losers: [android-aosp, android, mobile], min_winner: 6 }
excludes:                           # replaces non_engineering_title_tokens; pack-specific, user-overridable
  title_tokens: ["recruiter", "sales manager"]
seniority: { ladder: [intern, junior, mid, senior, staff, principal], aliases: { en: { senior: ["sr", "lead"] } } }
thresholds: { high_s1: 10, high_margin: 4, ambiguous_s1: 6 }     # today's values as defaults
score_defaults: { skills: 0.35, area_fit: 0.20 }                 # FR-018 weights as pack defaults; per-user override in 08
```

Behavioural changes versus today:

* D5 ("1:1 position to area, an `unmapped` bucket, no new areas") becomes: a Position maps to exactly one taxon *of one enabled pack*, or `unmapped`; users and coaches MAY create taxa (custom pack) because the tenant owns its taxonomy.
* Multi-pack search: a person may enable several packs (a nurse moving into health-informatics). The mapper runs per pack and the Position keeps one mapping per pack (table `mapping` gains `pack_id`, section 9).
* "Non-engineering forced unmapped" becomes pack data. A pack with no `excludes` excludes nothing.
* Cold start: with no pack the product offers (a) free-text search focus (title + keywords), which produces scored but unmapped Positions, and (b) a pack picker seeded from ESCO/O*NET. Quality of unmapped scoring versus mapped: UNKNOWN, to be measured by the existing gold-set method (FR-023).

## 5. Profile schema and the Fact ledger

Profile is a JSON Resume superset. The extension keys live under `x-jobseeker`, so a standard JSON Resume file imports and exports without loss.

The Fact ledger is the product's truthfulness mechanism (a unique selling point in [01](01-vision-personas-and-market.md)):

```go
// Fact is one atomic, owner-asserted claim. Documents may only be composed from Facts.
type Fact struct {
	ID        string    // ULID
	PersonID  string
	Kind      FactKind  // employment, education, skill, achievement, certification, language, project, ...
	Statement map[string]string // locale -> text written or approved by the PERSON; machine translation is flagged
	Evidence  []Evidence        // optional: document hash, URL, referee; strength shown in UI
	AssertedAt time.Time        // the person pressed "this is true"
	AssertedBy string           // the person; a coach may propose but never assert
	Status    FactStatus        // proposed | asserted | retired
}
```

Rules, each testable as a gate (same style as today's gates):

1. A rendered document stores `fact_ids[]`. A gate rejects any CV line that has no fact id (sentence-level provenance).
2. Tailoring may select, order, and translate Facts and may fill declared slots (company, role, date); it may not add a claim. AI-assisted rewording, if offered, produces a *proposed* Fact that the person must assert; it never edits an asserted one silently. (This keeps D8 "templates only" as the default and makes any later generation opt-in and reviewable.)
3. A coach acting for a client proposes Facts; only the client's assertion makes them usable (role matrix in [03](03-multi-tenant-architecture.md)).
4. Competitor context: auto-apply tools are reported to be criticised for volume and spam (https://www.nbcnews.com/tech/innovation/ai-making-job-applications-easier-creating-another-problem-rcna179683, retrieved 2026-11-24); the ledger is how we differ. No competitor was verified to lack such a ledger: UNCONFIRMED.

## 6. Document engine

Today the engine is the umbrella's career engine (Python generator under `_content/career`, vault output of 300 PDFs, served unsealed by slot). Product target: a self-contained renderer inside job_seeker, selected by a port.

```go
// DocumentRenderer is a plugin port in the internal/ext registry (kind "renderer").
type DocumentRenderer interface {
	Info() ext.Info
	Render(ctx context.Context, in RenderInput) (RenderedDoc, error)
}

type RenderInput struct {
	Profile    ProfileView   // only asserted Facts selected by the Selector
	FactIDs    []string
	Template   TemplateRef   // pack id + template id + version
	Locale     string        // document language, independent of console locale
	Country    string        // CV convention, e.g. "DE", "US"
	Slots      map[string]string // company, role, date (FR-102 style)
	Format     string        // "pdf", "docx", "html", "json-resume", "europass"
}

type RenderedDoc struct {
	Bytes      []byte
	SHA256     string
	FactIDs    []string // echoed; the provenance gate compares with the input
	ATSReport  ATSReport // text-extractable? fonts embedded? single column? (ATS-safe docs, USP)
}
```

Migration path from the operator's engine (no regression):

1. `renderer/vault_slots` adapter: wraps today's manifest lookup (area, lang, kind -> path + sha). The operator tenant keeps working unchanged.
2. `renderer/native`: Go or headless-HTML-to-PDF renderer from Profile + Template Pack. PDF toolchain choice (and its licence) is OPEN: see [05](05-commercial-licensing-and-third-party-terms.md) section on dependency audit.
3. Deprecate the vault adapter only when the operator's pack reproduces its documents (golden-master comparison of text content, not pixels).

## 7. Template packs, country conventions, locale packs

Three independent axes, because today's "language" conflates them:

| Axis | Today | Product |
|---|---|---|
| Console language | 15 locales, `internal/i18n/messages/<lang>.json`, per-language independent review (D10) | Same; plus tenant-supplied overrides (white-label wording). |
| Document language | slot lang chosen by C6 rule | Per-document, any language for which the person has a Facts translation or the pack has a locale pack. |
| Country convention | not modelled (the `format/country_then_default` strategy exists in policy, `docs/research/protocols/22`) | A Format Profile per country: section order, photo yes/no, date format, page length norm, personal-data fields to omit. |

Country conventions (for example whether a photo, date of birth or marital status is customary) are legal-adjacent (anti-discrimination) and culture-specific. **Not researched with primary sources in this document: UNCONFIRMED.** Plan: a Format Profile for a country ships only with a cited source and a reviewer from that locale; the default profile omits protected-characteristic fields everywhere. Task proposed in [09](09-product-roadmap-and-spec-impact.md).

Template Pack = `{templates/*.html + css, fonts, preview.png, ats_profile, licence}`; fonts MUST be redistributable (audit in 05). Locale Pack = `{ui strings, date/number formats, section headings, salutations, address formats}`; RTL (ar, fa) is already a requirement (FR-070) and becomes a pack capability flag.

## 8. The ten areas become a pack

| Today | In the `software-engineering` pack |
|---|---|
| `area_rules.yaml` 10 areas, precedence, thresholds | `pack.yaml` taxa + `precedence` + `thresholds`, byte-for-byte equivalent behaviour |
| `non_engineering_title_tokens` | `excludes.title_tokens` of this pack |
| `career_snapshot.yaml` | `taxa[].tokens` generated by the existing `jsctl snapshot-career` into the pack (the snapshot tool becomes a pack builder) |
| 300 PDF slots | `renderer/vault_slots` adapter bound to pack `software-engineering` (operator only) |
| Overlay file | the operator's `Constraint[]` rows, imported once from `CAREER_PRIVATE_DIR` (importer keeps the allow-list, FR-055) |

Acceptance for the migration: the existing gold set (FR-023) and `ruleset_sha`-pinned tests give identical mappings and scores before/after. The pack is publishable (it contains skill tokens only, no personal values: the existing "no personal data in git" gate, FR-054, still applies).

## 9. Data model and migration sketch

Next migration numbers on `github/main`: 0001..0004 exist (`internal/store/migrations`). Proposed (numbers to be re-checked at implementation time):

```sql
-- 0005_tenancy_core.sql  (see 03: tenant_id column on every owned table)
CREATE TABLE tenant (id TEXT PRIMARY KEY, slug TEXT UNIQUE, name TEXT, edition TEXT, created_at TEXT, status TEXT);
CREATE TABLE person (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL REFERENCES tenant(id), user_id TEXT NULL,
                     display_name TEXT, home_country TEXT, doc_locale TEXT, created_at TEXT);

-- 0006_profile_facts.sql
CREATE TABLE profile (id TEXT PRIMARY KEY, person_id TEXT NOT NULL REFERENCES person(id), version INT NOT NULL,
                      json_resume_enc BLOB /* E: envelope-encrypted, per-tenant KEK (03) */, imported_from TEXT,
                      created_at TEXT, UNIQUE(person_id, version));
CREATE TABLE fact (id TEXT PRIMARY KEY, person_id TEXT NOT NULL REFERENCES person(id), kind TEXT NOT NULL,
                   statement_enc BLOB NOT NULL /* E */, status TEXT CHECK(status IN ('proposed','asserted','retired')),
                   asserted_at TEXT, asserted_by TEXT, supersedes TEXT NULL REFERENCES fact(id));
CREATE TABLE fact_evidence (fact_id TEXT REFERENCES fact(id), kind TEXT, ref_enc BLOB, sha256 TEXT);
CREATE TABLE document_fact (document_id TEXT, fact_id TEXT, line_no INT, PRIMARY KEY(document_id, fact_id, line_no));

-- 0007_packs.sql
CREATE TABLE pack (id TEXT, tenant_id TEXT NULL /* NULL = built-in */, kind TEXT CHECK(kind IN ('occupation','template','locale','provider')),
                   version TEXT, sha256 TEXT, manifest_json TEXT, enabled INT, PRIMARY KEY(id, version, tenant_id));
CREATE TABLE search_focus (id TEXT PRIMARY KEY, person_id TEXT, pack_id TEXT NULL, taxon_id TEXT NULL,
                           free_text TEXT NULL, weight REAL, created_at TEXT);
ALTER TABLE mapping ADD COLUMN pack_id TEXT;          -- nullable until backfilled
CREATE TABLE constraint_private (id TEXT PRIMARY KEY, person_id TEXT, key TEXT, value_enc BLOB NOT NULL /* E */);
```

Backfill for the existing operator database: create tenant `default`, person `operator`, pack `software-engineering@1.0.0`; set `mapping.pack_id` from the pack; keep the overlay file reader working as an importer. Rollback follows the existing backup-before-migrate rule (`schema_migration.backup_ref`).

Invariant added to `data-model.md` when specified: no document line without a fact (gate); `fact.status='asserted'` requires `asserted_by = person.user_id`.

## 10. Interfaces (Go)

```go
// internal/pack: loads and validates packs; pure; no network.
type Taxonomy interface {
	Taxa(ctx context.Context, packID string) ([]Taxon, error)
	Map(ctx context.Context, p position.Merged, packs []string) ([]Mapping, error)
}
// internal/profile
type Ledger interface {
	Assert(ctx context.Context, factID, byUser string) error      // human act, audited
	Select(ctx context.Context, personID string, sel Selector) ([]Fact, error)
}
// Selector decides WHICH asserted facts appear for a Position; deterministic; same inputs, same output (FR-022 style).
type Selector interface { Select(facts []Fact, pos position.Merged, focus SearchFocus) []Fact }
```

All three are registered through `internal/ext` (kinds `taxonomy`, `selector`, `renderer`) so they inherit three-valued verdicts, `policy.yaml` enablement and the conformance suite (`docs/research/protocols/21`, `25`).

## 11. Open questions

| # | Question | Needed by |
|---|---|---|
| Q2-1 | Do we adopt JSON Resume as the canonical stored form, or keep an internal schema and treat JSON Resume/Europass as import/export only? (Author leans: internal schema + both as adapters, to avoid inheriting upstream schema churn.) | Phase P1 |
| Q2-2 | Which ISCO reuse terms apply (UNCONFIRMED)? Until confirmed, store codes only. | P1 |
| Q2-3 | Is machine translation of Facts allowed in v1 of the generic product (flagged as machine-translated), or is it person-written only? | P2 |
| Q2-4 | First non-software pack to prove genericity (candidates: healthcare nursing, finance accounting, skilled trades). Needs a gold set per pack: who labels? | P1 |
