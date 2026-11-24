-- 0003_ext_registry (JSK-T234, docs/research/protocols/23 section 2): audit copy of the registered plugins and of
-- the resolved policy per start-up. Code stays the source of truth. No private value in any column or default.
-- Numbering: 0002 belongs to the position tables (provider track); the runner applies by applied-set, so the gap is harmless.
CREATE TABLE ext_plugin (
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL,
  plugin_id TEXT NOT NULL,
  version TEXT NOT NULL,
  spec TEXT NOT NULL DEFAULT '',
  caps_json TEXT NOT NULL DEFAULT '{}',
  config_sha TEXT NOT NULL DEFAULT '',
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL,
  UNIQUE (kind, plugin_id, version)
);
CREATE INDEX ext_plugin_kind ON ext_plugin(kind, plugin_id);

CREATE TABLE ext_policy (
  sha TEXT PRIMARY KEY,
  enabled_json TEXT NOT NULL,
  strategy_json TEXT NOT NULL,
  ruleset_sha TEXT NOT NULL,
  created_at TEXT NOT NULL
);
