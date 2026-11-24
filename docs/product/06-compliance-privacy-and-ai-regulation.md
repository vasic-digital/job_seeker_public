# 06 - Compliance, privacy and AI regulation

| | |
|---|---|
| Part | Product research, 6 of 9 |
| Date | 2026-11-24 (web facts retrieved 2026-11-24) |
| Status | PROPOSAL. **Everything here needs counsel. No statement is a legal conclusion.** |
| Evidence tags | **[V]** read in a source this session, mostly law-firm or press summaries, primary texts not opened; **[M]** recalled from model knowledge, **UNCONFIRMED**, canonical place named |

## Contents

1. [How selling changes the legal posture](#1-how-selling-changes-the-legal-posture)
2. [GDPR and UK GDPR](#2-gdpr-and-uk-gdpr)
3. [EU AI Act and the candidate-side versus company-side line](#3-eu-ai-act)
4. [Feature gating by mode](#4-feature-gating-by-mode)
5. [US privacy and AI-hiring laws](#5-us-privacy-and-ai-hiring-laws)
6. [Accessibility](#6-accessibility)
7. [Consumer protection and subscriptions](#7-consumer-protection)
8. [Children](#8-children)
9. [Legal documents](#9-legal-documents)
10. [Security certifications and pen-test plan](#10-security-certifications-and-pen-test-plan)
11. [Product obligations table](#11-product-obligations)
12. [Gaps and counsel questions](#12-gaps)

## 1. How selling changes the legal posture

Today: one operator, own data, private repository, no third parties. As a product the company becomes, depending on role, a controller or processor of other people's CVs, employment history, contact data and (for companies) data about candidates and employees. It also becomes a party to provider terms (05), a consumer-contract seller and, in company mode, potentially a provider of an AI system in a high-risk area. These are separate regimes; each needs a role decision per product line.

## 2. GDPR and UK GDPR

Source for GDPR text: https://eur-lex.europa.eu/eli/reg/2016/679/oj (not opened; all items below **[M], UNCONFIRMED** unless marked).

| Topic | Design consequence |
|---|---|
| Roles, Art. 4(7)/(8) | Individual user: we likely decide purposes and means, so probably controller. Coach/agency/company tenant: tenant is likely controller of its clients'/employees' data and we likely a processor; we are controller for our own account, billing and security logs. Counsel to confirm role per edition and per feature; joint controllership for coaches is plausible. |
| DPA, Art. 28(3) | A DPA template covering: documented instructions, confidentiality, security (Art. 32), sub-processor authorisation and flow-down, assistance with data-subject rights and Arts 32-36, delete/return at end, audit/information rights. Public sub-processor list (hosting, KMS, email, payments, any LLM provider). |
| Automated decisions, Art. 22 | Candidate-side: the person decides (human approval, FR-026). Company-side screening may engage Art. 22; human intervention must be real, not nominal. |
| Rights, Arts 15, 17, 20 | Export (exists: encrypted archive, FR-058), per-record and whole-account delete (exists: `internal/privacy`), portability in machine-readable form (JSON Resume/Europass export, 02). Add per-tenant SLA for DSAR handling and a DSAR queue in the admin UI. |
| DPIA, Art. 35 | Likely for large-scale profiling/evaluation of individuals: run one before launching company mode; counsel to confirm. |
| EU representative, Art. 27 | Needed when the controller/processor is non-EU, subject to the GDPR and not within the Art. 27(2) exceptions. If the selling entity is established outside the EU (entity location is undecided, D-BIZ-2), a representative is likely required for EU customers, UNCONFIRMED. |
| Transfers, Ch. V | EU-US Data Privacy Framework: General Court dismissed the Latombe challenge on 3 Sep 2025 (T-553/23); appeal filed 31 Oct 2025 (C-703/25 P); status after Jan 2026 **UNCONFIRMED** [V] (https://www.wilmerhale.com/en/insights/blogs/wilmerhale-privacy-and-cybersecurity-law-blog/20251201-european-court-of-justice-to-review-challenge-to-eu-us-data-privacy-framework ). Keep SCCs as fallback in the DPA. Adequacy status of the selling entity's country was not researched: UNCONFIRMED. |
| Data residency | EU region first (04); tenant-pinned region; backups in region. |
| Retention per tenant | Existing defaults (C9: postings 90 days, raw snapshots 14 days, logs 30 days) become tenant policy with floors/ceilings (08). |
| Erasure | Crypto-shredding of the tenant KEK (03 section 3) plus backup ageing window; whether this satisfies Art. 17 for backups is a counsel question. |
| Lawful basis | Contract for the individual; legitimate interest/contract-with-controller for tenants; consent for optional analytics and any LLM processing of CV text. Cookie/ePrivacy: server-side sessions only (`__Host-` cookie is strictly necessary); no third-party analytics by default, so a cookie banner may not be needed: counsel (UNCONFIRMED). |

UK: reforms of the Data (Use and Access) Act 2025 took effect 5 Feb 2026, replacing UK GDPR Art. 22 with Arts 22A-22D; ICO ADM guidance was still being drafted at the time of the sources [V] (https://www.gov.uk/guidance/data-use-and-access-act-2025-plans-for-commencement ; https://www.traverssmith.com/knowledge/knowledge-container/uks-data-protection-reforms-take-effect-a-new-era-for-automated-decision-making/ ). A UK representative/IDTA/Addendum for transfers is an open item.

## 3. EU AI Act

**The line that matters: candidate-side versus company-side.**

* Annex III point 4 (employment and workers management): recruitment/selection including filtering applications and evaluating candidates; decisions on promotion, termination, task allocation and monitoring **[M, UNCONFIRMED against the primary text]** (https://eur-lex.europa.eu/eli/reg/2024/1689/oj ). A tool used *by an employer or agency to filter, rank or evaluate candidates* is the textbook case; a tool used *by the candidate* to find postings and tailor their own documents is not obviously inside the text, **but counsel must classify each product line**, including coach tooling that ranks clients' fit to postings and any "which candidate fits this posting" feature.
* Dates: the Digital Omnibus on AI is reported as Regulation (EU) 2026/1744 of 8 July 2026, in force 27 July 2026, moving stand-alone Annex III high-risk obligations from 2 Aug 2026 to **2 Dec 2027** and Annex I embedded products to 2 Aug 2028 **[V, law-firm and EEAS summaries; OJ text not opened]** (https://www.lewissilkin.com/insights/2026/07/27/the-digital-omnibus-on-ai-enters-into-force-today-102nedo ; https://www.eeas.europa.eu/delegations/chile/artificial-intelligence-council-gives-final-green-light-simplify-and-streamline-rules_en ). Treat dates as moving until the OJ text is read.
* Art. 5(1)(f) bans emotion inference in workplace and education, applying since 2 Feb 2025 **[M]**: **no emotion/confidence/sentiment inference from voice or face in any interview-practice or assessment feature.** Hard product rule.
* Art. 4 AI literacy: after the Omnibus, providers and deployers must "take measures" supporting AI literacy; no level mandated **[V]**. Provide staff training records and customer guidance.
* Art. 50 transparency (disclose AI interaction, mark synthetic content), with a reported transparency-grace date of 2 Dec 2026 **[V, sources conflict: one law-firm summary lists 2027]**; who it binds (provider vs deployer) to be read from the text.
* The Omnibus reportedly adds a ban on certain harmful generative-AI practices; scope not seen: UNCONFIRMED (may or may not touch this product).

## 4. Feature gating by mode

Proposed hard separation by **mode**, enforced in code as edition-locked policy gates (policy layer "locked", 22; 03 section 8) and visible in the claim ledger (§11.4.266 style: every advertised capability has a gate):

| Feature | Candidate mode (individual, coach on behalf of own clients) | Company mode: internal mobility / outplacement | Company mode: sourcing |
|---|---|---|---|
| Search postings, track, draft documents from own Facts | ON | ON | n/a |
| Score a posting against *the user's own* profile | ON | ON (employee sees own result) | n/a |
| Employer sees which employees applied internally | n/a | OFF by default; aggregate-only reporting; individual visibility needs explicit employee consent and counsel review | n/a |
| Rank or filter **candidates** for an employer | **absent** | **absent** | **OFF; not built until counsel classifies high-risk obligations (conformity assessment, risk management, logging, human oversight, bias testing)** |
| Candidate data import from third-party sources about people who did not sign up | absent | absent | OFF |
| Emotion/affect inference | absent | absent | absent (Art. 5) |
| Any decision with legal or similarly significant effect without human decision | absent | absent | absent |

The same code that already refuses `sent` without a human approval (FR-026/027) is the template for "no ranking of people": a path enumeration gate that fails if any code path outputs a person-ranking list in a non-sourcing tenant. If the operator elects company sourcing later, it is a separate product line with its own compliance programme: a conformity process, bias audit (cf. NYC LL144 below) and EU database registration obligations to be learned from the Act text. **Recommendation: do not build company sourcing in the first three stages (09).**

## 5. US privacy and AI-hiring laws

| Law | Finding | Source / tag |
|---|---|---|
| CCPA/CPRA | Covers employee/applicant data since the HR exemption lapsed 1 Jan 2023 [M]; thresholds include revenue above $25M (inflation-adjusted, current figure UNCONFIRMED), 100,000+ consumers/households, or 50%+ revenue from selling/sharing; amended regulations effective 1 Jan 2026; ADMT rules for significant decisions apply from 1 Jan 2027 | https://www.cppa.ca.gov/announcements/2025/20250923.html [V] |
| NYC Local Law 144 | Annual independent bias audit and notices for automated employment decision tools used for hiring/promotion in NYC; enforcement gaps reported in a Dec 2025 state audit (secondary) | https://www.nelsonmullins.com/insights/blogs/ai-task-force/ai/states-take-aim-at-ai-in-employment-decisions-illinois-s-disclosure-and-anti-discrimination-requirements-enforcement-gaps-under-nyc-s-local-law-144-and-connecticut-s-new-ai-law [V secondary] |
| Illinois HB 3773 | Amends Human Rights Act, effective 1 Jan 2026; bars discriminatory AI in employment decisions, requires notice; no bias-audit mandate; rulemaking reported postponed (16 Jun 2026) | same Nelson Mullins alert [V secondary] |
| Colorado | SB24-205 reported repealed and replaced by SB 26-189, signed 14 May 2026, effective 1 Jan 2027 conditional on AG rulemaking; litigation pending, so date may move: UNCONFIRMED | https://www.gtlaw.com/en/insights/2026/5/colorado-repeals-and-replaces-the-colorado-ai-act [V secondary] |
| California CRD ADS regulations | Amend FEHA regulations, effective 1 Oct 2025; apply to employers' automated-decision systems; ADS data retention at least 4 years | https://www.paulhastings.com/thought-leadership/new-california-regulations-on-employers-use-of-ai-to-make-decisions-go-into-effect-oct-1-2025 [V secondary] |

All of these target employer-side decision tools; candidate-side mode is expected to be outside most of them, but employers using our product in company mode would become the regulated party and may ask for vendor evidence. State-by-state sales gating is a decision for counsel.

## 6. Accessibility

* WCAG 2.2 AA (W3C Recommendation, Oct 2023, https://www.w3.org/TR/WCAG22/ **[M]**) as the console's target. The existing design-system contract and per-language review gates are where an accessibility gate belongs (axe-type automated checks plus manual screen-reader test per release; the extent to which the current console passes is UNKNOWN: not measured here).
* European Accessibility Act (Directive 2019/882): reported to apply since 28 June 2025; microenterprise exemption for services (fewer than 10 staff and turnover/balance sheet at most EUR 2M; "and" versus "and/or" disputed in sources); whether it reaches this product in every member state UNCONFIRMED (https://www.ccpc.ie/business/enforcement/accessibility/european-accessibility-act-guidelines-for-microenterprises/ [V secondary]). Tenant branding must keep contrast compliant (03 section 9).
* Generated documents: tagged PDF/UA and semantic structure are also an ATS-safety property (02 section 6); an accessibility check on rendered documents is proposed.

## 7. Consumer protection

| Topic | Finding | Tag |
|---|---|---|
| EU withdrawal right for digital services | 14-day withdrawal with loss-of-right on express consent to immediate performance (Directive 2011/83/EU, https://eur-lex.europa.eu/eli/dir/2011/83/oj) | [M] |
| EU "withdrawal button" | Directive 2023/2673 adds Art. 11a to the CRD from 19 Jun 2026; scope disputed (all distance contracts or mainly financial); transposition uneven | [V secondary] https://www.hoganlovells.com/en/publications/eu-consumer-protection-law-update-new-mandatory-withdrawal-button-what-online-traders-need-to-know |
| Germany cancellation button, section 312k BGB | Applies since 1 Jul 2022 to continuing contracts concluded online with consumers | [V secondary] https://www.gesetze-im-internet.de/bgb/__312k.html |
| US FTC click-to-cancel | The 8th Circuit vacated the rule on 8 Jul 2025 on procedural grounds; status in Nov 2026 UNCONFIRMED; state auto-renewal laws (e.g. California) still apply | [V] https://www.fenwick.com/insights/publications/eighth-circuit-vacates-ftcs-click-to-cancel-rule |
| Product rule | One-click cancel in the console for every plan, renewal reminders, clear pricing incl. VAT, no dark patterns. Cheap to build correctly once; expensive to retrofit. | design |

## 8. Children

GDPR Art. 8 sets 16 as the digital age of consent with member-state reductions to 13; COPPA applies to under-13s; the UK Children's Code applies to services likely accessed by children **[M]** (https://www.ftc.gov/legal-library/browse/rules/childrens-online-privacy-protection-rule-coppa ; https://ico.org.uk/for-organisations/uk-gdpr-guidance-and-resources/childrens-information/childrens-code-guidance-and-resources/ ). A job-search product typically sets a minimum age (16 or 18) with a signup gate: counsel decides. University/bootcamp mode may include 16-17 year-olds: age-gate and parental-consent flow are open design items.

## 9. Legal documents

To be drafted by counsel from product facts: Terms of Service (incl. acceptable use: no fabricated credentials, no bulk unattended applying, no scraping through the product), Privacy Policy (role-specific), Cookie notice if needed, DPA + sub-processor list, SCC/IDTA annexes, SLA (Enterprise), Acceptable Use, Provider-attribution notice, AI transparency notice (what AI is used, no solely automated decisions), Security overview, vulnerability disclosure policy (`security.txt`). Provide a **data map** and a **records of processing** template as engineering deliverables (the schema in 02/03 already marks E columns).

## 10. Security certifications and pen-test plan

| Item | Finding | Tag |
|---|---|---|
| SOC 2 | Type I is a point-in-time report (vendor sources claim roughly 2-4 months); Type II covers an observation period (3-12 months quoted; 6 commonly suggested); total about 6-15 months | [V vendor sources only, no AICPA primary] https://drata.com/learn/soc-2/type-1-vs-type-2 |
| ISO/IEC 27001:2022 | Current edition | [M] |
| ISO/IEC 27701:2025 | Published 14 Oct 2025 as standalone, ISO 27001 no longer a prerequisite; certification-body transition pending in the source | [V secondary] https://www.dnv.com/news/2025/iso-iec-27701-standard-update-release/ |
| OWASP ASVS 5.0.0 | Released 30 May 2025, latest stable per the search result | [V] https://github.com/OWASP/ASVS |

Path (proposal): (1) now: map controls to the existing hardening gates (SBOM, `govulncheck`, `egressban`, `check_no_personal_data`), adopt ASVS 5.0 Level 2 as the internal baseline; (2) before first paying B2B customer: external penetration test (scope: web console, API, SSO, multi-tenant isolation, file upload/render path, webhook SSRF) with retest evidence; (3) SOC 2 Type I when the first enterprise deal requires it, Type II after 6 months of operation; (4) ISO 27001/27701 only if EU public-sector or enterprise demand justifies the cost (UNKNOWN until quotes). Bug-bounty/VDP after the first pen test. Cost figures are not given because none were verified.

## 11. Product obligations

Engineering items that fall out of this document (all into `09` backlog): DSAR queue and SLA timers; tenant retention policy with floors; DPA/sub-processor registry; regional pinning; EU/UK representative arrangement; AI-literacy training log; person-ranking gate and no-emotion-inference gate; age gate; one-click cancel and renewal notices; accessibility gate; export in JSON Resume/Europass; pen-test tracking; licence scanner (05).

## 12. Gaps

Primary texts of Regulation (EU) 2024/1689 and 2026/1744 not opened; C-703/25 P status; Colorado litigation; FTC post-July-2025 action; EAA microenterprise test; scope of the Omnibus generative-AI ban; adequacy of the selling entity's country and UK transfer mechanism; whether candidate-side coach tools are in or out of Annex III point 4; state-by-state AI hiring law applicability. All require counsel.
