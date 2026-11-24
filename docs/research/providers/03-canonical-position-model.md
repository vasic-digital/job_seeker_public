# 03 - Canonical Position model

| | |
|---|---|
| Status | Design proposal (docs-only); nothing here is implemented |
| Builds on | `internal/ingest` (dup_of at 0.92, `markDup`), `internal/store/migrations/0001_init.sql`, `internal/fsm`, `specs/001-job-seeker-mvp/data-model.md` |
| Migration number | 0002 is the next free (re-checked 2026-11-24: only `0001_init.sql` exists on `github/main`, and no open PR adds a migration). Whoever merges a migration first takes it; the other renumbers at merge |

## 1. Concepts

- **Listing**: one provider's record of a job. This is today's `posting` row; we keep the table and name (no rename churn) and call it Listing in prose.
- **Position**: one real-world open job at one employer. N Listings attach to exactly one Position. A Position with a single Listing is the common case.
- **Employer** (`company` entity): normalised company with aliases, domains and ATS tenant ids.
- **Provider profile**: per-provider capability and quality record used for ranking.

Ownership split (so existing gates keep their meaning):

| Concern | Lives on | Why |
|---|---|---|
| Fetch, sanitize, idempotency, retention, attribution | Listing (`posting`) | per-provider ToS and idem key |
| Dedup identity, merged display fields, triage (shortlist/dismiss) | Position | the operator decides about the job, once |
| Area mapping and scoring | computed from the Position's *merged view*, stored against the representative Listing | avoids scoring the same job N times; see 3.3 |
| Draft, approval, handoff, tracking, `application` | Listing (one application per Listing, existing `posting_id UNIQUE`) | an application is a concrete handoff to one provider's apply URL |
| Duplicate-application warnings and policy | Position | spans Listings |

## 2. Data model sketch (migration `0002_position.sql`)

