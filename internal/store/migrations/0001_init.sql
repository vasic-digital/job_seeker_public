-- 0001_init: data-model.md in full. Columns marked (E) hold AES-256-GCM sealed BLOBs
-- (internal/crypto, AAD table|column|id). No default or CHECK may carry an overlay value:
-- only closed state vocabularies and integer flags appear as literals (tested).

CREATE TABLE provider (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, base_url TEXT NOT NULL, kind TEXT NOT NULL,
  rate_limit_rpm REAL, daily_cap INTEGER, enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  legal_notice_sha TEXT, circuit_state TEXT NOT NULL DEFAULT 'closed' CHECK (circuit_state IN ('closed','open','half_open')),
  next_allowed_at TEXT
);

CREATE TABLE limiter_event (
  id INTEGER PRIMARY KEY, provider_id INTEGER NOT NULL REFERENCES provider(id) ON DELETE RESTRICT, at TEXT NOT NULL
);
CREATE INDEX limiter_event_provider_at ON limiter_event(provider_id, at);

CREATE TABLE run (
  id INTEGER PRIMARY KEY, kind TEXT NOT NULL, started_at TEXT NOT NULL, ended_at TEXT,
  provider_id INTEGER REFERENCES provider(id) ON DELETE RESTRICT,
  requests INTEGER NOT NULL DEFAULT 0, ok INTEGER NOT NULL DEFAULT 0, errors INTEGER NOT NULL DEFAULT 0,
  git_rev TEXT, config_sha TEXT
);

CREATE TABLE provider_call (
  id INTEGER PRIMARY KEY, run_id INTEGER NOT NULL REFERENCES run(id) ON DELETE CASCADE,
  url_host TEXT NOT NULL, resolved_ip TEXT, tls_subject TEXT, method TEXT NOT NULL, status INTEGER,
  req_id TEXT, latency_ms INTEGER, resp_sha256 TEXT, ts TEXT NOT NULL
);

CREATE TABLE posting (
  id INTEGER PRIMARY KEY, provider_id INTEGER NOT NULL REFERENCES provider(id) ON DELETE RESTRICT,
  provider_job_id TEXT NOT NULL, idem_key TEXT NOT NULL UNIQUE, canonical_url TEXT, source_url TEXT, apply_url TEXT,
  state TEXT NOT NULL CHECK (state IN ('discovered','mapped','ambiguous','unmapped','scored','shortlisted','discarded','expired')),
  state_changed_at TEXT NOT NULL, title TEXT NOT NULL,
  company TEXT, location_raw TEXT, remote_mode TEXT, countries_json TEXT, tz_hint TEXT,
  salary_min INTEGER, salary_max INTEGER, salary_cur TEXT, salary_period TEXT, tags_json TEXT,
  body_text BLOB,                      -- (E)
  lang TEXT, lang_conf REAL, posted_at TEXT, expires_at TEXT,
  first_seen TEXT NOT NULL, last_seen TEXT NOT NULL, fetched_at TEXT, raw_sha256 TEXT,
  dup_of INTEGER REFERENCES posting(id) ON DELETE SET NULL CHECK (dup_of IS NULL OR dup_of <> id),
  UNIQUE (provider_id, provider_job_id)
);
CREATE INDEX posting_state ON posting(state);
CREATE INDEX posting_dup ON posting(dup_of);

CREATE TABLE mapping (
  id INTEGER PRIMARY KEY, posting_id INTEGER NOT NULL REFERENCES posting(id) ON DELETE CASCADE,
  is_current INTEGER NOT NULL CHECK (is_current IN (0,1)), area_id TEXT,
  status TEXT NOT NULL CHECK (status IN ('mapped','ambiguous','unmapped')), confidence REAL,
  method TEXT NOT NULL CHECK (method IN ('rules','rules+llm','human')), s1 REAL, s2 REAL, hits_json TEXT,
  ruleset_sha TEXT NOT NULL, llm_request_id TEXT, llm_model TEXT NOT NULL DEFAULT '', mapped_by TEXT, mapped_at TEXT NOT NULL,
  human_rev INTEGER NOT NULL DEFAULT 0,
  CHECK ((area_id IS NULL) = (status = 'unmapped') OR status = 'ambiguous'),
  UNIQUE (posting_id, ruleset_sha, method, llm_model, human_rev)
);
CREATE UNIQUE INDEX mapping_current ON mapping(posting_id) WHERE is_current = 1;

CREATE TABLE score (
  id INTEGER PRIMARY KEY, posting_id INTEGER NOT NULL REFERENCES posting(id) ON DELETE CASCADE,
  score_version TEXT NOT NULL, total REAL NOT NULL,
  components_json BLOB, gates_json BLOB, explain_json BLOB,      -- (E)
  ruleset_sha TEXT NOT NULL, overlay_present INTEGER NOT NULL CHECK (overlay_present IN (0,1)), scored_at TEXT NOT NULL,
  is_current INTEGER NOT NULL CHECK (is_current IN (0,1)),
  UNIQUE (posting_id, score_version, ruleset_sha)
);
CREATE UNIQUE INDEX score_current ON score(posting_id) WHERE is_current = 1;

