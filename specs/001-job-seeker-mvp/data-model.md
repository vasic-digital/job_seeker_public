# Data Model: job_seeker MVP

| | |
|---|---|
| Feature | `specs/001-job-seeker-mvp` |
| Engine | SQLite (WAL, FK on, busy_timeout 5000). All timestamps UTC ISO-8601. |
| Encryption legend | **E** = envelope-encrypted column (AES-256-GCM, AAD `table|column|id`). Unmarked columns are plaintext. |
| Privacy rule | No column, default, check or comment in the public schema may hold an overlay VALUE. Private values (salary floor, tz window, allowed cities, blocked companies) are read from the overlay file into memory only; the DB stores booleans `overlay_present` and derived scores. |

## Tables

```sql
provider(id INTEGER PK, name TEXT UNIQUE, base_url TEXT, kind TEXT, rate_limit_rpm REAL, daily_cap INTEGER,
         enabled INTEGER NOT NULL DEFAULT 1, legal_notice_sha TEXT, circuit_state TEXT, next_allowed_at TEXT)

limiter_event(id PK, provider_id FK, at TEXT)            -- persisted request times; restarts cannot exceed limits

run(id PK, kind TEXT, started_at, ended_at, provider_id FK NULL, requests INT, ok INT, errors INT,
    git_rev TEXT, config_sha TEXT)

provider_call(id PK, run_id FK, url_host TEXT, resolved_ip TEXT, tls_subject TEXT, method TEXT, status INT,
              req_id TEXT, latency_ms INT, resp_sha256 TEXT, ts TEXT)          -- real-call proof, no bodies

posting(id PK, provider_id FK, provider_job_id TEXT, idem_key TEXT UNIQUE, canonical_url TEXT, source_url TEXT, apply_url TEXT /*verbatim, e.g. Himalayas applicationLink*/,
        state TEXT CHECK(state IN('discovered','mapped','ambiguous','unmapped','scored','shortlisted','discarded','expired')),
        state_changed_at TEXT, title TEXT,
        company TEXT, location_raw TEXT, remote_mode TEXT, countries_json TEXT, tz_hint TEXT,
        salary_min INT, salary_max INT, salary_cur TEXT, salary_period TEXT, tags_json TEXT,
        body_text E, lang TEXT, lang_conf REAL, posted_at TEXT, expires_at TEXT,
        first_seen TEXT, last_seen TEXT, fetched_at TEXT, raw_sha256 TEXT, dup_of INTEGER FK NULL,
        UNIQUE(provider_id, provider_job_id))

mapping(id PK, posting_id FK, is_current INTEGER NOT NULL, area_id TEXT NULL,
        status TEXT CHECK(status IN('mapped','ambiguous','unmapped')), confidence REAL,
        method TEXT CHECK(method IN('rules','rules+llm','human')), s1 REAL, s2 REAL, hits_json TEXT,
        ruleset_sha TEXT, llm_request_id TEXT, llm_model TEXT, mapped_by TEXT, mapped_at TEXT, human_rev INTEGER NOT NULL DEFAULT 0,
        UNIQUE(posting_id, ruleset_sha, method, llm_model, human_rev))
        -- append-only history; partial UNIQUE INDEX(posting_id) WHERE is_current=1; identical re-runs upsert; a new ruleset or model, or each human re-map (human_rev+1), appends

score(id PK, posting_id FK, score_version TEXT, total REAL, components_json E, gates_json E,
      explain_json E, ruleset_sha TEXT, overlay_present INTEGER, scored_at TEXT,
      is_current INTEGER NOT NULL, UNIQUE(posting_id, score_version, ruleset_sha))     -- partial UNIQUE INDEX(posting_id) WHERE is_current=1

application(id PK, posting_id FK UNIQUE, idem_key TEXT UNIQUE, state TEXT CHECK(state IN(
        'pending_draft','draft_prepared','awaiting_human_approval','live_approved','sent','dry_run_recorded',
        'rejected_by_human','tracked','expired','failed_retryable','failed_terminal','withdrawn')),
        -- the application row is created when a posting is shortlisted; pre-application states live in posting.state; the application row starts at pending_draft
        state_reason TEXT /*closed codes, e.g. slot_missing*/, area_id TEXT, cv_slot TEXT, cover_slot TEXT, lang TEXT, lang_rule TEXT, draft_hash TEXT,
        dry_run INTEGER NOT NULL DEFAULT 1, approved_by TEXT, approved_at TEXT, approved_hash TEXT,
        sent_at TEXT, provider_ref E, follow_up_at TEXT, created_at TEXT, updated_at TEXT)

draft(id PK, application_id FK, kind TEXT CHECK(kind IN('cv','cover')), slot TEXT, lang TEXT,
      content_sha256 TEXT, path_ref E, created_at TEXT)   -- path_ref points into the private dir; never a PDF blob

approval(id PK, application_id FK, approver TEXT, decision TEXT CHECK(decision IN('approve','reject')),
         draft_hash TEXT, step_up_at TEXT, reason E, ts TEXT, superseded_at TEXT NULL)   -- a draft-hash change supersedes, never deletes

submission(id PK, application_id FK, idem_key TEXT UNIQUE /*sha256(application, kind, cycle)*/, kind TEXT CHECK(kind IN('would_send','handoff')),
           payload E, written_at TEXT, reconciled_at TEXT NULL, outcome TEXT)   -- written BEFORE any outbound step (FR-032)

audit_log(id PK, application_id INTEGER NULL, posting_id INTEGER NULL /*subject ids, NO foreign keys*/, from_state TEXT, to_state TEXT, actor TEXT,
          reason_code TEXT /*closed vocabulary*/, evidence_json TEXT /*ids and hashes only*/, prev_hash TEXT, this_hash TEXT, ts TEXT)
          -- trigger blocks UPDATE and DELETE; audit rows survive deletion of their subjects (ids only, no content), so pruning and per-record delete are possible; the log is kept for the life of the DB; the periodic anchor lives in a separate append-only file under the backup directory (strength recorded as `policy` unless the filesystem enforces append-only); this_hash = sha256(prev_hash || canonical row)

tracking(id PK, application_id FK, status TEXT CHECK(status IN('sent','viewed','replied','interview',
         'offer','rejected','ghosted')), source TEXT, note E, ts TEXT)

passkey(id PK, credential_id BLOB UNIQUE, public_key BLOB, sign_count INT, aaguid TEXT, transports TEXT,
        label TEXT, created_at TEXT, last_used_at TEXT)
session(id TEXT PK /*sha256 of 256-bit id*/, created_at, last_seen_at, idle_expires, abs_expires, step_up_at, csrf_hash)
api_token(id PK, sha256 TEXT UNIQUE, scope TEXT CHECK(scope IN('read','dryrun')), label, created_at, revoked_at)
setting(key TEXT PK, value TEXT)           -- kill_switch, live_mode (reset on boot), caps, retention, ui_language
key_envelope(id PK CHECK(id=1), kdf TEXT, kdf_params_json TEXT, salt BLOB, wrapped_dek BLOB, version INT)
tombstone(id PK, kind TEXT, count INT, ts TEXT)       -- content-free deletion record

jobops_job(id PK, jobops_id TEXT UNIQUE, title TEXT, employer TEXT, url TEXT, source TEXT, score INT NULL,
           status TEXT, updated_at TEXT, revision TEXT)          -- read model, optional (only if spike GO)

schema_migration(version INT PK, applied_at TEXT, backup_ref TEXT)
```

