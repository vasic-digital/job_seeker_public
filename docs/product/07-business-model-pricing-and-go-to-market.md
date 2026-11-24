# 07 - Business model, pricing and go-to-market

| | |
|---|---|
| Part | Product research, 7 of 9 |
| Date | 2026-11-24 (web facts retrieved 2026-11-24) |
| Status | PROPOSAL / HYPOTHESES. Every price below is a hypothesis to be tested, not a decision. |
| Inputs | [01](01-vision-personas-and-market.md) (competitor prices), [05](05-commercial-licensing-and-third-party-terms.md) (cost of data sources), [06](06-compliance-privacy-and-ai-regulation.md) (what is gated) |

## Contents

1. [Editions](#1-editions)
2. [Pricing hypotheses and reasoning](#2-pricing-hypotheses)
3. [Unit-economics inputs](#3-unit-economics-inputs)
4. [Billing](#4-billing)
5. [Landing site, docs, onboarding](#5-landing-site-docs-onboarding)
6. [Referral, affiliate, advertising](#6-referral-affiliate-advertising)
7. [Metrics](#7-metrics)
8. [Open questions](#8-open-questions)

## 1. Editions

| Edition | Persona | Deployment | Includes | Gated / excluded |
|---|---|---|---|---|
| **Free** | P1, P2 | SaaS (limited) or self-host | 1 person; 3 sources (permissive ones, 05); 1 pack; tracker; template documents; export/delete; 15-language console; community support | caps on active searches and documents; no white-label; no API |
| **Pro** | P1, P2 | SaaS or self-host licence | Free + all permitted sources; multiple packs and search focuses; Fact ledger tooling; ATS checks; document packs and country formats; bring-your-own provider keys; API token (read) | single person |
| **Coach** | P3 | SaaS (self-host allowed) | Pro + up to N client workspaces; invitations and consent grants; coach proposals; branded exports; client progress view | no SSO; no org hierarchy |
| **Agency** | P4, P5 | SaaS or dedicated | Coach + multiple coaches, cohort workspaces, white-label (domain, tokens), reporting (aggregate), webhooks, SSO (OIDC), audit export, bulk import | no person-ranking for employers |
| **Enterprise** | P5, P6 | dedicated SaaS or on-prem | Agency + SAML/SCIM, BYOK, regional pinning, DPA/SLA, dedicated support, custom packs, marketplace allow-listing, pen-test report access | sourcing/candidate ranking only if a separate legal programme is approved (06 section 4) |

Entitlements are data (`plan_entitlement`, 03 section 7), so editions and promotions change without code releases. The same binary serves every edition (04).

## 2. Pricing hypotheses

Anchors (vendor pages, 2026-11-24, see 01): Huntr Pro $40/mo ($30/mo quarterly, $26.66 biannual); Careerflow Premium $23.99/mo ($172.99/yr), Plus $44.99/mo; Kickresume EUR 10.32 for the first month of the monthly plan (separate yearly plan EUR 41.28 for the first year); Simplify+ $39.99/mo and Teal $29/mo ([S]); Jobscan $49.95/mo ([S]); LoopCV "from EUR 9.99/mo". Weekly plans exist at Teal, Simplify and Careerflow. No B2B (coach/university/outplacement) price was published by any vendor read.

Reasoning: job seekers use the product for a bounded period and leave when hired, so pricing should favour short commitments and make the first useful result free; our differentiation is trust, not volume, so we should not compete at the cheapest end.

| Edition | Hypothesis (USD or EUR, before VAT) | Basis | Test |
|---|---|---|---|
| Free | 0 | all eight vendors read have a free or limited tier (some UNCONFIRMED) | measure activation and conversion |
| Pro | **15/month, 39/quarter, 99/year**; weekly pass 9 | below Huntr/Simplify/Jobscan monthly, above Kickresume; quarter matches typical search length (author's assumption, UNCONFIRMED) | price-page A/B with 3 variants once traffic exists; willingness-to-pay interviews with 20 seekers |
| Coach | **49/month incl. 10 active clients; +4 per extra client** | no benchmark found; set so a coach with 10 clients pays under 5 per client; hypothesis only | interview 10 coaches |
| Agency | **from 249/month (5 coaches, 100 persons) + per-person overage** | no public benchmark; university tooling is quote-only (unverified third-party claims only) | quote-based pilots |
| Enterprise | quote; floor 25,000/year | placeholder for sales discipline, not a measured figure | pilots; do not publish a price list |
| Self-host licence | Free for 1 person; Pro key 99/year | mirrors Pro; avoids undercutting SaaS | same as Pro |

Expected conversion (context only, from web summaries): median freemium conversion about 8% across 200 products in a Jan 2026 report; 3-5% "good", 8-12% "great" per that summary (https://chartmogul.com/reports/saas-conversion-report/ , [S]); another source cites 3.7% for traditional freemium and 2.6% in EdTech (https://www.crazyegg.com/blog/free-to-paid-conversion-rate/ , agency data [S]). Job-seeker churn data: **not found, UNCONFIRMED**. Plan on 3% until measured.

## 3. Unit-economics inputs

| Input | Status |
|---|---|
| Hosting per tenant | UNKNOWN (SQLite per tenant is cheap; cost model needs a benchmark, 04) |
| Provider licences (Adzuna etc.) | UNCONFIRMED prices; BYO-key shifts cost to the customer (05) |
| LLM cost | Today no hosted generative drafting (D8). If optional assisted wording is added, metered and priced as credits (UNKNOWN) |
| Payment fees | Section 4 |
| Support | Free self-host community-only; Pro email; Agency/Enterprise named contact |
| Compliance overhead | pen test, counsel, insurance, SOC 2 (06): fixed costs, amounts UNKNOWN |

## 4. Billing

Facts from the vendors' pages (retrieved 2026-11-24):

| Provider | Fees | Merchant of record / EU VAT | Source |
|---|---|---|---|
| Stripe Payments | 2.9% + 30c domestic; +1.5% international cards; +1% if currency conversion | Stripe is not the seller; you remain responsible for tax | https://stripe.com/pricing |
| Stripe Tax | 0.5% per transaction (no-code) or $0.50 (API) where registered | calculates and collects; filing not shown as included | https://stripe.com/pricing |
| Stripe Managed Payments | 3.5% per transaction on top of Payments fees | described as merchant of record; who files EU VAT not stated in the excerpt | https://stripe.com/pricing |
| Paddle | 5% + 50c per Checkout transaction | merchant of record: collects, files and pays sales tax and VAT | https://www.paddle.com/pricing |
| Lemon Squeezy | 5% + 50c | MoR, but the page says it no longer accepts new sign-ups and will shut down on 2027-03-31 | https://www.lemonsqueezy.com/pricing |

Recommendation for decision (D-BIZ-2): **Paddle (merchant of record) for the launch** because it removes EU VAT registration and filing for a small company; revisit Stripe + Stripe Tax or Managed Payments when volume justifies a lower rate and a finance function. **Do not use Lemon Squeezy** (announced shutdown). MoR also changes the legal seller of record on invoices and refund handling: counsel and accountant to confirm for the selling entity (UNCONFIRMED). Enterprise: invoice and bank transfer. Self-host licence keys: signed offline-verifiable files.

Subscription UX obligations (one-click cancel, renewal notice, withdrawal handling) are in 06 section 7.

## 5. Landing site, docs, onboarding

* **Landing site** on the umbrella's design system / OpenDesign (the umbrella mandates a brand contract, token source, component library and screen catalogue for UI-shipping products, §11.4.216-§11.4.219; whether the product site is a separate repository is a decision). Pages: value proposition ("every line is true, every send is yours"), edition and price page, security & privacy page (links to the evidence: gates, SBOM), provider attribution page, comparison pages (factual, cited), changelog, status. Localised in the 15 console languages through the project's mandatory translation pipeline.
* **Docs site**: quickstart, self-host guide, packs authoring guide, API reference (OpenAPI), admin guide, compliance pack (DPA template, sub-processors, security overview), plugin author guide. Generated from the repository; same design system.
* **Onboarding**: first-run wizard (profile import from JSON Resume/Europass/CV PDF text, fact review, pack pick, source pick); the first useful result (scored shortlist from permitted sources) within 10 minutes is the activation target (hypothesis). Coaches: invite flow with consent explanation.
* **Support**: public knowledge base, issue tracker for self-host, email for paid.

## 6. Referral, affiliate, advertising

| Channel | Plan | Caveat |
|---|---|---|
| Referral | give one month, get one month; Coach referral credits | fraud limits; no cash for individuals initially |
| Affiliate | 20-30% first-year revenue to coaches/career creators (hypothesis; competitor programmes exist, AIApply lists affiliate, 01) | disclosure rules (FTC endorsement guides, EU UCPD) UNCONFIRMED, counsel |
| Content / SEO | factual comparison pages, "ATS-safe CV" guides per country, provider-by-provider job-board guides | compliant with provider attribution rules (05) |
| Communities | self-host and privacy communities, coaching communities | honest disclosure of authorship |
| Paid ads | search and professional-social ads in test budgets only after the activation funnel works | ad platform policies on employment ads and automated job applications UNCONFIRMED: read before spending |
| B2B | direct outreach to coaches, outplacement firms, bootcamps; pilots with 3 institutions | sales cycle UNKNOWN |

Advertising claims must be substantiated (no "X% more interviews" without our own measured, published method; one competitor-quoted "80% more likely to get hired" claim could not be verified, 01). Our own metric claims need an evidence record, consistent with the project's claim-vs-reality rule (§11.4.266).

## 7. Metrics

| Layer | Metrics |
|---|---|
| Acquisition | visits, signup rate, source mix |
| Activation | % reaching first shortlist, first asserted Fact set, first approved application (not "sent": a handoff recorded) |
| Engagement | weekly active searchers, applications approved per user, follow-ups done |
| Outcome (opt-in, user-reported) | interviews, offers, time-to-offer; only aggregate and only with consent (06) |
| Revenue | MRR, conversion, ARPU, churn by cohort, refund rate |
| Trust | Fact-ledger coverage of documents (target 100%), duplicate-application warnings acknowledged, DSAR time, support CSAT |
| Platform | SLO attainment (04), provider degradation minutes |
| B2B | seats, coaches per agency, client per coach, expansion |

Instrumentation is privacy-preserving and opt-in (04 section 5); no CV content in analytics.

## 8. Open questions

| # | Question |
|---|---|
| Q7-1 | Is revenue the goal at all in year 1, or reach and a hosted trial for self-hosters? |
| Q7-2 | Paddle vs Stripe at launch; legal entity and country of sale (affects tax, 06) |
| Q7-3 | Whether to publish prices for Coach/Agency or run quote-only |
| Q7-4 | Product name and domain (05 D-BRAND-1) before any landing page is published |
