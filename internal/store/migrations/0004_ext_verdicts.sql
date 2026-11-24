-- 0004_ext_verdicts (JSK-T234, docs/research/protocols/23 section 3): explainable, versioned plugin verdicts.
-- Reasons are closed-vocabulary codes plus ids/enums (validated in Go before insert). No private value anywhere.
CREATE TABLE ext_verdict (
  id INTEGER PRIMARY KEY,
  subject_kind TEXT NOT NULL CHECK (subject_kind IN ('posting','application')),
  subject_id INTEGER NOT NULL,
  stage TEXT NOT NULL CHECK (stage IN ('ingest','score','draft','approve','handoff')),
  plugin_kind TEXT NOT NULL,
  plugin_id TEXT NOT NULL,
  plugin_version TEXT NOT NULL,
  verdict TEXT NOT NULL CHECK (verdict IN ('yes','no','unknown')),
  reasons_json TEXT NOT NULL DEFAULT '[]',
  policy_sha TEXT NOT NULL,
  ruleset_sha TEXT NOT NULL,
  is_current INTEGER NOT NULL CHECK (is_current IN (0,1)),
  decided_at TEXT NOT NULL
);
CREATE UNIQUE INDEX ext_verdict_current
  ON ext_verdict(subject_kind, subject_id, stage, plugin_kind, plugin_id) WHERE is_current = 1;
CREATE INDEX ext_verdict_subject ON ext_verdict(subject_kind, subject_id, stage);

ALTER TABLE application ADD COLUMN approved_policy_sha TEXT;
ALTER TABLE application ADD COLUMN approved_posting_hash TEXT;