## Derived / non-DB data

| Data | Home | Notes |
|---|---|---|
| Overlay (salary floor and period, tz window, allowed cities, blocked companies, eligible countries, identity extras) | `career_private`, read through `CAREER_PRIVATE_DIR`, memory only | key allow-list enforced; unknown key rejected |
| Public career snapshot (area keywords, skill tokens) | `rules/career_snapshot/` (tracked, public) | source commit and SHA recorded |
| Rules | `rules/area_rules.yaml`, `rules/score.yaml` | `ruleset_sha` = SHA-256 of both |
| Template slots | manifest in private dir; resolved to (area, lang, kind) -> path + sha | not stored in DB beyond hash and path_ref |
| Message catalogue | `internal/i18n/messages/<lang>.json` | review verdicts in `evidence/i18n/<lang>/verdict.json` |

## Invariants (each has a test)

1. `application.state='sent'` implies exactly one NON-superseded `approval` row with `decision='approve'`, `draft_hash = application.draft_hash = approved_hash`, `dry_run=0`, and a `submission` row of kind `handoff`.
2. No `audit_log` row is UPDATE-able or DELETE-able; the chain verifies end to end.
3. Every state change and its audit row commit in one transaction.
4. `mapping.area_id IS NULL` iff `status='unmapped'` or an ambiguous row awaiting a human.
5. `posting.dup_of` never points to a row that itself has `dup_of`.
6. No overlay value is representable in any column default or CHECK.
7. Sensitive (E) columns are never plaintext in the DB file.

## State machine (summary)

Posting states: `discovered -> mapped|ambiguous|unmapped`; `mapped -> scored -> shortlisted (score >= 70, no gate) | discarded (score < 55 or gate)`; the review band 55 <= score < 70 is promoted by a human. Application states (row created at shortlist): `draft_prepared (slot resolvable) -> awaiting_human_approval (draft hash frozen) -> dry_run_recorded | rejected_by_human | expired (14 d) | live_approved (live approval recorded, handoff pending) -> sent (operator records confirmation)`; `sent|dry_run_recorded -> tracked`; `dry_run_recorded -> awaiting_human_approval` starts a fresh approval cycle for going live (the earlier dry-run approval is superseded and the submission key derives from `(application, kind, cycle)`); `ambiguous|unmapped -> mapped` by human map or accepted tiebreak; the application machine starts at `pending_draft`; a draft-hash change after approval supersedes the approval and returns to `draft_prepared`. `scored` is a posting state, as in FR-024. Transition table is data and unit-tested for every illegal pair.


## Foreign-key delete behaviour

posting -> mapping/score/application/draft: ON DELETE CASCADE for mapping, score; RESTRICT for application while it exists (per-record delete removes the application subtree explicitly in one transaction). application -> draft/approval/submission/tracking: CASCADE. audit_log: no FK. provider -> posting: RESTRICT. `gates_json` records generic gate ids only (e.g. `blocked_company`), never the matching overlay value; `explain_json` stores `floor_used: present|absent`, never the number.

## Position layer (migration 0002_position, JSK-T189; design: docs/research/providers/03)

New tables `employer`, `employer_alias`, `position`, `position_link`, `merge_candidate`, `position_constraint`, `provider_profile`, `employer_policy`, `position_apply_lock`; new nullable columns on `posting` (`position_id`, `ats_kind`, `ats_tenant`, `ats_job_id`, `apply_url_canon`, `employer_id`, `sig_sealed` (E)) and `application` (`position_id`, `policy_sha`). `position.state` is the closed vocabulary `fsm.PositionStates` (open, expired, merged; CHECK equality tested). `dup_of` is kept. Backfill is JSK-T192.
