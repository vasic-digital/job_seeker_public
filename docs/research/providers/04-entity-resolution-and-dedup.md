# 04 - Entity resolution and dedup

| | |
|---|---|
| Status | Design proposal (docs-only) |
| Starting point (read from `internal/ingest/ingest.go` on main) | `markDup`: a NEW listing gets `dup_of` when another provider holds an equal `normTokens(company)|normTokens(location_raw)` key and `TitleRatio` (1 - Levenshtein/maxlen over normalised titles) is at least `DupThreshold = 0.92`; `dup_of` always points at a root. Same-provider listings are never compared. |
| Known limits of the starting point | exact company string equality after lowercase/punctuation fold only; location string must match exactly (so "Remote" vs "Worldwide" never matches); no ATS identity; no body evidence; no human override; no un-merge; no audit; legacy matches are sticky |

## 1. Principles

1. **Precision over recall for automatic merges.** A false merge hides a distinct job (or blocks an application via a duplicate warning); a false split only costs a duplicate row. Automatic merge needs hard evidence or very strong soft evidence; the uncertain band goes to a human queue.
2. **Deterministic and versioned.** Same inputs and `resolver_version` give the same Positions (no clock, no map-iteration order, no RNG without a fixed seed). Embedding use is optional and its score is rounded before thresholding so floating-point noise cannot flip a decision.
3. **Human decisions are law.** `must_link` / `cannot_link` constraints beat every score.
4. **Explainable.** Each decision stores its feature vector in `position_link.features_json`; the console shows it.
5. **No new outbound traffic.** Resolution uses only data already fetched.

## 2. Normalisation

### 2.1 Employer

Pipeline (pure function `NormEmployer(s) string`):
Unicode NFKC, lowercase, fold diacritics, transliterate Cyrillic (RU and SR Cyrillic) to Latin, replace `&` with `and`, drop punctuation, then strip legal-form suffixes as a separate trailing-token pass (`inc, llc, ltd, limited, gmbh, ag, se, sa, bv, nv, plc, corp, co, oy, ab, as, srl, spa, d o o, doo, ооо, оао, зао, ад`), then strip noise words only at the tail (`group, holding, technologies, technology, software, labs`) **only when** it leaves at least one token (never reduce "Technologies" to empty).
Resolution to an `employer` row, in order: (1) `employer_alias(ats_tenant)` match from the listing's ATS identity, (2) `employer_alias(domain)` from apply-URL host or company URL (excluding known ATS/board hosts, so `boards.greenhouse.io` is never a company domain), (3) exact `name_norm`, (4) alias `name`, (5) fuzzy: Jaro-Winkler at least 0.96 on `name_norm` **and** a second corroborating signal (shared domain, or same ATS tenant, or same location+title); otherwise create a new employer. Fuzzy name-only never attaches silently: it enqueues an `employer_merge` review item (reason code `employer_alias_proposed`).
Agencies: a listing whose provider field or text marks a staffing intermediary (`employer.kind='agency'`, set by human or a small curated list) gets `employer_id` of the agency; cross-employer merge is forbidden, and the same req under an agency plus the end client is deliberately two Positions unless a human `must_link`s them.

### 2.2 Title

`NormTitle`: the existing `normTokens`, then: drop gender markers `(m/f/d) (m/w/d) (f/m/x) m/f/d`, drop bracketed/location suffixes (`- Remote`, `(EMEA)`, `| Europe`), expand `sr.`/`jr.`, map `Sr`/`Senior`/`Lead`/`Staff`/`Principal`/`Junior`/`Intern`/`Mid` to a **seniority token** extracted into its own feature. Language synonyms are table data, not code: `Entwickler(in)` = developer, `Softwareentwickler` = software engineer, `разработчик` = developer, `инжењер`/`инженер` = engineer (to be extended from the corpora; tests label these as table data).
Rule: **seniority mismatch (both present and different tier) is a hard negative** for auto-merge (Senior Android Engineer is not Junior Android Engineer, even at ratio 0.95). Technology tokens (`android`, `flutter`, `go`, `react native`) are kept as a separate set; a symmetric difference in the technology set is a strong negative.

