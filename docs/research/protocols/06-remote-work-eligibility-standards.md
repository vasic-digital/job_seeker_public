# 06 - Remote-work eligibility standards

Index: [00-README-index.md](00-README-index.md). Retrieval date **2026-11-24**. Tags: `[P]` primary fetched, `[S]` secondary, `[K]` standard cited from background knowledge, not re-fetched (edition UNCONFIRMED).

## Contents

1. [The eligibility problem](#1-the-eligibility-problem)
2. [Code standards](#2-code-standards)
3. [Timezones and overlap](#3-timezones-and-overlap)
4. [Salary normalisation](#4-salary-normalisation)
5. [Work authorisation, visas, EOR, contractor vs employee, tax residency](#5-work-authorisation-visas-eor-contractor-vs-employee-tax-residency)
6. [Remote taxonomy](#6-remote-taxonomy)
7. [Integration for job_seeker](#7-integration-for-job_seeker)
8. [Priority table](#8-priority-table)

## 1. The eligibility problem

"Remote" on a posting does not mean "I may take this job from where I live." The user lives in a specific country/timezone (the operator's profile is in the private overlay; the existing matcher handles `salary` and `tz` "with overlay present or absent" and `eligible_countries` keys, `JSK-T050/T054`). A good eligibility decision needs four independent facts: **where the work may be performed** (countries/regions), **in which hours** (timezone overlap), **under what contract** (employee, contractor, via EOR), **with what right to work**. Sources encode these facts inconsistently; this file standardises the vocabulary.

Existing structured signals in sources we already read or could read:

| Source | Fields (verified) |
|---|---|
| Himalayas API `[P]` | `locationRestrictions` (countries; empty = none), `timezoneRestrictions` (UTC offsets; full list or empty = none), `minSalary/maxSalary/salaryPeriod/currency`, `employmentType` incl. Contractor |
| Lever `[P]` | `workplaceType` (`unspecified|on-site|remote|hybrid`), `country` (job's country, not an applicant limit), `salaryRange{currency,interval,min,max}` |
| Ashby `[P]` | `workplaceType` (`OnSite|Remote|Hybrid`), `isRemote` true for Remote **or** Hybrid, `employmentType`, `address.postalAddress.addressCountry` |
| Greenhouse `[P]` | `location`, offices, `pay_input_ranges{min_cents,max_cents,currency_type}` |
| Pinpoint `[P]` | `workplace_type`, `compensation_minimum/maximum/currency/frequency` |
| schema.org / Google `[P]` | `jobLocationType=TELECOMMUTE`, `applicantLocationRequirements` (country-level), `baseSalary` |

## 2. Code standards

| Standard | Use | Authoritative |
|---|---|---|
| ISO 3166-1 alpha-2 (+ alpha-3, numeric) | countries; Lever `country` and JSON Resume use alpha-2 `[P]` | iso.org / OBP `[K]`; note: `XK` (Kosovo) is user-assigned, not in the standard `[K]` |
| ISO 3166-2 | subdivisions (US states, DE Laender) | `[K]` |
| ISO 4217 | currencies; Pinpoint, Lever, Himalayas carry codes | `[K]` (amendment numbers UNCONFIRMED) |
| ISO 639-1/-2/-3 and **BCP 47 (RFC 5646/4647)** | languages; use BCP 47 tags (`sr-Latn`, `sr-Cyrl`, `ru`, `de-DE`) - JSON Resume `meta.language` is BCP 47 `[P]` | `[K]` |
| ISO 8601 / RFC 3339 | dates and timestamps; Google `validThrough` is ISO 8601 `[P]` | `[K]` |
| ISO 15924 | script subtags (`Latn`, `Cyrl`) for Serbian | `[K]` |
| UN M49 | region groups ("EMEA", "Europe") | `[K]` |
| **IANA Time Zone Database** (tzdata) | named zones (`Europe/Belgrade`), DST rules; Go `time.LoadLocation` and `time/tzdata` embed | iana.org/time-zones `[K]`, release year-letter UNCONFIRMED |
| ISO 4217 minor units | rounding for salary | `[K]` |

Countries named in prose ("EMEA", "Europe-based", "EU only", "Schengen", "CIS") must resolve to explicit sets: maintain `rules/regions.yaml` mapping labels to alpha-2 sets **with the date of the mapping** (EU membership, EEA, Schengen membership change) and a test that fails on unknown labels (unknown -> `UNKNOWN`, never guessed).

## 3. Timezones and overlap

- Represent eligibility as a **UTC-offset window set** plus named-zone list; offsets move with DST, so compute overlap per date using IANA zones, not fixed offsets. Himalayas gives raw UTC offsets (`timezoneRestrictions`) `[P]`: convert user zone to its offset on the posting date and test membership.
- Typical posting prose: "overlap with US Eastern 4h", "+/- 3h of CET", "CET to EET". Parser grammar (deterministic, regex + table; no LLM for the gate) yields `TZRequirement{Anchor IANAZone; MaxDeltaHours float; MinOverlapHours float}`. Anything unparsed stays `TZHint` text (existing field) with `tz_unparsed=true` for human review.
- Overlap definition: intersection of the user's declared working hours and the employer's anchor-zone core hours (default 09:00-17:00 local to anchor, configurable). Output `overlap_hours` for scoring (existing `Score` has a tz gate).

## 4. Salary normalisation

- Canonical: `(min, max, currency ISO4217, period)` with period in `{hour, day, week, month, year}` mirroring Google `unitText` `HOUR, DAY, WEEK, MONTH, YEAR` `[P]` and Pinpoint/Lever/Himalayas variants (`annual`, `interval`). Convert to **annual gross in the posting currency** only with a stated hours model: hourly x 2080 (US convention) or user-configured; monthly x 12 (some EU countries pay 13/14 salaries: **do not assume**; flag `months_per_year_unknown`).
- **Currency conversion** is for display and filtering only, using a dated rate snapshot (ECB euro reference rates are the usual free source `[K]`, licence/terms UNCONFIRMED); store `rate_date` with each conversion. Never mix gross/net; many postings do not say: `gross_net=unknown` is a first-class value.
- Contractor day/hourly rates are not comparable to employee salary without costs (tax, social security, unpaid leave): present both numbers, do not auto-convert for ranking, unless the user supplies a factor.
- Estimates: schema.org separates `estimatedSalary` (third-party estimate) from `baseSalary` (employer) `[P]`; Google requires real employer-provided `baseSalary`. **Never store an estimate in the same column as a posted salary.**

## 5. Work authorisation, visas, EOR, contractor vs employee, tax residency

Vocabulary a parser and the drafter must recognise (EN; RU/SR equivalents to be collected from real postings, none measured yet: UNCONFIRMED):

| Term | Meaning | Matching consequence |
|---|---|---|
| "must be authorised to work in <country>" | candidate holds a right to work there | hard gate unless the user holds it |
| "visa sponsorship available / not available" | employer will/won't sponsor relocation | informational for remote-in-country roles |
| "EOR" / "Employer of Record" (Deel, Remote, Oyster, Papaya; names `[K]`) | a third party legally employs the worker in the worker's country | **expands** eligibility to countries covered by the EOR; postings often say "we hire via EOR in 90+ countries" (UNCONFIRMED per vendor) |
| "contractor" / "B2B" / "independent contractor" / "1099" / "freelance" | no employment relationship | requires the user to have a registered entity/status (e.g. sole proprietor); tax and invoicing duties on the user |
| "employee" / "W-2" / "permanent" | local employment contract in the employer's entity | needs the employer entity in the user's country or an EOR |
| "work from anywhere", "worldwide" | no country limit stated | still check export-control/sanctions and timezone; sanctions regimes can bar hiring from specific countries irrespective of the posting text (UNCONFIRMED applicability to the user's country; operator-level legal check) |
| "tax residency" / "permanent establishment" | where income is taxed / employer creates a taxable presence | employers sometimes restrict remote hiring to countries where they have entities to avoid PE risk: a *reason behind* country lists, not something we compute |

Rules for job_seeker: (1) the user's own facts (citizenship/residence/right-to-work/entity type) live **only in the private overlay**, never in tracked files; (2) the matcher outputs a three-valued `eligible: yes|no|unknown` with the specific blocking clause quoted; `unknown` is shown to the human, never silently treated as yes; (3) the drafter must **never claim** a work authorisation the user lacks and answers authorisation questions only from overlay data, otherwise leaves them for the human.

## 6. Remote taxonomy

Normalised enum `RemoteMode` (extends the existing field):

`onsite | hybrid | remote_country | remote_region | remote_timezone | remote_global | remote_unspecified | unknown`

plus orthogonal flags `async_first`, `travel_required_pct`, `office_days_per_week`. Mappings: Google `TELECOMMUTE` -> `remote_*` (country list present -> `remote_country`/`remote_region`); Lever `remote` -> `remote_unspecified` until restrictions found; Ashby `Hybrid` -> `hybrid` (do **not** use `isRemote`); Himalayas empty restrictions -> `remote_global`, non-empty `locationRestrictions` -> `remote_country`, non-empty partial `timezoneRestrictions` -> `remote_timezone`. Google's note that occasional/negotiable remote does not qualify as TELECOMMUTE `[P]` is the model for the hybrid boundary.

"Async" and "timezone overlap" have no standard; capture the phrase and parsed numbers, never infer.

## 7. Integration for job_seeker

Package `internal/eligibility`:

```go
type Requirement struct {
    Mode        RemoteMode
    Countries   []string          // ISO 3166-1 alpha-2, explicit
    Regions     []string          // labels resolved via rules/regions.yaml with mapping date
    TZ          *TZRequirement
    Contract    []ContractKind    // employee, contractor, eor_ok
    WorkAuth    []string          // quoted clauses, e.g. "authorised to work in DE"
    Sponsorship Tri               // yes/no/unknown
    Evidence    []Span            // byte spans in Body / JSON path of structured field
}
type Profile struct{ Countries, Zones []string; WorkAuth []string; EntityTypes []ContractKind } // from private overlay only
type Verdict struct{ Eligible Tri; Blocking []string; Overlap float64 }
func Extract(p providers.Posting, structured map[string]any) Requirement   // structured first, prose second
func Decide(r Requirement, prof Profile, on time.Time) Verdict
```

Order of evidence: structured field (Himalayas, Google JSON-LD, Lever) > ATS metadata > prose rules > unknown. Prose rules are a versioned YAML (`rules/eligibility.yaml`) in EN/RU/SR with each pattern traceable to a real posting in the private corpus.

Test plan: table tests per mapping above; property test "unknown never becomes yes"; DST boundary tests with `Europe/Belgrade` vs `America/New_York`; salary normalisation round-trip tests including 13th-salary flag; measurement task: label eligibility on the existing 115-posting real corpus (two independent labellers, as `JSK-T057`) and report precision/recall before enabling as a hard gate - until measured it stays advisory. Gate registry entry with `--prove-failure`.

**Risks:** hallucinated eligibility from prose; stale region mappings; sanctions/legal assumptions; mixing job country with applicant country (Lever `country`).

## 8. Priority table

| Item | Priority |
|---|---|
| ISO 3166-1 / 4217 / BCP 47 / ISO 8601 normalisation, IANA tz | MUST |
| `RemoteMode` enum + three-valued eligibility verdict | MUST |
| Himalayas/Google/Lever/Ashby structured mappings | MUST |
| Prose eligibility parser EN, then RU/SR | SHOULD (needs real RU/SR postings) |
| Salary normaliser with gross/net and 13th-salary flags | SHOULD |
| EOR/contractor/work-auth vocabulary extraction | SHOULD |
| Sanctions/export-control checks | MAY (operator legal call) |
