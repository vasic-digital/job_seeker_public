# 01 - Vision, personas and market

| | |
|---|---|
| Part | Product research, 1 of 9 |
| Date | 2026-11-24 (all web facts retrieved 2026-11-24) |
| Status | PROPOSAL. Docs only. |
| Evidence tags | **[P]** vendor's own page fetched; **[S]** search snippet or third-party page (weak); **UNCONFIRMED** not verified. Nothing here is legal advice. |

## Contents

1. [Vision](#1-vision)
2. [Personas](#2-personas)
3. [Competitive landscape](#3-competitive-landscape)
4. [What job_seeker uniquely offers](#4-what-job_seeker-uniquely-offers)
5. [Positioning and what we will not do](#5-positioning)
6. [Market-evidence gaps](#6-gaps)

## 1. Vision

Directive of 2026-11-24: job_seeker must become a full product that can be sold and advertised, fully configurable, generic and universal, usable by any job-seeking profile, or even companies. Today it is a single-operator, LAN-only tool (spec D1: "Single operator. Private tool.").

One-line product: **a job-search workbench that finds postings across providers, deduplicates them into one canonical Position, drafts truthful documents from the person's own verified facts, and never sends anything without a human decision.**

The chosen differentiation is trust (truthfulness, approval, privacy), not volume. Section 3 reports that volume-oriented auto-apply is the part of the category with documented backlash.

## 2. Personas

| # | Persona | Job to be done | What they pay for | Mode / edition (see [07](07-business-model-pricing-and-go-to-market.md)) | Regulatory note ([06](06-compliance-privacy-and-ai-regulation.md)) |
|---|---|---|---|---|---|
| P1 | Individual job seeker, any profession, seniority and country (incl. career changers, graduates, returners) | Find relevant postings across many sources once, tailor truthful documents, track applications and follow-ups | Time saved, fewer duplicate/spammy applications, privacy | Free / Pro, SaaS or self-host | Candidate-side use. Controller role likely ours (counsel). |
| P2 | Self-hosting technical seeker (the current operator is this persona) | Same, on own hardware, no data leaves | Control, source access | Self-host free / Pro licence | None beyond own data. |
| P3 | Career coach (independent) | Run 5-50 clients; propose edits; see pipelines; branded output | Per-client seats, white-label, client consent flows | Coach | Coach likely controller or joint controller for client data; client asserts facts (02 section 5). |
| P4 | Recruitment / outplacement agency | Support laid-off cohorts at scale; reporting to the sponsoring employer | Seats, SSO, reporting, SLAs | Agency | Outplacement = candidate-side support, but employer reporting needs care (aggregate only). |
| P5 | University / bootcamp career centre | Alumni and student cohorts; employer-independent | Per-seat or per-institution licence, accessibility | Agency/Enterprise | Student data, minors (06 section 8), accessibility (EAA, WCAG). |
| P6 | Company, internal mobility / outplacement | Help own employees find internal roles or support exits | Enterprise licence, SSO, data residency | Enterprise | Employer is controller; employee-ranking features are high-risk territory (06). |
| P7 | Company, sourcing (screening/ranking candidates) | Find and shortlist candidates | Not in scope without legal gating | Enterprise, gated | EU AI Act Annex III point 4 (employment) is the relevant high-risk category per the author's knowledge, UNCONFIRMED against the primary text. Counsel decision before any build (06 section 3, 09 risk R1). |

P1 and P2 are served first. P3 is the first B2B2C step because coaches multiply seats without needing enterprise sales. P7 is deliberately last and behind a gate.

## 3. Competitive landscape

Pricing from vendor pages where fetched. Monthly list prices run from about EUR 10 to USD 50 (author's reading of the table below); weekly and short plans are common at Teal, Simplify and Careerflow, which fits users leaving once hired. That inference is not a published churn figure; no job-seeker churn data was found (UNCONFIRMED).

| Vendor | Positioning | Free tier | Paid (as found) | Auto-apply | Coach/B2B offer | Source |
|---|---|---|---|---|---|---|
| Teal | Tracker + resume tool | Yes [S] | Weekly $13, monthly $29, quarterly $79 [S]; pricing page returned 403, UNCONFIRMED | UNCONFIRMED | UNCONFIRMED | https://help.tealhq.com/en/articles/9530153-teal-vs-teal ; https://toolradar.com/tools/teal-hq/pricing |
| Huntr | Tracker + AI resume tools | $0: unlimited base resumes, tracker to 100 jobs, autofill, 2 tailored resumes [P] | Pro $40/mo, $30/mo quarterly, $26.66/mo biannual [P] | No [P] | Lists organisations, bootcamps, universities, coaches, outplacement; prices not shown [P] | https://huntr.co/pricing |
| Simplify | Autofill extension, Simplify+, Autopilot | Autofill free [S] | Simplify+ weekly $19.99, monthly $39.99 [S]; page 404 | Yes, credit-metered (20/week) [S] | UNCONFIRMED | https://help.simplify.jobs/articles/5623502-whats-included-in-simplify-features-and-pricing |
| Jobscan | ATS resume scanner | ~5 scans/month [S] | Monthly $49.95, quarterly $89.95 [S] | No | UNCONFIRMED | https://blog.theinterviewguys.com/?p=10428 |
| LoopCV | Auto-apply across boards | EUR 0 with daily cap [P] | "from EUR 9.99/mo", per-plan prices not shown [P] | Yes (30+ boards) [P] | Agencies, coaches, universities, bootcamps, white-label, API listed; prices not shown [P] | https://loopcv.pro/pricing |
| Sonara | Auto-apply | UNCONFIRMED | UNCONFIRMED | Yes | UNCONFIRMED. Reported shutdown 2024-02-01 and later relaunch by another company [S]; the premise "now Jobcase" was not found and is UNCONFIRMED | https://blog.loopcv.pro/what-happened-to-sonara/ |
| AIApply | Auto-apply, resume, cover, interview | Limited free tier [P] | Premium + credit packs, prices not shown [P] | Yes (reviewable drafts) [P] | White label, career coaches, affiliate listed [P] | https://aiapply.co/pricing |
| Careerflow | LinkedIn + resume AI + tracker | $0 with extension [P] | Premium $8.99/wk, $23.99/mo, $54.99/qtr, $172.99/yr; Premium Plus $19.99/wk ... $299.99/yr [P] | Not stated | Universities, coaches, bootcamps, outplacement listed [P] | https://www.careerflow.ai |
| Kickresume | Resume + cover builder | 4 templates; students free [P] | EUR 10.32/mo, 23.22/qtr, 41.28/yr first year [P] | No | No business plan on the page [P] | https://www.kickresume.com/en/pricing/ |

Category facts:

* Auto-apply backlash is reported: duplicate applications flagged as spam, stricter ATS filters, trap questions (https://www.nbcnews.com/tech/innovation/ai-making-job-applications-easier-creating-another-problem-rcna179683, [S] for the claims about employer response).
* Coach/university/outplacement plans exist at four of eight vendors but no B2B price was published anywhere found; B2B price discovery is an open task (UNCONFIRMED benchmarks, see [07](07-business-model-pricing-and-go-to-market.md)).
* No privacy or fabrication findings were located for Simplify, Teal, Jobscan, Huntr, Careerflow, Kickresume. That is absence of evidence, not clearance.

## 4. What job_seeker uniquely offers

Each item is a capability that exists in the repository or its specs today, so the claim is checkable, and is compared against what the table above shows vendors state publicly. "Competitor lacks X" is never asserted; at most "not stated on the pages read".

| Differentiator | Evidence in this repo | Competitor stance as read |
|---|---|---|
| Truthfulness / no fabrication | Umbrella rule that nothing in the profile may be invented; product design Fact ledger (02 section 5); FR-029 templates-only drafts (D8) | No competitor page read states a sentence-level provenance guarantee: UNCONFIRMED that none exists |
| Human approval, dry-run default, no unattended submit | D4; FR-025..FR-027; `sent` needs 8 conditions (FR-026) | Simplify Autopilot and LoopCV/AIApply market auto-apply [P/S] |
| Privacy-first | Envelope encryption of sensitive columns (FR-056), no personal data in git (FR-054), egress allow-list (FR-009), retention (C9), export/erase (FR-058) | Not compared; vendor privacy pages not read |
| ATS-safe documents | ATS research and profiles (`docs/research/protocols/02`, `10-13`), `ats_profile` strategy (22 section 4) | Jobscan/Careerflow/Kickresume sell ATS checking [P/S]; ours is a build-time property of the renderer, not only a checker |
| Multi-provider Position dedup | Position/Listing model, resolver, FR-079..FR-094 (`docs/research/providers/03-05`) | Not stated on pages read |
| 15 languages (console) and RTL | D10, FR-068..FR-070 | Kickresume/others multilingual support not compared: UNCONFIRMED |
| Self-host option | Go single binary, SQLite, rootless containers (FR-072) | None of the eight pages read mention self-hosting |
| Extensible policy-as-data kernel | `internal/ext`, `rules/policy.yaml` (22) | n/a |

## 5. Positioning

* **Promise:** "Every line is true, every send is yours." Aimed at people tired of spray-and-pray tools.
* **Not doing:** unattended mass applying; scraping platforms whose terms forbid it ([05](05-commercial-licensing-and-third-party-terms.md)); candidate ranking for employers without legal gating ([06](06-compliance-privacy-and-ai-regulation.md)); emotion inference.
* **Wedge markets:** (a) privacy-conscious technical seekers (self-host), (b) coaches and outplacement who need client-safe, branded tooling, (c) EU users where privacy and AI Act literacy are buying criteria (UNCONFIRMED as a buying criterion; to be tested in interviews).

## 6. Gaps

Teal/Simplify/Jobscan official pricing pages, all B2B prices, job-seeker churn, university per-seat prices (only unverified third-party claims were found), Sonara's current status. These gate the pricing hypotheses in 07; they are research tasks in 09, not assumptions.