CREATE TABLE application (
  id INTEGER PRIMARY KEY, posting_id INTEGER NOT NULL UNIQUE REFERENCES posting(id) ON DELETE RESTRICT,
  idem_key TEXT NOT NULL UNIQUE,
  state TEXT NOT NULL CHECK (state IN ('pending_draft','draft_prepared','awaiting_human_approval','live_approved','sent','dry_run_recorded',
        'rejected_by_human','tracked','expired','failed_retryable','failed_terminal','withdrawn')),
  state_reason TEXT, area_id TEXT, cv_slot TEXT, cover_slot TEXT, lang TEXT, lang_rule TEXT, draft_hash TEXT,
  dry_run INTEGER NOT NULL DEFAULT 1 CHECK (dry_run IN (0,1)), approved_by TEXT, approved_at TEXT, approved_hash TEXT,
  sent_at TEXT, provider_ref BLOB,                               -- (E)
  follow_up_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);

CREATE TABLE draft (
  id INTEGER PRIMARY KEY, application_id INTEGER NOT NULL REFERENCES application(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('cv','cover')), slot TEXT NOT NULL, lang TEXT NOT NULL,
  content_sha256 TEXT NOT NULL, path_ref BLOB,                   -- (E)
  created_at TEXT NOT NULL
);

CREATE TABLE approval (
  id INTEGER PRIMARY KEY, application_id INTEGER NOT NULL REFERENCES application(id) ON DELETE CASCADE,
  approver TEXT NOT NULL, decision TEXT NOT NULL CHECK (decision IN ('approve','reject')),
  draft_hash TEXT, step_up_at TEXT, reason BLOB, ts TEXT NOT NULL, superseded_at TEXT   -- reason (E)
);
CREATE INDEX approval_app ON approval(application_id);

CREATE TABLE submission (
  id INTEGER PRIMARY KEY, application_id INTEGER NOT NULL REFERENCES application(id) ON DELETE CASCADE,
  idem_key TEXT NOT NULL UNIQUE, kind TEXT NOT NULL CHECK (kind IN ('would_send','handoff')),
  payload BLOB, written_at TEXT NOT NULL, reconciled_at TEXT, outcome TEXT                -- payload (E)
);

-- audit rows reference subjects by id only (no foreign keys) so they survive deletion of the subject.
CREATE TABLE audit_log (
  id INTEGER PRIMARY KEY, application_id INTEGER, posting_id INTEGER, from_state TEXT, to_state TEXT,
  actor TEXT NOT NULL, reason_code TEXT NOT NULL, evidence_json TEXT, prev_hash TEXT NOT NULL, this_hash TEXT NOT NULL UNIQUE, ts TEXT NOT NULL
);
CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log BEGIN SELECT RAISE(ABORT, 'audit_log is append-only'); END;
CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log BEGIN SELECT RAISE(ABORT, 'audit_log is append-only'); END;

CREATE TABLE tracking (
  id INTEGER PRIMARY KEY, application_id INTEGER NOT NULL REFERENCES application(id) ON DELETE CASCADE,
  status TEXT NOT NULL CHECK (status IN ('sent','viewed','replied','interview','offer','rejected','ghosted')),
  source TEXT, note BLOB, ts TEXT NOT NULL                                              -- note (E)
);

CREATE TABLE passkey (
  id INTEGER PRIMARY KEY, credential_id BLOB NOT NULL UNIQUE, public_key BLOB NOT NULL, sign_count INTEGER NOT NULL DEFAULT 0,
  aaguid TEXT, transports TEXT, label TEXT, created_at TEXT NOT NULL, last_used_at TEXT
);
CREATE TABLE session (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, idle_expires TEXT NOT NULL, abs_expires TEXT NOT NULL,
  step_up_at TEXT, csrf_hash TEXT NOT NULL
);
CREATE TABLE api_token (
  id INTEGER PRIMARY KEY, sha256 TEXT NOT NULL UNIQUE, scope TEXT NOT NULL CHECK (scope IN ('read','dryrun')),
  label TEXT, created_at TEXT NOT NULL, revoked_at TEXT
);
CREATE TABLE setting (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE key_envelope (
  id INTEGER PRIMARY KEY CHECK (id = 1), kdf TEXT NOT NULL, kdf_params_json TEXT NOT NULL, salt BLOB NOT NULL,
  wrapped_dek BLOB NOT NULL, version INTEGER NOT NULL
);
CREATE TABLE tombstone (id INTEGER PRIMARY KEY, kind TEXT NOT NULL, count INTEGER NOT NULL, ts TEXT NOT NULL);

CREATE TABLE jobops_job (
  id INTEGER PRIMARY KEY, jobops_id TEXT NOT NULL UNIQUE, title TEXT, employer TEXT, url TEXT, source TEXT,
  score INTEGER, status TEXT, updated_at TEXT, revision TEXT
);
