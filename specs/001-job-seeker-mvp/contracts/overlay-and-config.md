# Contract: overlay, configuration and secrets

Covers FR-053..FR-056, FR-050.

## Overlay (private, memory only)

Source: file `career_private/overlay.yaml` resolved from env `CAREER_PRIVATE_DIR`; schema id `career.overlay.v1` (extended keys below). The Go reader enforces an allow-list and rejects unknown keys with a path-only error (never the value).

| Key | Type | Used by | Notes |
|---|---|---|---|
| existing identity keys (as in umbrella `_tools/career/overlay.py`) | string | none in v1 (templates only) | read for presence only |
| `salary_min`, `salary_currency` | int, ISO 4217 | scoring salary gate and component | stored nowhere; DB gets `overlay_present` |
| `tz_window` | `{start_utc, end_utc}` | scoring location component | |
| `allowed_onsite_cities` | list of strings | scoring location gate | |
| `blocked_companies` | list of strings | hard gate `blocked_company` | normalised compare, no regex from the file |
| `salary_period` | `year|month|hour` | normalises posting salary before comparing with `salary_min` | |
| `eligible_countries` | list of ISO 3166 codes | hard gate `excluded_region` against a posting's country restrictions | |
| `work_auth_countries` | list of ISO 3166-1 alpha-2, upper case; empty list = explicit "none" | `eligibility.WorkAuth` via `OverlayProfile.WorkAuthCountries` (JSK-T177/T242) | scoring-only; absent key = not configured (rule says Unknown); never in the DB |
| `languages` | non-empty list of ISO 639-1 codes, lower case | `eligibility.Language` via `OverlayProfile.Languages` (JSK-T177/T242) | scoring-only; absent key = not configured; an empty list is rejected |

The umbrella `overlay.py` allow-list receives `salary_min`, `salary_currency`, `salary_period`, `tz_window`, `allowed_onsite_cities`, `blocked_companies`, `eligible_countries` in a separate reviewed umbrella change (JSK-T011); the two readers are kept in step by a cross-check test that loads both against a throwaway overlay file containing only synthetic values.

## Environment (names only; values never printed)

| Name | Purpose |
|---|---|
| `JOBSEEKER_ENV` | `production` enables boot guards |
| `JOBSEEKER_HTTP_ADDR`, `JOBSEEKER_HTTPS_ADDR` | listeners (ports discovered at run time by tooling, never hardcoded in docs) |
| `JOBSEEKER_DATA_DIR`, `JOBSEEKER_CERT_DIR` | data and TLS material locations |
| `JOBSEEKER_DB_KEY` | KEK secret (or 0600 file via `JOBSEEKER_DB_KEY_FILE`) |
| `JOBSEEKER_SESSION_KEY` | session MAC key |
| `JOBSEEKER_BOOTSTRAP_TOKEN` | first-passkey registration |
| `JOBSEEKER_KILL_SWITCH`, `JOBSEEKER_METRICS_ADDR`, `JOBSEEKER_BACKUP_DIR`, `JOBSEEKER_TEMPLATE_DIR`, `JOBSEEKER_CANDIDATE_NAME`, `JOBSEEKER_EXPORT_PASSPHRASE` | kill switch, metrics listener, backup target, template slots dir, candidate display name for download filenames (JSK-T175), export archive key (separate from the DEK) |
| `JOBSEEKER_ALLOWED_HOSTS`, `JOBSEEKER_ORIGINS` | Host and Origin allow-lists |
| `JOBSEEKER_EGRESS_ALLOWLIST` | path to allow-list file |
| `CAREER_PRIVATE_DIR`, `CAREER_CONTENT_DIR`, `DESIGN_SYSTEM_DIR` | private overlay, public career content, design system |
| `TYPESAFE_API_KEY`, `JEV_GATE` | hosted decision layer (key by name only; `JEV_GATE` must equal `shadow`) |
| `JS_LLM_TIEBREAK` | `1` enables the tiebreak (default off) |
| `JOBSEEKER_JOBOPS_URL`, `JOBSEEKER_JOBOPS_CRED` | sidecar (if spike GO) |
| `JOBSEEKER_CRED_<SITE>` | per-site credentials if ever stored (default: none) |

## Boot invariants (fail closed, names-only capability matrix)

dry-run default on; live mode off; `JEV_GATE` == `shadow`; TLS certificate valid; disk and port headroom; overlay readable WHEN configured (an unconfigured overlay is allowed: scoring neutral, live approval refused); allow-list loaded and non-empty; secret files mode 0600 (refuse if group/world readable); sensitive columns verified ciphertext by sentinel row; CSP configured; production refuses example values (`bootguard`); migrations applied after a successful backup. Output example line: `invariant dry_run_default ... OK`. A violation exits non-zero and prints the invariant name only.