```sql
-- Employers
CREATE TABLE employer (
  id INTEGER PRIMARY KEY, name_norm TEXT NOT NULL UNIQUE, display_name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'unknown' CHECK (kind IN ('direct','agency','unknown')),
  created_at TEXT NOT NULL
);
CREATE TABLE employer_alias (
  id INTEGER PRIMARY KEY, employer_id INTEGER NOT NULL REFERENCES employer(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('name','domain','ats_tenant')),   -- ats_tenant value: 'greenhouse:acme'
  value TEXT NOT NULL, source TEXT NOT NULL CHECK (source IN ('auto','human')),
  UNIQUE (kind, value)
);

-- Positions
CREATE TABLE position (
  id INTEGER PRIMARY KEY, employer_id INTEGER REFERENCES employer(id) ON DELETE SET NULL,
  state TEXT NOT NULL CHECK (state IN ('open','expired','merged')),          -- 'merged' rows keep merged_into
  merged_into INTEGER REFERENCES position(id), CHECK (merged_into IS NULL OR merged_into <> id),
  title_norm TEXT NOT NULL, seniority TEXT, remote_scope_json TEXT,           -- {mode, countries[], tz_from, tz_to}
  posted_first TEXT, posted_last TEXT, first_seen TEXT NOT NULL, last_seen TEXT NOT NULL,
  representative_listing_id INTEGER REFERENCES posting(id) ON DELETE SET NULL,
  resolver_version TEXT NOT NULL,
  apply_override TEXT NOT NULL DEFAULT 'inherit' CHECK (apply_override IN ('inherit','all','single','none')),
  apply_override_listing INTEGER REFERENCES posting(id) ON DELETE SET NULL,
  apply_override_reason TEXT
);
CREATE INDEX position_employer ON position(employer_id, state);

ALTER TABLE posting ADD COLUMN position_id INTEGER REFERENCES position(id) ON DELETE SET NULL;
ALTER TABLE posting ADD COLUMN ats_kind TEXT;           -- greenhouse|lever|ashby|workable|smartrecruiters|...
ALTER TABLE posting ADD COLUMN ats_tenant TEXT;
ALTER TABLE posting ADD COLUMN ats_job_id TEXT;
ALTER TABLE posting ADD COLUMN apply_url_canon TEXT;
ALTER TABLE posting ADD COLUMN employer_id INTEGER REFERENCES employer(id);
ALTER TABLE posting ADD COLUMN sig_sealed BLOB;         -- (E) minhash + optional embedding, derived from body
CREATE INDEX posting_position ON posting(position_id);
CREATE INDEX posting_ats_identity ON posting(ats_kind, ats_tenant, ats_job_id)   -- lookup only, NOT unique:
  WHERE ats_job_id IS NOT NULL;                                                   -- a provider may re-list the same ATS job under a new id

-- Resolution evidence, human-review queue, constraints
CREATE TABLE position_link (            -- the evidence for attaching a Listing (or merging two Positions)
  id INTEGER PRIMARY KEY, listing_id INTEGER NOT NULL REFERENCES posting(id) ON DELETE CASCADE,
  position_id INTEGER NOT NULL REFERENCES position(id) ON DELETE CASCADE,
  decision TEXT NOT NULL CHECK (decision IN ('auto','human','new')), score REAL, features_json TEXT NOT NULL,
  resolver_version TEXT NOT NULL, is_current INTEGER NOT NULL CHECK (is_current IN (0,1)), ts TEXT NOT NULL
);
CREATE UNIQUE INDEX position_link_current ON position_link(listing_id) WHERE is_current = 1;
CREATE TABLE merge_candidate (          -- the human-review queue (uncertain band)
  id INTEGER PRIMARY KEY, a_listing INTEGER NOT NULL REFERENCES posting(id) ON DELETE CASCADE,
  b_listing INTEGER NOT NULL REFERENCES posting(id) ON DELETE CASCADE, score REAL NOT NULL, features_json TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','merged','kept_apart','stale')),
  decided_by TEXT, decided_at TEXT, UNIQUE (a_listing, b_listing), CHECK (a_listing < b_listing)
);
CREATE TABLE position_constraint (      -- human decisions that every future re-resolution MUST respect
  id INTEGER PRIMARY KEY, a_listing INTEGER NOT NULL, b_listing INTEGER NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('must_link','cannot_link')), actor TEXT NOT NULL, ts TEXT NOT NULL,
  UNIQUE (a_listing, b_listing), CHECK (a_listing < b_listing)
);

-- Provider capability profile and apply policy (see 05)
CREATE TABLE provider_profile (
  provider_id INTEGER PRIMARY KEY REFERENCES provider(id) ON DELETE CASCADE,
  apply_kind TEXT NOT NULL CHECK (apply_kind IN ('ats_native','direct_form','redirect','email','none','unknown')),
  attribution_required INTEGER NOT NULL DEFAULT 1 CHECK (attribution_required IN (0,1)),
  data_completeness REAL, notes TEXT
);
CREATE TABLE employer_policy (
  employer_id INTEGER PRIMARY KEY REFERENCES employer(id) ON DELETE CASCADE,
  one_per_position INTEGER NOT NULL DEFAULT 0 CHECK (one_per_position IN (0,1)),
  max_per_window INTEGER, window_days INTEGER, cooldown_days INTEGER, note TEXT
);
CREATE TABLE position_apply_lock (      -- single-provider modes: at most one live application per Position
  position_id INTEGER PRIMARY KEY REFERENCES position(id) ON DELETE CASCADE,
  listing_id INTEGER NOT NULL REFERENCES posting(id), application_id INTEGER NOT NULL REFERENCES application(id) ON DELETE CASCADE
);
ALTER TABLE application ADD COLUMN position_id INTEGER REFERENCES position(id);
ALTER TABLE application ADD COLUMN policy_sha TEXT;   -- digest of the policy decision taken at approval
-- settings (rows in existing `setting`): apply_mode, provider_favourites (ordered JSON), favourites_strict
```

Backfill (inside the migration or a `jsctl migrate-positions` step, idempotent): for every existing `posting` create a Position;
for every row with `dup_of IS NOT NULL` attach it to the root's Position (`position_link.decision='auto'`, features `{legacy_dup_of:true}`).
Backfill creates one Position per posting except where `dup_of` links them (the legacy `markDup` only linked across providers, so no same-provider links exist yet). At resolve time the mirror is maintained for **all** members, including same-provider members sharing an ATS identity. `dup_of` is **kept** and maintained as a derived mirror (listing -> representative of its Position) so existing readers, the console filter and
invariant 5 ("`dup_of` never points at a row that has `dup_of`") keep holding. Removing `dup_of` is a later, separately approved decision (section 11.4.122: nothing shipped is removed silently).

