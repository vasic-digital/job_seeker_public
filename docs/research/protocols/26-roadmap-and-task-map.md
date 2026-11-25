# 26 - Roadmap and task map

| | |
|---|---|
| Part | C of the protocols research |
| Date | 2026-11-24 |
| Numbering (reconciled against `github/main` after PR #43) | JSK-T160..T187 = Part B ATS backlog ([13](13-remediation-backlog.md)); JSK-T188..T219 = provider/Position backlog ([../providers/06](../providers/06-integration-plan-and-backlog.md)), T220..T224 reserved there; **this part uses JSK-T225..JSK-T244**, **FR-095..FR-101**, **SC-030..SC-035**. FR-079..094 and SC-024..029 belong to the provider proposal. Epic: new work under JSK-005 (applications) and JSK-016 (provider expansion); the kernel tasks propose a new epic `JSK-017 Extension architecture` (an operator/maintainer decision; nothing is inserted into `docs/workable_items.db` by this PR). |
| Status | PROPOSAL, since APPLIED to `specs/001-job-seeker-mvp/{spec,tasks}.md` (Phase 13, epic JSK-017) with the operator-decision defaults Q-ARCH-1..6 recorded in spec.md Clarifications; where this document and the spec differ (FR-097: no seventh weight), the spec governs |
| Effort | S <= 0.5 d, M 1-2 d, L 3-5 d, XL > 1 week (one engineer, tests and evidence included) |

## Contents

1. [Ordering](#1-ordering)
2. [New tasks](#2-new-tasks)
3. [Map: existing backlog items to interfaces](#3-map-existing-backlog-items-to-interfaces)
4. [Provider backlog mapped to the interfaces](#4-provider-backlog-mapped-to-the-interfaces)
5. [Proposed FRs and SCs](#5-proposed-frs-and-scs)
6. [Operator decisions needed](#6-operator-decisions-needed)
7. [Risks](#7-risks)

## 1. Ordering

```
K  (kernel, no behaviour change)      T225 kernel -> T232 gates -> T233 policy loader/CLI -> T234 migrations
G  (parity, behaviour preserved)      T235 profile adapter + legacy gate parity -> T228 remote_mode -> (T182 approval re-check)
A  (new capability, parallel)         T226 jsonld reader | T227 source helpers -> adapters (T205..T215 in providers) | T236 keyword | T237 ATS data profile
C  (channels, strictly human)         T229 openurl -> T230 mailto -> T240 deliverability -> T231 smtp_compose
D  (checks on artifacts)              T238 parser sim + pdf profile -> T162/T170 (umbrella) consumed
X  (compliance/telemetry)             T239 compliance gates | T241 hooks+evidence | T242 more rules
Z  acceptance                         T244
```

Wave 1 (start together): T225, T226 (reader needs only the `ext` types), T236 (feature is independent of the registry). Wave 2: T232, T233, T234, T235. Wave 3: T228, T227, T229, T237, T238. Wave 4: remaining.

## 2. New tasks

```
- [ ] JSK-T225 [US*] Extension kernel: internal/ext (Verdict, Reason, Registry, Combine, EvaluateEligibility, Resolve, Strategy, conformance Reporter) copied from the compiled sketch with its tests (FR-095, FR-096; 21)
- [ ] JSK-T226 [P] [US*] schema.org JobPosting StandardReader with golden/bad fixtures and mutation pair (FR-095; 24 s3)
- [ ] JSK-T227 [US*] source helpers: export Get/Translate/HTTPSURL from internal/providers, RegisterAttribution, source.Wrap shim; no behaviour change (FR-004, FR-095; 24 s2, s4)
- [ ] JSK-T228 [US*] RemoteMode EligibilityRule + parity test against score.go gate output + wiring into ScorePosting behind a nil-guard (FR-096; 24 s5)
- [ ] JSK-T229 [US*] ApplicationChannel kernel + provider_url channel + ChannelVet + Approved token minted only in Approve (FR-098; 21 s7)
- [ ] JSK-T230 [P] [US*] mailto channel: RFC 6068 parser with header-injection tests, fixed-template subject/body, compose-only (FR-098, FR-029; 24 s8)
- [ ] JSK-T231 [US*] smtp_compose channel: creates a draft in the operator's mailbox via OAuth/app password (env names only); human sends; needs T240 Yes (FR-098; 24 s8)
- [ ] JSK-T232 [P] [US*] gates verify_plugins.sh, verify_channels_nosend.sh with --prove-failure (FR-100, SC-031, SC-033; 25 s6)
- [ ] JSK-T233 [US*] rules/policy.yaml schema + loader + jsctl policy check + jsctl ext list + GET /api/ext (FR-099; 22)
- [ ] JSK-T234 [US*] migrations 0003_ext_registry, 0004_ext_verdicts + verdict store (is_current flip, forged-reason refusal) + staleness detection (FR-096, SC-035; 23)
- [ ] JSK-T235 [US*] Profile adapter over match.Overlay + eligibility plugins for excluded_region, onsite_outside_allowed, blocked_company, salary_below_floor with 100% parity on gold/score tests (FR-096, SC-034; 21 s5)
- [ ] JSK-T236 [P] [US*] keyword ScoreFeature with rules/synonyms.yaml, weight line, score_version bump, gold false-positive rows (FR-097; T164, T166; 24 s6)
- [ ] JSK-T237 [US*] ATSProfile data plugin + rules/ats/*.yaml for greenhouse, lever, ashby, generic; Applies exact host (FR-095; T176; 23 s5)
- [ ] JSK-T238 [US*] ParserSimulator (pdftotext/pdfinfo) + pdf_links FormatProfile checks stored as ext_verdict at draft; block findings refuse approval (FR-096, FR-101; T162, T170 consumed)
- [ ] JSK-T239 [P] [US*] ComplianceGate robots and provider_tos at ingest/draft/approve stages, fail-closed (FR-095; Part A 05)
- [ ] JSK-T240 [P] [US*] DeliverabilityCheck spf/dkim/dmarc via allow-listed DNS-over-HTTPS Fetcher; gate for ext.channel.allow_mailto (Part A 04)
- [ ] JSK-T241 [P] [US*] TrackerHook + EvidenceSink ports with reply-detect/ghosting reference hooks (Part A 07, 08)
- [ ] JSK-T242 [US*] Eligibility rules work_auth, timezone, language over the Profile accessors (T167, T177, T183 share inputs) (FR-096; Part A 06)
- [ ] JSK-T243 [P] [US*] Derived-format DocumentRenderer skeleton: TXT from the resolved model with round-trip equality gate (formerly a T171 prerequisite; T171 is WON'T-DO, ADR 0011)
- [ ] JSK-T244 [REVIEW] Extension-architecture acceptance: SC-030..SC-035 evidence, independent review, tag proposal
```

Detail table:

| ID | Pri | Test-first (RED) | Files | Effort |
|---|---|---|---|---|
| T225 | P0 | kernel tests copied from sketch run RED before the package exists (compile error recorded) | `internal/ext/*.go`, `internal/audit/reasons.tsv` | M |
| T226 | P1 | golden-bad (Person, broken JSON) and absent-location-type rows fail before the reader exists | `internal/ext/reader/jsonld/**` | M |
| T227 | P0 | `source.Wrap(remotive)` returns identical `Batch` as the bare provider over the recorded sample (RED: wrapper absent) | `internal/providers/providers.go:121,139,230`, `internal/ext/source/legacy.go` | S |
| T228 | P0 | parity table old gates vs plugin (RED: plugin absent) | `internal/ext/eligibility/remotemode/**`, `internal/match/score.go` (~:205) | M |
| T229 | P0 | zero `Approved` accepted by a stub channel -> RED; enumeration test over `Registry.IDs()` | `internal/ext/channel/**`, `internal/draft/service.go:432` | M |
| T230 | P2 | header-injection table (CR/LF, unknown header, `bcc`) | `internal/ext/channel/mailto/**` | M |
| T231 | P2 | mailer fake proves only drafts are created; a send call is a compile error (no such method on `Mailer`) | `internal/ext/channel/smtpcompose/**` | L |
| T232 | P0 | gate run on a tree with a plugin lacking a mutant test -> rc 1 | `gates/verify_plugins.sh`, `gates/verify_channels_nosend.sh` | M |
| T233 | P0 | unknown key / locked gate off by provider layer -> loader error | `internal/ext/policy_load.go`, `cmd/jsctl`, `internal/api` | L |
| T234 | P0 | schema test RED on missing tables; forged reason refused | `internal/store/migrations/0003*,0004*`, `internal/ext/verdictstore.go` | M |
| T235 | P0 | parity over `internal/match/gold` and score tests | `internal/ext/eligibility/**`, `internal/match/score.go:141-205` | L |
| T236 | P1 | Django/Go, Java/JavaScript, C/C++/C#, k8s/Kubernetes table RED against the old behaviour | `internal/ext/match/keyword/**`, `rules/synonyms.yaml`, `rules/score.yaml` | M |
| T237 | P1 | over-limit and wrong-type fixtures per profile | `rules/ats/*.yaml`, `internal/ext/ats/dataprofile/**` | M |
| T238 | P1 | GoToR golden-bad PDF -> block finding; 260/300 on the umbrella set recorded RED | `internal/ext/ats/parsersim/**`, `internal/ext/render/pdflinks/**`, `internal/draft/service.go:245` | L |
| T239 | P1 | settings missing -> Unknown; robots disallow fixture -> No | `internal/ext/compliance/**` | M |
| T240 | P2 | DNS fixtures (no SPF, p=none DMARC) | `internal/ext/deliver/**`, `deploy/egress-allowlist.txt` | M |
| T241 | P2 | hook error does not fail status write; full sink buffer does not block | `internal/ext/track/**`, `internal/ext/evidence/**`, `internal/tracker/tracker.go:87` | M |
| T242 | P1 | truth tables with Unknown rows | `internal/ext/eligibility/{workauth,timezone,language}/**` | L |
| T243 | P2 | DOCX/TXT text round-trip diff counts | `internal/ext/render/txt/**` | M |
| T244 | - | n/a | evidence directory | M |

## 3. Map: existing backlog items to interfaces

Part B ATS backlog ([13](13-remediation-backlog.md)); "umbrella" = renderer work in the career engine repository.

| Task | Gap / failure | Interface that carries it | Plugin / data | Notes |
|---|---|---|---|---|
| T160 cover slots | G-01 | `DocumentRenderer`/slot API (`internal/templates`) | umbrella build + `FormatProfile` for the cover | typed slots, no generation (FR-029) |
| T161 send-time date | G-01, F-07 | `Approved` hash input + `EligibilityRule` `date_fresh` | rule at approve stage | `DraftHash` input widens (`service.go:191`) |
| T162 link annotations | G-02 | `ATSProfile.Check` + `FormatProfile.LinkSchemes` | T238 `pdf_links` | renderer fix is umbrella |
| T163 placeholder gate | F-06 | `ATSProfile.Check` (`placeholder_leak`) | T238 | reserved-token registry as data |
| T164 term extractor | G-03 | `Enricher` + `ScoreFeature` | T236 | dictionary = `rules/synonyms.yaml` |
| T165 coverage + honesty | G-03 | `ScoreFeature` reasons + approval report | T236 + T187 | allow-list via `Profile` |
| T166 exact matcher | G-04 | `ScoreFeature` (umbrella side separate) | T236 | table in 24 s6 |
| T167 language detector | G-05, G-13 | `EligibilityRule` `language` + draft language rule | T242 | unify `minLangConf` with `match.LangMinConfidence` |
| T168/T169 extractor variance, RTL | G-06, G-07 | `ParserSimulator` + `ATSProfile` | T238 | measurements in 25 s3 |
| T170 tagged PDF/XMP | G-08 | `FormatProfile.RequiresTags/XMP` | T238 | umbrella renders |
| T171 DOCX/TXT | G-09 | `DocumentRenderer` | T243 | WON'T-DO 2026-11-24 (ADR 0011: PDF-only); TXT stays with T243 |
| T172 manifest freshness | G-10 | `ComplianceGate` `manifest_fresh` at draft | new data check in `templates.Open` | `gate_stamp` field |
| T173 page budget | G-11 | `FormatProfile.MaxPages` | T238 | decision first |
| T174 headings | G-12 | `FormatProfile.LocaleHeading` | data | alias table per locale |
| T175 filenames | G-15 | `FormatProfile.FilenameTmpl` | data + `templates.go:197` | |
| T176 upload profiles | G-16 | `ATSProfile.Limits` | T237 | |
| T177 knockout checklist | G-17 | `KnockoutHandler` | handler plugins `work_auth`, `sponsorship` | answers in overlay |
| T178-T180 header/footer, OCR, Hebrew | G-18..G-23 | `ATSProfile.Check`, `ParserSimulator` | T238 | |
| T181 company duplicate | F-04 | `ComplianceGate` `duplicate_company` (with providers G9c) | T239 family | shares `employer` table with providers |
| T182 re-evaluate gates at approval | F-12..14 | `EvaluateEligibility` in `checkLive` | T228, T235 | |
| T183 business hours | F-15 | `EligibilityRule` `local_hours` (advisory: Yes/Unknown only) | T242 | |
| T184 mailto manual-only | F-16 | `ApplicationChannel` strategy `manual_only` | T229/T230 | |
| T185 scam flags | F-17 | `Enricher` + `ComplianceGate` | T239 family | |
| T186 pin posting hash | F-18 | `application.approved_posting_hash` column | T234 | |
| T187 approval report | F-27 | reads `ext_verdict` rows, findings, coverage | T238 + console | |

## 4. Provider backlog mapped to the interfaces

| Provider task ([providers/06](../providers/06-integration-plan-and-backlog.md)) | Interface | Note |
|---|---|---|
| T200 manual intake | `SourceAdapter` (manual, no fetch) + `StandardReader` | |
| T201-T203 HN, Bundesagentur, Arbeitnow, Jobicy | `SourceAdapter` | recipe 24 s9 |
| Greenhouse/Lever/Ashby (P2, in the T190s group) | `SourceAdapter` | shape in 24 s4 |
| T204 provider profile + ranking | data feeding `Strategy` `quality_ranked` | 22 s4 |
| T205 G9a-d, `position_apply_lock`, `policy_sha` | `ComplianceGate` + core lock | 22 s5 |
| T206 bypass suite | extends SC-005 enumeration with channels (25 s6) | |
| T209/T210 merge queue, `jsctl resolve` | `position.Resolver` (provider-owned) | not user-pluggable |
| T211-T215 | `SourceAdapter`, JSON-LD reader (T226 supplies it) | T213 consumes T226 |
| T216 API contract additions | `GET /api/ext` added by T233 | merge into one contract PR |
| T218 privacy | `ext_verdict` export/delete (23 s6) | |

## 5. Proposed FRs and SCs

To be applied by a separate small PR after this one (not edited here). Numbering starts at FR-095 / SC-030.

| ID | Text (proposal) |
|---|---|
| FR-095 | Every pluggable capability (source, reader, eligibility rule, score feature, channel, compliance gate, ATS profile, format profile, deliverability check, hook, sink) MUST be a registered plugin with id, kind, semantic version, spec reference and capability flags; registration happens in code, activation in data; a duplicate or incomplete registration MUST abort start-up. |
| FR-096 | Every blocking decision of a plugin MUST be a three-valued verdict (yes, no, unknown) with explainable reason codes from the closed vocabulary and ids/hashes only; Unknown MUST block exactly as No; a panicking, timed-out, unconfigured or absent gate plugin MUST yield Unknown; a verdict MUST be stored with plugin id, plugin version, policy hash and ruleset hash. |
| FR-097 | Score features beyond the six specified weights MAY be added only through the score feature port with a weight in `rules/score.yaml`, a `score_version` bump and a spec amendment recording the new weight set. |
| FR-098 | An application channel MUST accept only an approval token produced from a human approval record whose approved hash equals the draft hash; no registered channel MAY declare direct sending; channels return hand-off data (link, mail draft) for the human to act on. |
| FR-099 | Plugin activation, strategy choice, thresholds and per-ATS profiles MUST be data (policy file, settings, rules files) with a validated schema, layered precedence with provenance, and locked gates that only the operator layer can disable, with an audit row. |
| FR-100 | Every registered plugin MUST ship fixtures (good, bad, missing-data), a conformance run and a mutation pair proving the suite fails on a weakened copy; a gate MUST fail the tree when any is missing. |
| FR-101 | The hard gates evaluated at scoring MUST be re-evaluated by the same plugins at live approval and at hand-off completion; a regression MUST refuse the action with a reason code and an audit row. |

| ID | Success criterion (proposal) |
|---|---|
| SC-030 | Adding a reference eligibility rule, reader and score feature each requires only a new package, one registration line, one policy line and fixtures; verified by a diff-size test that fails if any other production file changes. |
| SC-031 | `gates/verify_plugins.sh` exits 0 on the tree and exits 1 on a copy where one plugin lacks its mutation test (`--prove-failure`). |
| SC-032 | Over the full provider x ATS x country scope matrix no locked gate resolves to off. |
| SC-033 | Every registered channel rejects a zero approval token; the callers of the token constructor are exactly the approval service. |
| SC-034 | The plugin gate output equals the legacy gate ids on 100% of the gold and score test rows before the legacy blocks are removed. |
| SC-035 | A stored verdict computed under a different policy or ruleset hash is reported stale and is never used to approve. |

## 6. Operator decisions needed

| # | Question | Needed by |
|---|---|---|
| Q-ARCH-1 | Accept the new epic JSK-017 and ids T225..T244, FR-095..101, SC-030..035? | all |
| Q-ARCH-2 | Migration order: `0002_position` (providers) before `0003`/`0004` (this part)? Recommended: yes. | T234 |
| Q-ARCH-3 | Is a compose-only SMTP/OAuth channel (draft in the operator mailbox, human presses Send) acceptable, and is an unattended SMTP send out of scope for v1? (Recommended: yes and yes; an unattended send needs a spec amendment of FR-026/FR-027.) | T231 |
| Q-ARCH-4 | Adding a seventh score weight (keywords) amends the specified six weights (FR-097). Accept, or keep keywords as a reasons-only feature with weight 0? | T236 |
| Q-ARCH-5 | A PDF-parsing Go dependency vs shelling out to poppler tools (no new dependency, host tools required)? Recommended: shell out first. | T238 |
| Q-ARCH-6 | Remove the legacy gate blocks in `ScorePosting` after parity is proven (dead-code rule: needs operator approval)? | T235 |

## 7. Risks

| Risk | Mitigation |
|---|---|
| Kernel generics (`Registry[T]`) and Go 1.26 toolchain mismatch | the sketch compiles with Go 1.26.8; `go.mod` of the module pins 1.26.11 (verified `go.mod`), the sketch uses no feature newer than 1.22 |
| Over-abstraction: ports with one implementation | only ports with a Part A/B consumer are in scope (20 s4); `MatchRule`, `Enricher`, `TrackerHook`, `EvidenceSink` stay specified-but-unbuilt until a task needs them |
| Policy file becomes a second source of truth for rules | policy holds on/off, strategy and params only; weights and tokens stay in the existing rules files |
| Parity drift while moving gates | parity test (SC-034) is RED-first and runs on the gold set before any legacy removal |
| Provider research changes `Posting` fields | the sketch `ext.Posting` mirrors `providers.Posting` (`providers.go:94`); the production kernel imports that type directly, so a field change is one compile error away |
| Vendor claims unverified | every vendor mapping in 24 is marked UNCONFIRMED until a recorded sample exists (P0 capture task in providers/06) |
