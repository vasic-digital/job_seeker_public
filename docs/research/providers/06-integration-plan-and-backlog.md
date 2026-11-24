# 06 - Integration plan and backlog (proposed additions)

| | |
|---|---|
| Status | Proposal. **No edit to `specs/001-job-seeker-mvp/{spec,tasks}.md` is made by this PR**; section 5 lists the proposed text, to be applied by a second small PR after this one merges |
| Numbering | Reconciled 2026-11-24 against `github/main`: FR-079..FR-094 and SC-024..SC-029 are unused elsewhere (part B, docs/research/protocols/13, proposes no FR/SC); JSK-T160..T187 belong to part B's ATS remediation backlog (`protocols/13-remediation-backlog.md`), so this plan uses JSK-T188..JSK-T219 (T220..T224 reserved) |
| Epic / items | Epic JSK-016 (type Feature, "Provider expansion and canonical Positions") in `docs/workable_items.db`; the tasks below are its children. Deferred items JSK-013/014/015 stay as they are |

## 1. Phasing

| Phase | Content | Depends on |
|---|---|---|
| P0 | Capture real samples (single reads) of Greenhouse/Lever/Ashby/Workable/SmartRecruiters boards and one HN thread; verify the UNCONFIRMED regexes; operator decisions (section 6) | none |
| P1 | Position model: migration 0002, employer normalisation, resolver (hard keys + soft score), backfill from `dup_of`, console Position page read-only | P0 |
| P2 | ATS adapters Greenhouse, Lever, Ashby (company list = operator config data, not code); `manual` URL intake | P1 |
| P3 | HN Who is hiring, Bundesagentur, Arbeitnow, Jobicy | P1 |
| P4 | Apply policy G9, provider profiles, ranking, console settings and UX | P1 |
| P5 | Review queue, merge/split ops, re-resolution dry-run | P1 |
| P6 | Adzuna, Workable/SmartRecruiters/Recruitee/Personio/Teamtailor, generic RSS + JSON-LD adapter, WWR/Working Nomads read-through | P2 |
| P7 | hh.ru read-only (operator app registration), MAY items on demand | operator |

## 2. Adapter checklist (every provider; `docs/providers/extending.md` is the base)

