# Proposal: overlay keys for work authorisation and languages (JSK-T242)

Status: PROPOSAL, awaiting an operator decision. Nothing in this document is applied; no code, schema or private
file changed. Revision: 2026-11-24.

## Why

`eligibility.WorkAuth` and `eligibility.Language` (merged, registered, disabled) read `ext.Profile.WorkAuthCountries()`
and `ext.Profile.Languages()`. `OverlayProfile` answers `ok=false` for both because the overlay schema
`career.overlay.v1` has no key for them, so against real data both rules say Unknown. This change adds the two keys.
It touches the private overlay schema, which is an operator-approved change (contract `overlay-and-config.md`).

## Proposed keys (career.overlay.v1, additive, both optional)

| key | value | meaning | validation (key path in errors only, never the value) |
|---|---|---|---|
| `work_auth_countries` | list of ISO 3166-1 alpha-2, upper case | countries where the operator may legally work | list of strings each matching `^[A-Z]{2}$`; empty list allowed (explicit "none") |
| `languages` | list of ISO 639-1 primary subtags, lower case | languages the operator can work in | list of strings each matching `^[a-z]{2}$`; empty list is rejected (use absence for "not configured") |

Both are scoring-only keys: validated and readable, never merged into the PDF or vault identity, never stored in the
database (the DB keeps only `overlay_present`). Unknown-key behaviour is unchanged (rejected with a path-only error).

## Exact change 1: job_seeker (Go reader)

`internal/match/overlay.go`

* `Overlay` gains `WorkAuthCountries []string` and `Languages []string`.
* `LoadOverlay` gains two `case` arms next to `eligible_countries`:
  `work_auth_countries` reuses the `ccRe` check; `languages` uses a new `langRe = ^[a-z]{2}$` and rejects an empty list.
* `identityKeys` unchanged.

`internal/ext/eligibility/eligibility.go`

* `OverlayProfile.WorkAuthCountries()` returns `(p.O.WorkAuthCountries, true)` when the overlay is present and the key
  was set (a new `waSet` flag mirrors `tz`), else `(nil, false)`.
* `OverlayProfile.Languages()` likewise.

Tests (synthetic values only): reader accepts both keys, rejects `"usa"`, `"EN"`, an empty `languages`, a non-list;
`OverlayProfile` answers `ok=false` when the key is absent and the list when present; the existing WorkAuth/Language
rule tests are re-run against a real `LoadOverlay` result instead of the synthetic Profile.

## Exact change 2: umbrella `_tools/career/overlay.py`

```python
ALLOWED: dict[str, str] = {
    ...
    "eligible_countries": "iso3166list",
    "work_auth_countries": "iso3166list",   # new
    "languages": "iso639list",              # new
}
SCORING_KEYS = (... , "eligible_countries", "work_auth_countries", "languages")
_LANG = re.compile(r"[a-z]{2}")
# in _kind_ok:
    if kind == "iso639list":
        return isinstance(v, list) and len(v) > 0 and all(isinstance(x, str) and _LANG.fullmatch(x) for x in v)
# in _KIND_MSG:
    "iso639list": "must be a non-empty list of lower-case ISO 639-1 codes",
```

`work_auth_countries` reuses the existing `iso3166list` kind (an empty list stays valid, matching `eligible_countries`).

## Cross-check

The contract already requires the two readers to be kept in step by a test that loads both against one throwaway
overlay containing only synthetic values. That test gains the two keys in both the accepting and rejecting direction.
The umbrella change and the job_seeker change land together (umbrella first, as a gitlink-free Python edit; job_seeker
second), because a Go reader that accepts a key the Python reader rejects would make `career` builds fail on a valid file.

## What is NOT part of this proposal

* Enabling `work_auth` / `language` / `timezone` in `rules/policy.yaml`. That stays a separate operator decision once the
  keys exist and the overlay carries real values.
* Rewiring T167/T177/T183 to the shared Profile accessors.
* Any private value. The overlay file itself is edited only by the operator.

## Decision requested

Approve (as written, or with edits to the key names or validation) so the two edits above can be made and T242 ticked
with a real-overlay run. Until then T242 stays PARTIAL.
