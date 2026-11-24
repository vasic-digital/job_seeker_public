# 22 - Strategy and policy model

| | |
|---|---|
| Part | C of the protocols research |
| Date | 2026-11-24 |
| Code | `Layer`, `Resolve`, `Strategy`, `FavouritesFirst` are compiled in [appendix-compiled-sketch/ext/](appendix-compiled-sketch/ext/) (`policy.go`, `strategy.go`) |
| Related | [../providers/05-apply-policy-and-provider-selection.md](../providers/05-apply-policy-and-provider-selection.md) (apply modes, G9, `policy_sha`) |

## Contents

1. [Three kinds of configuration](#1-three-kinds-of-configuration)
2. [Policy as data: `rules/policy.yaml`](#2-policy-as-data)
3. [Precedence](#3-precedence)
4. [Strategy selection](#4-strategy-selection)
5. [Composition with Position/Listing apply policy](#5-composition-with-positionlisting-apply-policy)
6. [Explainability and reason codes](#6-explainability-and-reason-codes)
7. [How settings and the overlay drive it](#7-how-settings-and-the-overlay-drive-it)
8. [Validation](#8-validation)

## 1. Three kinds of configuration

| Kind | Examples | Lives in | Changed by | Audited |
|---|---|---|---|---|
| Rules and weights (public, versioned with code) | score weights, area tokens, keyword synonyms, per-ATS upload limits, format profiles | `rules/*.yaml` (existing: `area_rules.yaml`, `score.yaml`, loaded by `match.LoadRules`, `internal/match/rules.go:101`) | a reviewed PR | git history, `ruleset_sha` |
| Operator settings | which plugins are on, favourites, caps, apply mode | `setting` table (`0001_init.sql:125`) | the operator in the console/CLI | `setting_changed` audit row |
| Private overlay | salary floor, tz window, blocked companies, eligible countries, work-auth answers | overlay file (`internal/match/overlay.go:43`), never persisted, never logged | the operator on disk | presence only |

A plugin reads (1) through its own typed config struct, (2) through `Settings`, and (3) only through the `Profile` accessor interface (21 section 3.1). The three sources never mix inside a plugin.

## 2. Policy as data

New file `rules/policy.yaml`, loaded next to the other rules, hashed into `ruleset_sha` (so a policy change invalidates stale verdicts exactly as a rules change does today).

```yaml
schema: policy.v1            # unknown schema is rejected
defaults:                    # layer "policy_yaml"
  enabled:                   # "<kind>/<id>": bool ; absent = port default (gates: off)
    eligibility/remote_mode: true
    eligibility/blocked_company: true
    eligibility/salary_floor: true
    eligibility/timezone: true
    eligibility/work_auth: true
    match/keyword_tokens: true
    compliance/robots: true
    compliance/provider_tos: true
    channel/provider_url: true
    channel/mailto: false         # needs deliverability proof first
  strategy:                       # port -> strategy id
    channel: favourites_first
    ats_profile: host_match
    format: country_then_default
  params:
    eligibility/remote_mode: { allow_hybrid: "false" }
    match/keyword_tokens:    { synonyms_file: "rules/synonyms.yaml" }
locked:                           # gates only the operator layer may switch off
  - eligibility/remote_mode
  - eligibility/blocked_company
  - eligibility/salary_floor
  - compliance/robots
  - compliance/provider_tos
scopes:                           # overrides keyed by scope
  provider:
    himalayas: { enabled: { compliance/robots: true } }
  ats:
    greenhouse: { params: { ats/greenhouse: { max_upload_bytes: "10485760" } } }
  country:
    DE: { strategy: { format: europass_de } }
```

Schema rules (enforced by the loader, tested): every key is a known `<kind>/<id>` of a registered plugin or the load fails naming only the key path; booleans are real booleans; params are strings parsed by the plugin's `ConfigSchema` (type, bounds); `locked` entries must exist; `scopes` keys must be a registered provider / ATS profile / ISO country. A missing file means "built-in defaults only", and the built-in defaults enable **no gate**, which makes every gate `Unknown` and therefore blocks: a deployment without a policy cannot approve anything. (`policy.yaml` ships enabled in the repository.)

## 3. Precedence

Lowest to highest; later wins per key; implemented by `ext.Resolve` (`appendix-compiled-sketch/ext/policy.go`):

| # | Layer | Source | Can switch a **locked** gate off |
|---|---|---|---|
| 1 | `builtin` | code defaults | no |
| 2 | `policy_yaml` | `rules/policy.yaml` `defaults` | no |
| 3 | `country` | `scopes.country.<CC>` of the posting/candidate market | no |
| 4 | `ats` | `scopes.ats.<profile>` of the apply host | no |
| 5 | `provider` | `scopes.provider.<name>` | no |
| 6 | `operator` | `setting` rows | **yes** (audited) |
| 7 | `application` | per-application override with mandatory reason (providers/05 `apply_override`) | no |

Rules: (a) a locked gate can be turned off only by layer 6 and never by 3-5 or 7, because those are data a provider or a posting can influence; (b) the resolver output is `Resolved` with `Sourced[V]{Value, From}` so every effective value names its layer (tested: `TestResolvePrecedenceAndLockedGate`); (c) the resolved enabled-set hash is stored with each stored verdict (`policy_sha`, 23) so a verdict computed under a different policy is detectably stale.

## 4. Strategy selection

A `Strategy[C]` resolves "more than one plugin applies". It is separate from rules so selection can be tested without any rule running.

| Decision point | Candidates | Built-in strategies | Notes |
|---|---|---|---|
| Channel for an application | `ApplicationChannel`s whose `Supports(posting)` | `favourites_first` (compiled in the sketch), `most_capable`, `manual_only` | `mailto` apply URLs force `manual` until a mail path exists (T184) |
| ATS profile for a draft | `ATSProfile`s where `Applies(host)` | `host_match` (most specific host wins; ties by id), `generic_fallback` | no match -> `generic` profile, never "no checks" |
| Format/CV convention | `FormatProfile`s by country and language | `country_then_default`, `operator_pinned` | e.g. `DE` -> Europass-style heading set |
| Source ordering within a run | `SourceAdapter`s | `priority`, `quality_ranked` (provider profile R, providers/05 section 3) | ordering never changes which are enabled |
| Listing of a Position | Listings of one Position | `all_aware`, `single_auto`, `single_favourites` (providers/05 section 2) | see section 5 |

Contract for every strategy: pure, deterministic, returns `idx=-1` plus a reason when nothing is acceptable (strict mode refuses rather than guesses; `TestFavouritesFirst`).

## 5. Composition with Position/Listing apply policy

The provider research defines the apply modes, the provider ranking and gate G9a-d; this part supplies the slots they plug into, without re-defining them:

| providers/05 element | Slot in this architecture | Interface |
|---|---|---|
| `apply_mode` (`all_aware`, `single_auto`, `single_favourites`) | the three built-in strategies of decision point "Listing of a Position" | `Strategy[position.Listing]` |
| `provider_favourites`, `favourites_strict` | `StrategyInput.Favourites`, `.Strict` | data in `setting` |
| Gate G9a (mode eligibility), G9c (employer policy), G9d (duplicate acknowledgement) | `ComplianceGate` plugins `apply_mode`, `employer_policy`, `duplicate_ack`, `Stage=approve` | verdict + reason codes below |
| Gate G9b (`position_apply_lock`) | not a plugin: a transactional insert inside `Approve` (a PK conflict is the proof); this part only records that the plugin chain ran first | core |
| `policy_sha` | a stored hash that now **also covers** the resolved enabled-set and `ruleset_sha`, so one hash answers "did any decision input change since approval" at `handoff-complete` | 23 |
| Provider quality ranking R | an input to `quality_ranked` strategies | data |

Order at approval (fixed): core FR-026 checks (`checkLive`, `internal/draft/service.go:510`) -> eligibility re-run (T182) -> `ComplianceGate`s (including G9a/c/d) -> G9b lock insert -> write approval. Nothing earlier in the list can be skipped by a later one.

## 6. Explainability and reason codes

Every `Decision` carries `Reason{Code, Refs}`. Codes are rows in `internal/audit/reasons.tsv` (column 3 names the call-site family that must emit it, as for the existing codes). Proposed new family (all `Refs` are ids/hashes only):

| Code | Emitted by | Refs |
|---|---|---|
| `plugin_panic` | kernel | `plugin`, `kind` |
| `plugin_timeout` | kernel | `plugin` |
| `rule_no_data` | any rule answering Unknown for missing profile data | `rule`, `field` (field name only) |
| `not_remote`, `remote_mode_unknown` | `eligibility/remote_mode` | `mode` |
| `work_auth_unknown`, `work_auth_region_excluded` | `eligibility/work_auth` | `region` |
| `tz_overlap_insufficient` | `eligibility/timezone` | none (values stay private) |
| `lang_not_supported` | `eligibility/language` | `lang` |
| `gate_regressed_at_approval` | `draft.Approve` | `gate`, `plugin_version` |
| `strategy_favourite`, `strategy_first_ranked`, `strategy_none_acceptable` | strategies | `id` |
| `channel_manual_only` | channel strategy | `reason` |
| `ats_finding_block` | ATS checks | `profile`, `check` |
| `compliance_blocked` | `ComplianceGate` | `gate`, `stage` |
| `policy_stale` | verdict reader | `stored_sha`, `current_sha` |

Audit rows keep the existing shape (`audit.Append`, `internal/audit/audit.go:147`): `reason_code` from the closed vocabulary, `evidence_json` scalar ids only (`evidenceJSON` rejects anything else, `:109`).

## 7. How settings and the overlay drive it

* Operator switches: `ext.<kind>.<id>.enabled` and `ext.strategy.<port>` in `setting` (23), written through the existing settings API with an audit row.
* The overlay never reaches a plugin as a struct. `Profile` accessors return `(value, ok)`; the production `Profile` adapts `*match.Overlay` (`HasSalaryFloor`, `HasTZ`, `EligibleCountries`, `BlockedCompanies`, `AllowedOnsiteCities`, `internal/match/overlay.go:16-31`). An absent overlay yields `ok=false` everywhere, so rules answer `Unknown` and the live path stays blocked (`ErrOverlayAbsent`, `service.go:517`).
* `pdgate` (`internal/pdgate/pdgate.go`) keeps scanning the tree for overlay needles; plugin fixtures are synthetic (persona fixtures only) so they pass it.

## 8. Validation

1. `jsctl policy check` loads `rules/policy.yaml`, resolves every scope combination for the registered plugins and fails on unknown keys, locked-gate violations, or a gate port left with zero active plugins in the default scope.
2. A matrix test enumerates `provider x ats x country` scopes and asserts no locked gate resolves off (mutation: allow a provider layer to switch one off -> test fails).
3. `gates/verify_policy.sh` (with `--prove-failure`) runs 1 and 2 (25).
