# 05 - Identity, trust, privacy and legal

Index: [00-README-index.md](00-README-index.md). **This is engineering research, not legal advice.** Every legal statement is tagged and anything not read in a primary text is `UNCONFIRMED:`. Retrieval date **2026-11-24**. Tags: `[S]` secondary (law-firm notes, vendor blogs, search summaries), `[K]` known instrument cited from background knowledge, not re-fetched.

## Contents

1. [Position of job_seeker](#1-position-of-job_seeker)
2. [Data protection: GDPR, UK GDPR, CCPA/CPRA](#2-data-protection)
3. [EU AI Act relevance](#3-eu-ai-act-relevance)
4. [Pay and job-advert transparency law](#4-pay-and-job-advert-transparency-law)
5. [Crawling: robots.txt, ToS, API licences, AI-use signals](#5-crawling-robotstxt-tos-api-licences-ai-use-signals)
6. [Automated applying, anti-bot and CAPTCHA ethics](#6-automated-applying-anti-bot-and-captcha-ethics)
7. [Identity and authentication: OAuth, OIDC, passkeys](#7-identity-and-authentication)
8. [Verifiable credentials: W3C VC, DID, Open Badges, EDC](#8-verifiable-credentials)
9. [Integration for job_seeker](#9-integration-for-job_seeker)
10. [Priority table](#10-priority-table)

## 1. Position of job_seeker

- **Private, single-operator tool** (spec FR-010): it processes the *operator's own* personal data (CV, contact data) and *third-party* public postings (which contain recruiter names/emails = personal data of third parties).
- It is **candidate-side**: it ranks postings *for the user*; it does not rank or screen *candidates* for an employer. That distinction drives the AI Act reading in section 3.
- Household/personal-activity exemption (GDPR Art. 2(2)(c)) `[K]` may cover purely personal use; whether it covers an operator who also processes third-party recruiter data in a structured, searchable system is **UNCONFIRMED**. Design as if GDPR applies.

## 2. Data protection

| Instrument | Key points for us | Source |
|---|---|---|
| GDPR (EU) 2016/679 | lawfulness (Art. 6), special categories (Art. 9: a CV can reveal health, ethnicity via photo, union membership), transparency (Art. 13/14), access (15), erasure (17), restriction (18), portability (20), objection (21), **automated decision-making (22)**, records of processing (30), security (32), DPIA (35) | EUR-Lex 32016R0679 `[K]` |
| UK GDPR + DPA 2018 | same shape; ICO guidance on recruitment | `[K]` |
| CCPA/CPRA (California) | right to know/delete/correct; employment-related data scope changed 2023 | `[K]` UNCONFIRMED details |
| Serbia: Zakon o zastiti podataka o licnosti (2018) | GDPR-modelled | `[K]` UNCONFIRMED current text |
| Greenhouse API exposes consent/retention hints | `data_compliance{requires_consent, requires_processing_consent, requires_retention_consent, retention_period}` and POST fields `gdpr_processing_consent_given`, `gdpr_retention_consent_given`, `gdpr_demographic_data_consent_given` | `[P]` docs.greenhouse.io/job-board.html |

**Engineering consequences (the existing FR set already points the same way: retention pruner `JSK-T044`, privacy ops `JSK-007`):**
1. **Minimisation:** store only the posting fields needed; recruiter emails/names from postings are stored encrypted, tagged `third_party_pii`, and purged with the posting (90 d / 14 d / expiry rules already exist; application-linked postings exempt).
2. **Erasure/export:** `jsctl purge-source` exists; add `jsctl erase-contact <email>` that removes a third-party contact from all tables and logs (audit keeps a salted hash, not the address).
3. **Consent capture for the user's application data:** when an ATS form asks for consent (Greenhouse `requires_consent` etc.), the drafter **never ticks consent boxes**; consent is a human act in the approval step, recorded with timestamp and exact consent text version.
4. **Retention of the user's applications:** keep until the user deletes; default review prompt at 12 months. Retention periods employers apply are their own (Greenhouse exposes `retention_period` in days; value varies by tenant).
5. **Special categories:** the CV renderer omits photo, age, marital status, religion by default ([03](03-resume-cv-format-and-parsing.md)); EEO/demographic form fields are never auto-answered ([02](02-ats-and-application-apis.md)).
6. **Cross-border transfer:** LLM/API providers receiving posting text or CV data are processors; use only providers the operator approved (`internal/llm` allow-list) and log each transfer.
7. **Security:** encryption at rest exists (`internal/crypto`); add retention of mail tokens with the same envelope.

Test plan: privacy gates already exist (`internal/privacy`, `internal/pdgate`); add tests that an `erase-contact` run leaves zero matches for the address in a decrypted dump of every table and in `provider_call` bodies.

## 3. EU AI Act relevance

- Annex III lists AI systems **for recruitment/selection of persons, including filtering applications and evaluating candidates** as *high-risk* `[K]`; deployers/providers of those systems carry obligations.
- **Dates found `[S]`:** several law-firm and trade sources report that the Digital Omnibus on AI **postponed the Annex III high-risk application date from 2 August 2026 to 2 December 2027** (Annex I embedded products: 2 August 2028), one reporting adoption as **Regulation (EU) 2026/1744 in force from 27 July 2026**; other transparency duties reportedly still apply from 2 August 2026. Some older notes still cite the original date. **UNCONFIRMED until the Official Journal text is read** (not fetched).
- **Reading for job_seeker (interpretation, UNCONFIRMED legal view):** the high-risk category targets systems used *by or for employers* to evaluate candidates. job_seeker evaluates **postings for its own user**, produces drafts, and never decides about a person. It is therefore unlikely to be a high-risk system. Two caveats: (a) if the tool is ever offered to third parties (job-seeker coaching services) or exposes candidate-ranking features, re-assess; (b) LLM-generated text has **transparency** implications (Art. 50 AI Act, chatbot/synthetic content disclosure) mainly for providers/deployers interacting with natural persons: a cover letter the user reviews and sends as their own is the user's statement. **Operator decision needed:** whether to add a visible disclosure line when a draft is substantially model-written; recommended default = off, because the human approves and adopts the text, but flagged for legal confirmation.
- The *receiving* ATS may run high-risk screening on the user's application; the user has Art. 22 GDPR rights against solely-automated decisions. Nothing for us to implement beyond logging what was sent and when (supports a later access/objection request).

## 4. Pay and job-advert transparency law

- EU Pay Transparency Directive (EU) 2023/970: transposition deadline **7 June 2026** `[K]`; reports from September-October 2026 say only a few Member States (Italy, Slovakia, Malta, Lithuania reported as having transposed by mid-June 2026; Ireland, Czechia, Netherlands delayed) had done so `[S: Baker McKenzie 2026-06, Littler, Eurofound, Lockton 2026-09-25]`. The Directive requires pay information to be provided **to applicants before the interview** (in the posting or otherwise) and bans asking about pay history; national texts differ - UNCONFIRMED per country.
- Several US states (CO, NY, CA, WA, and others) require pay ranges in postings `[K]` UNCONFIRMED current list.
- **Meaning:** `Posting.SalaryMin/Max` completeness will rise over 2026-27 in EU postings; scoring should treat "range present" as a positive trust signal ([07](07-matching-ranking-and-quality-signals.md)). **Never volunteer salary history** in drafts (illegal to ask in some jurisdictions and counter-productive); state expectations only if the form requires and the operator set a number.
- Ghost-job rules: New York S8877 reportedly passed both houses (as of mid-Sept 2026 awaiting signature); Ontario reportedly in effect since 2026-01-01; no US state law in force per one tracker `[S]` - UNCONFIRMED.

## 5. Crawling: robots.txt, ToS, API licences, AI-use signals

- **robots.txt = RFC 9309** `[K]`: honour `User-agent`, `Allow/Disallow`, cache <= 24 h; a 4xx on robots.txt means "no restrictions" and a 5xx means "assume disallow". Identify with a stable `User-Agent` including a contact URL. IETF **aipref** drafts (`draft-ietf-aipref-vocab-07`, `draft-ietf-aipref-attach-05`, Aug 2026; still Internet-Drafts) define a `Content-Usage` header / robots.txt directive for AI-use preferences `[S: ietf.org archive search results]`; honour if present when feeding postings to a model (treat "no AI training/inference" markers conservatively: skip LLM use of that posting body and use rule-based matching only).
- **ToS and API licences are binding independent of robots.txt.** Verified terms (primary): **Himalayas** `[P: himalayas.app/api]`: show jobs only on sites/apps you operate, use `applicationLink` unchanged, visible source link, remove expired jobs, re-check with the single-job endpoint, **do not pass jobs to other boards/aggregators/Google Jobs/LinkedIn Jobs**, 60 req/min + 10,000/day with key. Secondary (not re-verified, UNCONFIRMED): Remotive asks <= 4 fetches/day, delayed listings and attribution; RemoteOK requires attribution link and forbids using listings to collect signups; Jobicy permits storage with credit and hourly sync at most; Arbeitnow free, may revoke `[S: dev.to/scrapemint and Apify listings]`.
- **Legal-notice tracking** already exists (`JSK-T045`: re-read, hash, diff log). Extend to store the *date a ToS was last reviewed by a human* per provider; a provider whose notice hash changed is **auto-paused** until reviewed.
- **LinkedIn/Indeed/Glassdoor:** secondary sources report automation prohibited (LinkedIn User Agreement section 8.2 quoted by `[S]`); primary text not fetched: UNCONFIRMED. Policy: no crawling, no automation, human-pasted links only.
- Scraping-law background (e.g., the hiQ v. LinkedIn line of US cases, EU database right, DSM Directive Article 4 TDM opt-out) `[K]` is unsettled and fact-specific; not relied upon.

## 6. Automated applying, anti-bot and CAPTCHA ethics

- Secondary sources, many from auto-apply vendors with a commercial interest `[S]`, report: ATS sites often have terms against automated submission, enforcement against assisted autofill is uncommon, Lever added fraud signals (email validity, virtual phone numbers, bot-like behaviour, employment-history consistency) in 2026 that flag applications to recruiters rather than auto-reject them. Treat all as UNCONFIRMED.
- **Our position:** a human submits. We neither solve CAPTCHAs, rotate identities/IPs, spoof fingerprints, nor mass-submit. CAPTCHAs exist to say "a human is here"; defeating them contradicts the product's honesty premise and risks the candidate's reputation inside the employer's ATS. If a CAPTCHA or bot challenge appears in any flow job_seeker *reads*, the adapter marks the source `blocked`, backs off (circuit state exists: `internal/scheduler`), and surfaces it in provider health. No bypass code, no paid solver integration.
- Rate and identity: stable UA string with contact, per-host limiter, no cookie-jar harvesting, no logging into employer accounts.

## 7. Identity and authentication

| Standard | Use | Status found |
|---|---|---|
| OAuth 2.0 (RFC 6749), PKCE (RFC 7636), OAuth 2.0 Security BCP (RFC 9700) | mailbox access ([04](04-email-and-transport-protocols.md)), future provider logins | `[K]` |
| OpenID Connect Core 1.0 | sign-in to the console if multi-user | `[K]` |
| WebAuthn / passkeys | **already used**: `go-webauthn/webauthn` v0.18.2 in `go.mod`; console auth in `internal/auth` | WebAuthn Level status in 2026 UNCONFIRMED |
| JWT/JOSE (RFC 7519, 7515) | vendor tokens | `[K]` |

Nothing new is required for sign-in; the useful action is to keep OAuth refresh tokens for mail in the encrypted store with per-provider scope records and a revocation command (`jsctl revoke-oauth <provider>`).

## 8. Verifiable credentials

- **W3C Verifiable Credentials Data Model 2.0** is a W3C Recommendation (family incl. JOSE/COSE securing and Bitstring Status List v1.0) `[S: w3.org news 2025 via search]`. **DID Core** `[K]`. **Open Badges 3.0** (1EdTech) is built on VC; adoption reports conflict: vendor blog says OB 2.0 still most widely accepted (Mar 2026), W3C community call (Feb 2026) says OB 3.0 implementation nearly complete among vendors `[S]`. **European Digital Credentials (Europass EDC)** moved to JSON-LD in release 1.8 (June 2023), W3C VC aligned `[S]`.
- **Relevance:** employers rarely request machine-verifiable credentials from applicants today (UNCONFIRMED; no evidence of ATS ingestion found). Value to job_seeker is **small and later**: (1) store user-held credentials (degree EDC, badges) as attachments with their verification URL; (2) when a posting asks for proof, offer the artefact. **Priority MAY.** Do not build an issuer/verifier. Interface sketch if ever needed: `type CredentialRef struct{ Kind, URL, SHA256 string; Verified bool }` on `cvdata`.

## 9. Integration for job_seeker

New/extended packages:

```go
// internal/compliance (new)
type Terms struct{ Provider, URL string; Hash, ReviewedBy string; ReviewedAt time.Time; Pause bool }
func (s *Store) NeedsReview(provider string, currentHash string) bool   // blocks Fetch when hash changed
// internal/egress: Robots(host) with RFC 9309 semantics + 24h cache; AIPref(host) reads Content-Usage when present
// internal/privacy: EraseContact(ctx, email) error ; ExportSubject(ctx, email) ([]byte, error)
```

Gates and tests: `gates/robots` - no outbound request is made to a host whose cached robots disallows the path (mutation: disable the check, expect RED); `compliance` test that a changed legal-notice hash pauses the provider; erase-contact end-to-end on a real SQLite store; audit that no code path ticks a consent field (grep gate for `gdpr_*consent_given` writes outside the approval handler).

**Risks:** over-reading secondary legal summaries (every legal row needs counsel confirmation before any commercial use); AI Act scope creep if the tool is ever productised; third-party PII in logs.

## 10. Priority table

| Item | Priority |
|---|---|
| robots.txt (RFC 9309) enforcement + stable UA | MUST |
| Provider ToS hash/ review/ auto-pause | MUST |
| Third-party PII tagging, erase/export commands | MUST |
| Never auto-consent / never auto-answer EEO | MUST |
| aipref `Content-Usage` honouring | SHOULD |
| Legal confirmation of AI Act and disclosure line | SHOULD (operator action) |
| CAPTCHA/bot bypass, identity rotation | WON'T |
| VC/DID/Open Badges ingestion | MAY |