### 2.3 Location and remote scope

Normalise to `RemoteScope{Mode: onsite|hybrid|remote, Countries: set ISO-3166, TZ: [from,to], Worldwide: bool}`. Raw strings map through a table (`Worldwide, Anywhere, Global` => Worldwide; `EMEA`, `Europe` => region set; `Remote - Serbia`). Compatibility: onsite/hybrid require same city or same country when both known; remote requires set intersection or one side Worldwide. Missing on one side is neutral (not negative).

### 2.4 Apply URL canonicalisation and ATS identity

`CanonURL(u)`: lowercase scheme/host, strip `www.`, drop default ports, drop fragments, drop tracking params (`utm_*`, `gh_src`, `source`, `ref`, `lever-source[]`, `lever-origin`, `gclid`, `fbclid`) while **keeping** identity params (`gh_jid`), strip trailing slash, strip trailing `/apply`, collapse `jobs.eu.lever.co` to `jobs.lever.co` for matching (but record region), percent-decode path.
ATS extraction (regexes from the research cheat sheet in `02`, section C):

| ATS | Pattern | Confidence |
|---|---|---|
| Greenhouse | `greenhouse\.io/([^/]+)/jobs/(\d+)`; also `gh_jid=(\d+)` on company-hosted pages | path form shown in an official doc example (format not formally specified); `gh_jid` UNCONFIRMED |
| Lever | `jobs(?:\.eu)?\.lever\.co/([^/]+)/([0-9a-f-]{36})` | path confirmed; UUID shape from memory |
| Ashby | `jobs\.ashbyhq\.com/([^/]+)/([0-9a-f-]{36})` | UNCONFIRMED: capture one live response before enabling |
| Workable | `apply\.workable\.com/([^/]+)/j/([A-Z0-9]+)` | UNCONFIRMED |
| SmartRecruiters | `smartrecruiters\.com/([^/]+)/(\d+)` | UNCONFIRMED |

