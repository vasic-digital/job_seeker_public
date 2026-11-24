# 23 - Data and migrations

| | |
|---|---|
| Part | C of the protocols research |
| Date | 2026-11-24 |
| Migration numbering | `internal/store/migrations/` on main holds only `0001_init.sql`. The provider research claims `0002_position.sql` ([providers/03](../providers/03-canonical-position-model.md) section 2). This part therefore proposes **`0003_ext_registry.sql`** and **`0004_ext_verdicts.sql`**; if the order of merging differs, renumber at merge time (the runner applies files in name order, `internal/store/store.go:145-163`), never edit an applied file. |
| Rules | additive only; no overlay value in any column or default (`0001_init.sql` header and the schema test `internal/store/schema_test.go`); sealed columns marked (E) use `internal/crypto` with AAD `table|column|id`; every migration is preceded by the verified backup (`store.ErrBackupFailed`) |

## Contents

1. [What is stored and what is not](#1-what-is-stored-and-what-is-not)
2. [`0003_ext_registry.sql`](#2-0003_ext_registrysql)
3. [`0004_ext_verdicts.sql`](#3-0004_ext_verdictssql)
4. [Settings keys](#4-settings-keys)
5. [Per-ATS profile data](#5-per-ats-profile-data)
6. [Retention, privacy, export](#6-retention-privacy-export)
7. [Migration tests](#7-migration-tests)

## 1. What is stored and what is not

| Thing | Stored? | Where | Why |
|---|---|---|---|
| Which plugins exist (id, kind, version, spec, caps) | yes, as a **snapshot at start-up** | `ext_plugin` | audit and discovery after the binary changed; registration remains code |
| Operator on/off, strategy choice | yes | `setting` rows (no new table) | already audited |
| Rule configs and per-ATS profiles | **no**: files under `rules/`, hashed into `ruleset_sha` | git | reviewed, versioned, no drift between DB and code |
| Verdicts (eligibility, compliance) per posting/application/stage | yes | `ext_verdict` | explainability, staleness detection, approval re-check evidence |
| Reasons | yes, as ids/codes only | `ext_verdict.reasons_json` | closed vocabulary |
| Profile values, overlay values, posting text | **never** | - | FR-053..FR-056 |
| Knockout confirmations | the private overlay (T177), not the DB | overlay | private values stay private |
| Channel handoffs | existing `submission` (`kind='handoff'`) | `0001_init.sql:93` | no new table |

## 2. `0003_ext_registry.sql`

```sql
-- 0003_ext_registry: snapshot of registered plugins (code is the source of truth; this is the audit copy)
CREATE TABLE ext_plugin (
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL,                       -- port name, e.g. 'eligibility'
  plugin_id TEXT NOT NULL,                  -- lower_snake
  version TEXT NOT NULL,                    -- semver of the logic
  spec TEXT NOT NULL DEFAULT '',            -- standard/rule implemented, with its version/date
  caps_json TEXT NOT NULL DEFAULT '{}',     -- capability flags (booleans only)
  config_sha TEXT NOT NULL DEFAULT '',      -- sha256 of the resolved, non-secret config at start-up
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL,
  UNIQUE (kind, plugin_id, version)
);
CREATE INDEX ext_plugin_kind ON ext_plugin(kind, plugin_id);

-- policy snapshots: the resolved enabled-set hash per start-up, so a stored verdict can be compared with today's
CREATE TABLE ext_policy (
  sha TEXT PRIMARY KEY,                     -- sha256(resolved enabled-set + strategy + ruleset_sha)
  enabled_json TEXT NOT NULL,               -- sorted ["kind/id", ...]
  strategy_json TEXT NOT NULL,
  ruleset_sha TEXT NOT NULL,
  created_at TEXT NOT NULL
);

-- operator-visible provenance of settings changes is the existing audit_log (reason 'setting_changed');
-- no table is added for it.
```

## 3. `0004_ext_verdicts.sql`

```sql
-- 0004_ext_verdicts: explainable, versioned plugin verdicts (no private values; reasons are codes + ids)
CREATE TABLE ext_verdict (
  id INTEGER PRIMARY KEY,
  subject_kind TEXT NOT NULL CHECK (subject_kind IN ('posting','application')),
  subject_id INTEGER NOT NULL,              -- no FK: survives deletion like audit_log
  stage TEXT NOT NULL CHECK (stage IN ('ingest','score','draft','approve','handoff')),
  plugin_kind TEXT NOT NULL,
  plugin_id TEXT NOT NULL,
  plugin_version TEXT NOT NULL,
  verdict TEXT NOT NULL CHECK (verdict IN ('yes','no','unknown')),
  reasons_json TEXT NOT NULL DEFAULT '[]',  -- [{"code":"not_remote","refs":{"mode":"onsite"}}] ids/enums only
  policy_sha TEXT NOT NULL,
  ruleset_sha TEXT NOT NULL,
  is_current INTEGER NOT NULL CHECK (is_current IN (0,1)),
  decided_at TEXT NOT NULL
);
CREATE UNIQUE INDEX ext_verdict_current
  ON ext_verdict(subject_kind, subject_id, stage, plugin_kind, plugin_id) WHERE is_current = 1;
CREATE INDEX ext_verdict_subject ON ext_verdict(subject_kind, subject_id, stage);

-- application-level pin used by T186 and the policy re-check (providers/05 policy_sha lives beside it)
ALTER TABLE application ADD COLUMN approved_policy_sha TEXT;
ALTER TABLE application ADD COLUMN approved_posting_hash TEXT;     -- T186
```

Notes:

* `is_current` mirrors `score.is_current` (`0001_init.sql:58-66`): a new verdict for the same key flips the old row to 0 in one transaction; history stays.
* The `approve` and `handoff` stages hold the T182 re-evaluation, so the approval evidence is "these plugin versions said this at that time".
* `reasons_json` is validated in Go by the same scalar-only check as audit evidence (`evidenceJSON`, `internal/audit/audit.go:109`) before insert; a CHECK on JSON content is not possible in SQLite, so the schema test asserts no column default carries a value and a unit test inserts a forged reason (private-looking string) and expects refusal.
* T181 (company-level duplicate) uses providers' `employer` table once `0002` lands; until then it needs only a `company_norm` index on `posting`, which is added by that task, not here.
* `ALTER TABLE ... ADD COLUMN` is permitted by SQLite for nullable columns without defaults; the migration test (below) opens a DB at `0002`, applies `0003`/`0004`, and compares `PRAGMA table_info`.

## 4. Settings keys

All in the existing `setting(key, value)` table; values are strings; the API validates against the registry.

| Key | Values | Default | Notes |
|---|---|---|---|
| `ext.<kind>.<id>.enabled` | `0` or `1` | absent -> policy default | operator layer (22) |
| `ext.strategy.<port>` | strategy id | absent -> policy default | validated against registered strategies |
| `ext.flag.<name>` | `0` or `1` | `0` | experimental plugin registration |
| `ext.channel.allow_mailto` | `0` or `1` | `0` | only settable to `1` when the deliverability check for the configured domain is `Yes` |

## 5. Per-ATS profile data

Files, not tables (versioned, reviewed): `rules/ats/<profile>.yaml`.

```yaml
schema: ats_profile.v1
id: greenhouse
applies_hosts: ["boards.greenhouse.io", "job-boards.greenhouse.io"]   # exact hosts; also must exist in deploy/egress-allowlist.txt if fetched
spec: "Greenhouse Job Board API (checked 2026-11)"
upload:  { types: [pdf, docx, txt], max_bytes: 20971520, max_files: 2 }
checks:  [link_annotations, headings, page_budget, filename_policy]
quirks:
  - { id: gh_cover_optional, note: "cover letter is a separate optional upload" }
```

The loader validates `applies_hosts` against the egress host grammar (`reHost`, `internal/egress/egress.go:65`) and refuses a profile naming a check that no registered plugin provides.

## 6. Retention, privacy, export

* `ext_verdict` rows follow the owning posting/application: the retention pruner (`internal/ingest/retention.go`) deletes rows for pruned postings and writes a `tombstone` count (`0001_init.sql:130`).
* Export/delete (providers' T218 pattern): `ext_verdict` is included by subject id; `ext_plugin` and `ext_policy` hold no personal data and are exported as-is.
* No new sealed column is needed because nothing private is stored; the gate `check_no_personal_data.sh` is run over the migration test DB.

## 7. Migration tests

1. Fresh DB: apply all migrations; schema test asserts the new tables/indices and that no default or CHECK mentions a profile value (extends `internal/store/schema_test.go`).
2. Upgrade DB: start from a fixture at `0002`, apply `0003`/`0004` with the backup step; row counts of old tables unchanged.
3. Forged reason test (above).
4. `is_current` uniqueness: two concurrent writers of the same key; exactly one current row (use the existing `gates/txcheck` pattern).
