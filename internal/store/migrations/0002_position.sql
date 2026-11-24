-- 0002_position: canonical Position/Listing layer (JSK-016, docs/research/providers/03). Listing = today's `posting` row.
-- Closed vocabularies only (the overlay-literal test covers this file). Columns marked (E) hold sealed BLOBs.
-- Position.state mirrors fsm.PositionStates (tested). Backfill from dup_of is a separate idempotent job (JSK-T192).

CREATE TABLE employer (
  id INTEGER PRIMARY KEY, name_norm TEXT NOT NULL UNIQUE, display_name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'unknown' CHECK (kind IN ('direct','agency','unknown')),
  created_at TEXT NOT NULL
);
CREATE TABLE employer_alias (
  id INTEGER PRIMARY KEY, employer_id INTEGER NOT NULL REFERENCES employer(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('name','domain','ats_tenant')),
  value TEXT NOT NULL, source TEXT NOT NULL CHECK (source IN ('auto','human')),
  UNIQUE (kind, value)
);

CREATE TABLE position (
  id INTEGER PRIMARY KEY, employer_id INTEGER REFERENCES employer(id) ON DELETE SET NULL,
  state TEXT NOT NULL CHECK (state IN ('open','expired','merged')),
  merged_into INTEGER REFERENCES position(id),
  title_norm TEXT NOT NULL, seniority TEXT, remote_scope_json TEXT,
  posted_first TEXT, posted_last TEXT, first_seen TEXT NOT NULL, last_seen TEXT NOT NULL,
  representative_listing_id INTEGER REFERENCES posting(id) ON DELETE SET NULL,
  resolver_version TEXT NOT NULL,
  apply_override TEXT NOT NULL DEFAULT 'inherit' CHECK (apply_override IN ('inherit','all','single','none')),
  apply_override_listing INTEGER REFERENCES posting(id) ON DELETE SET NULL,
  apply_override_reason TEXT,
  CHECK (merged_into IS NULL OR merged_into <> id)
);
CREATE INDEX position_employer ON position(employer_id, state);

ALTER TABLE posting ADD COLUMN position_id INTEGER REFERENCES position(id) ON DELETE SET NULL;
ALTER TABLE posting ADD COLUMN ats_kind TEXT;
ALTER TABLE posting ADD COLUMN ats_tenant TEXT;
ALTER TABLE posting ADD COLUMN ats_job_id TEXT;
ALTER TABLE posting ADD COLUMN apply_url_canon TEXT;
ALTER TABLE posting ADD COLUMN employer_id INTEGER REFERENCES employer(id);
ALTER TABLE posting ADD COLUMN sig_sealed BLOB;      -- (E) minhash + optional embedding, derived from the body
CREATE INDEX posting_position ON posting(position_id);
CREATE INDEX posting_ats_identity ON posting(ats_kind, ats_tenant, ats_job_id) WHERE ats_job_id IS NOT NULL;  -- lookup only, NOT unique

CREATE TABLE position_link (
  id INTEGER PRIMARY KEY, listing_id INTEGER NOT NULL REFERENCES posting(id) ON DELETE CASCADE,
  position_id INTEGER NOT NULL REFERENCES position(id) ON DELETE CASCADE,
  decision TEXT NOT NULL CHECK (decision IN ('auto','human','new')), score REAL, features_json TEXT NOT NULL,
  resolver_version TEXT NOT NULL, is_current INTEGER NOT NULL CHECK (is_current IN (0,1)), ts TEXT NOT NULL
);
CREATE UNIQUE INDEX position_link_current ON position_link(listing_id) WHERE is_current = 1;

CREATE TABLE merge_candidate (
  id INTEGER PRIMARY KEY, a_listing INTEGER NOT NULL REFERENCES posting(id) ON DELETE CASCADE,
  b_listing INTEGER NOT NULL REFERENCES posting(id) ON DELETE CASCADE, score REAL NOT NULL, features_json TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','merged','kept_apart','stale')),
  decided_by TEXT, decided_at TEXT, UNIQUE (a_listing, b_listing), CHECK (a_listing < b_listing)
);

CREATE TABLE position_constraint (
  id INTEGER PRIMARY KEY, a_listing INTEGER NOT NULL REFERENCES posting(id) ON DELETE CASCADE,
  b_listing INTEGER NOT NULL REFERENCES posting(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('must_link','cannot_link')), actor TEXT NOT NULL, ts TEXT NOT NULL,
  UNIQUE (a_listing, b_listing), CHECK (a_listing < b_listing)
);

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
CREATE TABLE position_apply_lock (
  position_id INTEGER PRIMARY KEY REFERENCES position(id) ON DELETE CASCADE,
  listing_id INTEGER NOT NULL REFERENCES posting(id),
  application_id INTEGER NOT NULL REFERENCES application(id) ON DELETE CASCADE
);
ALTER TABLE application ADD COLUMN position_id INTEGER REFERENCES position(id);
ALTER TABLE application ADD COLUMN policy_sha TEXT;