Each pattern ships disabled-by-default until a golden test built from a real captured URL (labelled) passes (anti-bluff: a regex that matches only the author's imagination is not evidence). Providers that return the ATS URL as `apply_url` (most remote boards redirect to the employer ATS) thus yield `(ats_kind, ats_tenant, ats_job_id)` for non-ATS Listings too, which is what links a Remotive listing to the Greenhouse listing of the same job.

## 3. Pipeline

```text
new/changed Listing L
 0. identity   : same (provider_id, provider_job_id) -> existing Listing (already handled by idem_key)
 1. hard keys  : ATS identity equal to another Listing's        -> link (score 1.00, reason ats_identity)
                 apply_url_canon equal                            -> link (0.99, reason apply_url)
                 must_link constraint                             -> link
 2. blocking   : candidate set = Listings with same employer_id AND state not expired
                 AND |posted_at delta| <= 60 days (or unknown) AND no cannot_link with L
                 [no employer -> block by (title token set, country) only, auto-merge DISABLED]
 3. pair score : features below -> logistic combination -> s in [0,1]
 4. decide     : s >= T_auto  AND guards pass -> attach to the candidate's Position (best s, tie: lowest id)
                 T_review <= s < T_auto           -> create merge_candidate (status open), L gets its own Position
                 else                             -> new Position
 5. transitive : attaching may bridge two Positions (L matches A and B above T_auto) -> merge both (keep lower id)
                 only if no cannot_link crosses the two sets; else queue candidate, no merge
```

### 3.1 Features

| Feature | Definition | Role |
|---|---|---|
| `f_title` | existing `TitleRatio` on `NormTitle` | main soft signal |
| `f_tokens` | Jaccard on title token sets minus stop words | robust to word order |
| `f_seniority` | 1 equal / 0.5 unknown / 0 differ | hard negative when 0 |
| `f_tech` | Jaccard on technology token sets | strong negative on disjoint non-empty |
| `f_loc` | scope compatibility 1 / 0.5 neutral / 0 | hard negative when 0 for onsite |
| `f_date` | exp(-delta_days/30), 0.5 unknown | soft |
| `f_salary` | range overlap 1 / 0.5 unknown / 0 disjoint | soft; disjoint is a negative |
| `f_body_mh` | minhash (128 perms) Jaccard estimate over 5-word shingles of sanitized body | strong when both bodies exist |
| `f_body_emb` | cosine of local embedding (nomic-embed-text through the local embedder) | optional; see 3.3 |
| `f_apply_host` | same employer-owned apply host | soft |

Combination: a hand-set logistic with weights in a versioned data file, e.g. `logit = -6 + 4.0 f_title + 1.0 f_tokens + 1.5 f_body_mh + 1.0 f_body_emb + 0.8 f_date + 0.5 f_apply_host + 0.5 f_salary`, **with hard guards applied after**: `f_seniority=0`, `f_loc=0` (onsite), `f_tech=0` with both non-empty, different `employer_id`, or cannot_link force "not auto". The numbers above are *initial proposals*, not tuned values: they are calibrated on the labelled pair set (section 6) and the calibration is part of the task; no threshold is claimed measured.

### 3.2 Thresholds (proposal, to be calibrated)

| Band | Condition | Action |
|---|---|---|
| auto | s >= 0.92 and guards pass (keeps today's 0.92 as the single published constant, now applied to `s` not the raw title ratio; the legacy title-only rule is reproduced exactly when no other feature is available, so existing tests stay valid) | attach |
| review | 0.75 <= s < 0.92, or auto-eligible but employer unresolved/agency | queue |
| distinct | s < 0.75 | new Position |

Thresholds live in the versioned resolver data, not code constants (FR rule: overlay/tunables as data); changing them bumps `resolver_version` and triggers a **re-resolution dry run** (3.5).

### 3.3 Embeddings

Optional stage, off by default; enabled only when the local embedder is configured. UNCONFIRMED: `internal/llm` today contains only the tie-break client and no embedding client; the nomic embedder exists in the workshop stack, not (as far as read) in job_seeker. Task T-P05 adds a small `Embedder` interface with a nomic/Ollama implementation behind the existing egress allow-list (loopback only), with unavailability = feature absent (neutral 0.5 weight redistributed), never an error. Vectors are derived from the sealed body, therefore stored sealed (E) in `sig_sealed` and deleted with the Listing. Cost control: only computed for pairs in the review band candidates of step 2 (typically a handful per employer).

### 3.4 Review queue and operations

- Console `GET /queue/merges`: pairs side by side with the feature vector, differing fields highlighted, provider chips; actions **Merge** (writes `must_link`), **Keep apart** (writes `cannot_link`), **Skip**.
- `POST /positions/{id}/merge {into}` and `POST /listings/{id}/split` for corrections after the fact; both require a reason, are audited, step-up is **not** required (they change no outbound state), but they are blocked while an application on either side is in `live_approved` (409 `position_busy`) to protect the approved-hash invariant.
- **Merge** semantics: all Listings move to the keeper Position; applications keep their `listing_id` and gain the new `position_id`; if both sides had applications, a warning banner is raised (not blocked); constraints written. `merged_into` set on the absorbed Position.
- **Split** semantics: the Listing gets a new Position and a `cannot_link` to every former sibling; the representative is recomputed; if the split Listing had an application, `application.position_id` follows it.
- Audit reason codes (new, string constants in one table, tested for presence like `retention_prune`): `position_auto_attach`, `position_new`, `position_review_queued`, `position_merge_human`, `position_split_human`, `position_constraint_added`, `position_rerun_applied`, `employer_alias_proposed`, `employer_alias_human`. Evidence JSON holds ids, score, resolver_version; never body text (privacy).

### 3.5 Re-resolution and drift

`jsctl resolve --dry-run` recomputes with the current `resolver_version` over a snapshot and prints a diff (would-merge, would-split, would-queue) without writing; `--apply` writes with `position_rerun_applied` audit rows and refuses to violate constraints. Human-decided links are never re-evaluated. A resolver change is therefore reviewable, mirroring how the ruleset sha works for mapping.

## 4. Edge cases (each is a named test)

| Case | Expected |
|---|---|
| Same job on Remotive and RemoteOK, apply URL points to the same Greenhouse id | auto, `ats_identity` |
| Same employer, two real openings with identical title in different countries | distinct (`f_loc`) |
| Same employer, same title, two Greenhouse ids | distinct: two ATS identities on one tenant is a hard negative |
| Reposted job: same provider, new `provider_job_id`, same ATS id | same Position, two Listings of one provider (permitted: `relist`) |
| Senior vs Junior, otherwise identical | not auto (hard guard) |
| Agency vs end client posting of the same role | distinct unless `must_link` |
| Company renamed / alias (Facebook -> Meta) | via `employer_alias` human or domain match |
| RU/SR Cyrillic vs Latin company name | transliteration then equal |
| Title in German vs English | table synonyms; else body similarity only reaches review band |
| Missing company | never auto; review at best |
| Expired Listing, live duplicate | Position stays `open`; representative moves |
| Body edited by provider (hash changes) | Listing updates; re-resolve only that Listing; human links untouched |

## 5. Complexity and operations

Blocking by employer keeps comparisons to a handful per Listing; no global pairwise pass. Re-resolution is O(listings x block size). The 115-posting corpus and any realistic personal volume (thousands) fit trivially in SQLite; no external index. Resolution runs inside the ingest write transaction for hard keys and cheap features; body-signature and embedding stages run in a short second transaction after the batch commit (so a slow embedder never holds the writer lock), recorded as `position_link` upgrades.

## 6. Test plan (see also 06)

1. **Unit (tool-logic, labelled so)**: `NormEmployer`, `NormTitle`, `CanonURL`, ATS regexes against golden strings, `RemoteScope` compat, minhash determinism (fixed seed), threshold edges 0.919/0.92/0.921, guard overrides, union-find invariants, constraint precedence, idempotency (running resolve twice is a no-op), crash mid-merge (txcheck style, FR atomicity).
2. **Mutation pairs (section 1.1)**: each guard has a paired mutant (remove seniority guard -> Senior/Junior test fails; remove cannot_link check -> constraint test fails; remove agency rule -> test fails).
3. **Real-data evaluation**: (a) the 115 real postings (remotive 15, remoteok 100; manifest `evidence/JSK-T057/manifest.tsv`, bodies in the private data dir per FR-077): run the resolver and have an independent labeller adjudicate every auto-merge and every queued candidate; report precision of auto-merges (target: zero false merges; any false merge is a defect to root-cause) and the number queued. A cross-provider overlap rate is UNKNOWN today; this evaluation measures it. (b) observer DB copies (24 h observer from T048): same procedure on the larger set. (c) Himalayas data once the ToS/key gate is cleared.
4. **Synthetic cross-provider pairs**, clearly labelled `synthetic-pair` and never counted as evidence of recall: constructed by taking a real posting and re-emitting it as a second provider with realistic perturbations (title casing, `(m/w/d)`, suffix removal, location rewrites, tracking params). Used to exercise every edge case in section 4 deterministically and to calibrate thresholds' lower bound; **a synthetic pair never closes an SC** (section 11.4.289: a double proves only the tool's logic).
5. **Live**: once an ATS adapter exists, the live contract test fetches one real Greenhouse/Lever/Ashby board and one aggregator listing that points to it, asserts the ATS identity extraction and the resulting single Position; evidence recorded in `provider_call` rows. The regex rows marked UNCONFIRMED stay disabled until this passes.
6. **Property tests**: random permutation of arrival order yields identical Positions (order-independence modulo documented tie-breaks); no cannot_link violation ever.