1. Terms page read and quoted (<=25 words) in the adapter's `PROVENANCE`/legal notice; attribution entry added to `attributions`.
2. `Limits()` from the documented limit or a conservative default (`MinInterval`, `MaxPerDay`); allow-list entry in egress.
3. Live contract test (`JS_LIVE=1`) hitting the real endpoint once; sample stored as labelled `fixture-input` only for parsing tests.
4. Fills `ApplyURL` faithfully (no tracking removal in the adapter; canonicalisation is the resolver's job) and, where it knows, `ATS` hints.
5. Provider row + `provider_profile` row; entry in the console Providers page.
6. Counts toward the daily/host caps only through the apply gate, never through ingest.

## 3. Backlog (proposed tasks)

| ID | Task | Phase | FRs |
|---|---|---|---|
| JSK-T188 | Capture and label one real sample each of Greenhouse, Lever, Ashby, Workable, SmartRecruiters public board/job payloads and URLs (single reads, private data dir, manifest tracked); fix the ATS URL regex table | P0 | FR-079 |
| JSK-T189 | Migration `0002_position.sql` (tables in `03`), fsm `PositionStates`, store tests incl. CHECK vocabularies match fsm | P1 | FR-079, FR-080 |
| JSK-T190 | `NormEmployer`, `NormTitle`, `CanonURL`, `RemoteScope` with golden and property tests | P1 | FR-081 |
| JSK-T191 | `position.Resolver` hard-key and soft-score stages, constraints, resolver_version, `position_link` evidence; replaces `markDup` call (legacy retained and tested until an approved removal) | P1 | FR-080, FR-082 |
| JSK-T192 | Backfill job from `dup_of`; `dup_of` mirror maintenance and invariant tests | P1 | FR-080 |
| JSK-T193 | Minhash signatures (sealed) and optional Embedder interface with local nomic implementation behind the allow-list | P1 | FR-082 |
| JSK-T194 | Real-data evaluation of the resolver on the 115-posting corpus and observer DB copies; independent adjudication; record precision/queue size | P1 | FR-082, FR-083 |
| JSK-T195 | Synthetic cross-provider pair generator (labelled) and edge-case suite from `04` section 4 | P1 | FR-083 |
| JSK-T196 | Position detail page, merged-view with provenance, redirect from `/postings/{id}` | P1 | FR-084 |
| JSK-T197 | Greenhouse adapter + company-list config | P2 | FR-085 |
| JSK-T198 | Lever adapter | P2 | FR-085 |
| JSK-T199 | Ashby adapter | P2 | FR-085 |
| JSK-T200 | `manual` intake (paste URL/text; ATS resolves by id; no fetch from non-ATS hosts) | P2 | FR-086 |
| JSK-T201 | HN "Who is hiring" adapter (Algolia) with deterministic comment parser and labelled gold set | P3 | FR-085 |
| JSK-T202 | Bundesagentur adapter (public key from config; terms re-checked at implementation) | P3 | FR-085 |
| JSK-T203 | Arbeitnow and Jobicy adapters | P3 | FR-085 |
| JSK-T204 | `provider_profile`, ranking `R`, ranking_version, stats queries | P4 | FR-087 |
| JSK-T205 | Apply gate G9a-d, `position_apply_lock`, `policy_sha`, handoff re-check | P4 | FR-088, FR-089 |
| JSK-T206 | Bypass-attempt suite (6 named attempts) and concurrency/txcheck tests | P4 | FR-088, SC-025 |
| JSK-T207 | Settings UI: providers, favourites ordering, apply mode, employer policies, per-Position override; i18n all languages | P4 | FR-090 |
| JSK-T208 | Draft-review duplicate warnings and acknowledgement flow | P4 | FR-089 |
| JSK-T209 | Merge candidate queue UI, merge/split endpoints, audit reason codes | P5 | FR-091 |
| JSK-T210 | `jsctl resolve --dry-run/--apply` | P5 | FR-092 |
| JSK-T211 | Adzuna adapter (after terms are read: 403 earlier) | P6 | FR-085 |
| JSK-T212 | Workable, SmartRecruiters, Recruitee, Personio, Teamtailor adapters | P6 | FR-085 |
| JSK-T213 | Generic RSS/Atom adapter and JSON-LD JobPosting fallback (company pages, WWR, Jobspresso) with robots/ToS check per host | P6 | FR-085 |
| JSK-T214 | WWR / Working Nomads read-through with TTL display-only policy | P6 | FR-085 |
| JSK-T215 | hh.ru read-only adapter after operator registers an app and the developer agreement is read | P7 | FR-085 |
| JSK-T216 | HTTP API contract additions merged into `contracts/http-api.md` (section 4) | P4 | FR-093 |
| JSK-T217 | Screenshots and vision checks of new pages per language, light/dark, RTL | P4 | FR-090 |
| JSK-T218 | Privacy: export/delete/retention cover new tables; sealed signature tests | P1 | FR-094 |
| JSK-T219 | Provider-expansion acceptance review and tag proposal | all | all |

(T220-T224 reserved for findings from T194.)

## 4. API contract additions (for `contracts/http-api.md`)

| Method + path | Purpose | Notes |
|---|---|---|
| GET `/positions` ; GET `/positions/{id}` | list / detail with Listings, merged view, policy box | `/postings/{id}` 302 to its Position |
| POST `/positions/{id}/apply-override` body `{mode: inherit|all|single|none, listing_id?, reason}` | per-Position override | audited `position_override_set`; 409 if a `live_approved` application exists |
| POST `/positions/{id}/merge` `{into, reason}` ; POST `/listings/{id}/split` `{reason}` | corrections | audited; 409 `position_busy` while `live_approved` |
| GET `/queue/merges` ; POST `/queue/merges/{id}/merge` / `keep-apart` | review queue | writes constraints |
| GET/POST `/settings/apply-policy` | `apply_mode`, `favourites_strict`, employer defaults | audited |
| POST `/settings/providers/favourites` | ordered list | audited |
| GET/POST `/settings/employers/{id}/policy` | per-employer policy | audited |
| POST `/applications/{id}/approve` | gains optional `ack_duplicate=1` | new 409 codes: `policy_not_eligible`, `policy_position_taken`, `policy_employer_limit`, `duplicate_unacknowledged` |
| POST `/applications/{id}/handoff-complete` | re-checks policy | new 409 `policy_changed` |
| GET `/api/v1/status` | adds `{positions, listings, merge_queue_open}` | scope `read` |

## 5. Proposed additions to the spec (to be applied in the second PR)

New functional requirements (FR-079 onward):

- **FR-079**: Every Listing MUST belong to exactly one Position; a Position is the single canonical unit of triage, scoring display and duplicate-application control.
- **FR-080**: Position membership MUST be derived by a versioned deterministic resolver; `dup_of` MUST remain a maintained mirror until its removal is separately approved.
- **FR-081**: Employer, title, location/remote-scope and apply-URL normalisation MUST be pure, tested functions; ATS identity extraction patterns MUST be enabled only after a labelled real-capture golden test passes.
- **FR-082**: Automatic merges MUST require hard evidence or soft evidence at or above a published threshold and MUST pass hard guards (seniority, location, technology, employer); uncertain pairs MUST go to a human queue.
- **FR-083**: The resolver MUST be evaluated on real postings by an independent adjudicator; synthetic pairs MAY exercise logic but MUST NOT satisfy any success criterion.
- **FR-084**: The console MUST show merged fields with per-field provider provenance and attribution.
- **FR-085**: Each added provider MUST satisfy the adapter checklist (terms read and recorded, limits, allow-list, live contract test, attribution).
- **FR-086**: Postings from sources whose terms forbid automation MUST enter only by manual operator intake; the system MUST NOT scrape them.
- **FR-087**: Providers MUST carry a capability profile and a deterministic, versioned quality rank.
- **FR-088**: An apply policy (`all_aware`, `single_auto`, `single_favourites`, per-Position override) MUST restrict, and MUST NOT relax, any FR-026 condition; single modes MUST be race-safe at the database level.
- **FR-089**: Duplicate applications to one Position MUST be visible and require explicit acknowledgement; employer policies (per-Position, per-window, cooldown) MUST be enforceable.
- **FR-090**: Policy and provider settings MUST be operable in the console in all UI languages.
- **FR-091**: Merge and split MUST be audited, reversible via constraints, and MUST NOT run while an application on the Position is `live_approved`.
- **FR-092**: Resolver changes MUST be previewable by a dry run that writes nothing.
- **FR-093**: The HTTP contract MUST list every new route and 409 code.
- **FR-094**: Export, delete and retention MUST cover Positions, employers, policies and sealed signatures.

New success criteria:

- **SC-024**: On the real corpus, zero automatic merges are judged false by the independent adjudicator; the queue size and cross-provider overlap are reported (PARTIAL until a corpus contains real cross-provider pairs).
- **SC-025**: Six named bypass attempts against G9a-d, the policy-change re-check and override-with-live-approval are each refused with zero provider-side mutation.
- **SC-026**: Two concurrent approvals of different Listings of one Position in a single mode: exactly one succeeds, 100 of 100 trials.
- **SC-027**: Re-resolution twice yields identical Positions; arrival order permutations yield identical Positions (property test, 1,000 cases).
- **SC-028**: Each ATS adapter is shown by a live read to produce an ATS identity that matches the apply URL of a second provider's listing of the same job, or the pattern stays disabled and the SC stays PARTIAL.
- **SC-029**: Provider rank renders "insufficient history" for n < 10 and never reports an outcome-based claim below that.

Task additions: the table in section 3 (JSK-T188..JSK-T219).

## 6. Operator decisions needed (not taken here)

1. Himalayas ToS/key (already open from T048).
2. Whether to register a hh.ru application (RU coverage); acceptable to rely on manual intake for RU and SR?
3. Companies to follow via ATS boards (a data file of `{ats, board}` pairs); initial list of target employers per area.
4. Adopt the starter employer-policy profile or keep none by default.
5. Whether embeddings (local nomic) may be enabled for dedup.
6. Acceptance that LinkedIn/Indeed-originated jobs enter only through manual intake.
7. Removal of `dup_of` eventually (kept for now, section 11.4.122).

## 7. Risks

- ATS regex wrongly enabled -> false merges (mitigated by the golden-capture gate).
- Provider terms change (all cards are dated 2026-11-24; the adapter checklist re-reads terms at implementation time).
- Over-reliance on community-documented keys (Bundesagentur).
- Scope creep: the Position model touches ingest, match, console and apply; ship P1 behind read-only display before P4 changes any gate.