## 3. Invariants (each needs a test)

1. A Listing has at most one current `position_link`; its `position_id` equals that link's position.
2. A `merged` Position has no Listings; `merged_into` chains are acyclic and resolved to the root on write (like `dup_of`).
3. `posting.dup_of` is NULL for the representative of a Position and equals the representative id for every other member.
4. A `cannot_link` pair is never in the same Position after any resolver run; a `must_link` pair always is. Human constraints outrank all automatic evidence.
5. Two Listings of the same provider with different `provider_job_id` are never auto-merged unless they share an ATS identity or `must_link` (a provider genuinely may list two openings with one title).
6. Every Position membership change commits together with its audit row in one transaction (same discipline as state transitions).
7. In a single-provider apply mode, at most one `application` per Position reaches `live_approved` or `sent` (enforced by `position_apply_lock` PK, not by application code alone).
8. Body-derived signatures are sealed (E) and removed with the Listing; a Position with zero Listings is deleted (or tombstoned) by retention.

### 3.1 Position state

`open` while at least one member Listing is not `expired`; `expired` when all are; `merged` when absorbed. Defined as data in `internal/fsm` next to the posting/application tables (new `PositionStates`, `PositionCan`) per FR-024.

### 3.2 Merged view (what the console shows)

Per field, choose by documented precedence, remember provenance:
title/company/location from the highest-ranked Listing with a non-empty value; salary from the Listing with `salary_min` set (prefer ATS-native); `posted_first` = min, `posted_last` = max; remote scope = union of countries, intersection of tz when both present; apply URL candidates = all Listing apply URLs, canonicalised. Every merged field stores `from_listing` so the operator sees why.

### 3.3 Mapping and scoring

`internal/match` runs on the merged view of the Position, written against `representative_listing_id` (so `mapping`/`score` tables and their unique-current indexes need no schema change). When the representative changes (new better Listing, expiry), scoring is re-run only if merged-field hash changed. Shortlist/dismiss act on the Position and fan out to Listings as audited state changes. UNCONFIRMED: how `matchstore` keys its queue; the implementation task (T-P09) starts with a read of `internal/matchstore` and may adjust this paragraph.

## 4. Go sketches

```go
package position

type ID int64

type Listing struct { // projection of posting
    ID, ProviderID int64
    ProviderJobID  string
    Title, Company string
    LocationRaw    string
    ApplyURL, SourceURL string
    PostedAt       time.Time
    ATS            ATSIdentity // Kind, Tenant, JobID
    Sig            Signature   // minhash [128]uint32, optional embedding []float32
}

type Resolver interface {
    // Resolve attaches l to an existing Position, creates one, or queues a merge candidate. Pure given (store state, version).
    Resolve(ctx context.Context, tx *sql.Tx, l Listing) (Outcome, error)
    Merge(ctx context.Context, tx *sql.Tx, actor string, keep, absorb ID, reason string) error
    Split(ctx context.Context, tx *sql.Tx, actor string, listing int64, reason string) (ID, error)
}

type Outcome struct {
    Position ID
    Decision string  // auto|new|queued
    Score    float64
    Features map[string]float64
}
```

`Runner.ingestBatch` calls `Resolver.Resolve` instead of `markDup` for each `new` listing (the legacy function stays as a stage-0 blocker-equivalent and is covered by its existing tests until removed by an approved task).

## 5. Interactions with existing machinery

- **FSM**: `application` states unchanged. Policy eligibility is a precondition of `draft_prepared -> awaiting_human_approval` and re-checked at `live_approved -> sent` (05).
- **Hash approval (FR-026)**: draft hash unchanged; approval evidence adds `listing_id` and `policy_sha`. Conditions 1-8 are untouched; policy is condition 9, a *restriction only*.
- **Kill switch / caps**: unchanged and evaluated first. Caps count per Listing (provider host), plus the new employer cap.
- **Tracker**: `tracking` rows stay per application; Position page aggregates.
- **Retention/delete/export**: `data_delete` per record cascades Listing -> `position_link` -> constraints referencing it; export includes positions and policies.
- **Attribution (FR-004)**: every merged field shows the originating provider chip with link; a Position never hides that a field came from a provider whose terms demand attribution. Providers whose terms forbid re-display beyond TTL (WWR via search) keep their Listing body out of the merged view after TTL.
