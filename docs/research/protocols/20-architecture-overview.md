# 20 - Architecture overview: ports, adapters and plug-in points

| | |
|---|---|
| Part | C of the protocols research (pluggable architecture) |
| Date | 2026-11-24 |
| Inputs | Part A `01`-`08` (standards), Part B `10`-`13` (inventory, gaps, failure modes, backlog T160-T187), code at `github/main` `aae1000` |
| Status | PROPOSAL. No production code changed. Interfaces compile: see [appendix-compiled-sketch/](appendix-compiled-sketch/) and [appendix-sketch-output.txt](appendix-sketch-output.txt). |
| Sibling work | Provider landscape, canonical Position model and apply policy are in [../providers/](../providers/README.md) (merged in PR #43). This part integrates **by interface name** (21 section 10, 22 section 5) and does not duplicate it. Task ids from JSK-T225, FR from FR-095, SC from SC-030 (26). |

## Contents

1. [Why](#1-why)
2. [What exists today](#2-what-exists-today)
3. [Layered view](#3-layered-view)
4. [Extension-point map](#4-extension-point-map)
5. [Pipeline and where each standard plugs in](#5-pipeline-and-where-each-standard-plugs-in)
6. [Design rules](#6-design-rules)
7. [What is deliberately NOT pluggable](#7-what-is-deliberately-not-pluggable)
8. [Package layout](#8-package-layout)
9. [Reading order](#9-reading-order)

## 1. Why

Part A lists 40+ standards, protocols and rules. Part B shows the code already has three provider adapters, one rules-file classifier/scorer, a slot-based template store and a human-approval state machine, but each new rule today means editing a switch inside `ScorePosting` or `Approve`. The directive is: adding a standard must be **a new package, one registry line, fixtures** and nothing else. This part defines the seams that make that true, with exact Go signatures, a settings/policy model and migrations.

## 2. What exists today

Seams that are already good and must be kept:

| Seam | Where | Why it is already a port |
|---|---|---|
| `providers.Provider` | `internal/providers/providers.go:111` (4 methods) | Adding a provider is an adapter plus an allow-list line plus a provider row (`specs/001-job-seeker-mvp/contracts/provider-adapter.md` rule 7). Wiring is a literal slice in `cmd/jsobserve/main.go:81-83`. |
| Egress allow-list as data | `internal/egress/egress.go:57`, `deploy/egress-allowlist.txt` | One client, hosts are data. |
| Rules as YAML data | `rules/area_rules.yaml`, `rules/score.yaml`, loaded by `match.LoadRules` (`internal/match/rules.go:101`) | Weights, bands and tokens change without code. |
| Closed reason vocabulary | `internal/audit/reasons.tsv`, enforced by `audit.Append` (`internal/audit/audit.go:147`) | Every decision has a code; unknown code is refused (`ErrUnknownReason`). |
| State machine | `internal/fsm/fsm.go:48-51` | One place defines legal transitions. |
| Slot-addressed templates | `internal/templates/templates.go:57,118` | Drafts are assembled by (kind, area, lang) slot, no generation (FR-029). |
| Gates as scripts with `--prove-failure` | `gates/verify_*.sh`, runner `gates/run_all.sh:14-17` | Local CI-less enforcement with paired mutation proof. |

Seams that are **missing** (each is a gap in Part B or a failure mode in 12):

| Missing seam | Today | Evidence |
|---|---|---|
| Eligibility as a rule set | hard gates are hard-coded `if` blocks appending ids to `Score.Gates`: `excluded_region` (`internal/match/score.go:147`), `onsite_outside_allowed` (`:159`), `blocked_company` (`:181`), `salary_below_floor` (`:199`) | cannot add work-authorization, language or timezone rules without editing `ScorePosting` (`:97`); gates are not re-evaluated at approval (F-12..F-14, T182) |
| Posting standards reader | each adapter hand-maps its own JSON into `Posting` | no JSON-LD / HR Open reader; no ATS board adapters (Part A 01, 02) |
| Score features | components computed inline (`score.go:112-175`) | T164/T166 need a pluggable matcher |
| Channels | `HandoffComplete` (`internal/draft/service.go:665`) records a human-confirmed handoff; no abstraction over *how* the human is helped | `mailto:` URLs not flagged (F-16, T184) |
| Per-ATS / per-provider upload profiles | none | G-16, T176 |
| Compliance gates | kill switch, caps, step-up are in `checkLive` (`service.go:510`) as sequential `if`s | GDPR/ToS/robots checks need a home (Part A 05) |
| Telemetry/evidence emitters | each call site writes its own evidence | Part A 08 |

## 3. Layered view

```
                        +-----------------------------------------------------------+
   L6  Interfaces       |  console (internal/console)   REST (internal/api)   CLI   |
                        +----------------------------+------------------------------+
                                                     |  commands / queries
                        +----------------------------v------------------------------+
   L5  Use cases        |  ingest.Runner   match pipeline   draft.Service   tracker |
       (orchestration)  |  (call ports through registries; own transactions + audit)|
                        +--+-----------+------------+-----------+-----------+-------+
                           |           |            |           |           |
                  ports ->  SourceAdapter  Eligibility   Renderer/   Channel/   Tracker
                           StandardReader  MatchRule     ATSProfile  Knockout   hooks
                           Normalizer      ScoreFeature  FormatProfile Compliance Telemetry
                        +--+-----------+------------+-----------+-----------+-------+
   L4  Extension kernel |  internal/ext : Verdict, Reason, Registry[T], Settings,    |
                        |  Policy (YAML), Conformance reporter, fail-closed combine  |
                        +--+-----------+------------+-----------+-----------+-------+
   L3  Adapters         |  internal/ext/<kind>/<plugin>  (one package per standard)  |
                        +----------------------------+------------------------------+
   L2  Core domain      |  Posting, Application FSM (internal/fsm), Approved, Draft  |
                        |  hash, audit chain (internal/audit)                        |
                        +----------------------------+------------------------------+
   L1  Infrastructure   |  store (SQLite, migrations), crypto, egress client,        |
                        |  limiter, scheduler, i18n, config                          |
                        +-----------------------------------------------------------+
```

Dependency rule: arrows point downward only. Adapters (L3) import the kernel (L4) and the domain types (L2); the use cases (L5) import the kernel and obtain adapters **only through registries**, never by package import. `cmd/*/main.go` is the one composition root that imports adapters and registers them (one line each).

## 4. Extension-point map

| # | Port (package `internal/ext/...`) | Stage | Sync/async | Fail mode | Part A source | Part B tasks |
|---|---|---|---|---|---|---|
| 1 | `SourceAdapter` (`source`) | ingest fetch | I/O, egress client | rate/circuit via limiter; error class from `providers.Class*` | 01, 02 | T176 |
| 2 | `StandardReader` (`reader`) | ingest normalise | pure | `ok=false` = not mine; error = skip + count | 01 | T164 |
| 3 | `Enricher` / tagger (`reader`) | after normalise | pure | advisory: failure never blocks | 01 (ESCO), 07 | T164, T185 |
| 4 | `EligibilityRule` (`eligibility`) | after normalise, again at approve | pure | three-valued, Unknown blocks | 05, 06 | T182, T183, T184, T186 |
| 5 | `MatchRule` / `ScoreFeature` (`match`) | scoring | pure | feature error = component 0 + reason, never panics the run | 07 | T164-T166 |
| 6 | `KnockoutHandler` (`knockout`) | draft / approval | pure + human | unanswered = approval blocked | 02, 05 | T177 |
| 7 | `FormatProfile` / `DocumentRenderer` (`render`) | draft | CPU, umbrella engine | gate failure refuses the artifact | 03 | T162, T168-T171, T173-T175, T178-T180 |
| 8 | `ATSProfile` / `ParserSimulator` (`ats`) | pre-handoff + gates | CPU | findings are advisory except `Block` severity | 02, 03 | T176 |
| 9 | `ApplicationChannel` (`channel`) | after human approval | I/O (never in v1) | no `Approved` value = no call; `direct_send` rejected at registration | 02, 04 | T184 |
| 10 | `ComplianceGate` (`compliance`) | ingest, draft, approve | pure + settings | fail closed; refusal audited | 05 | T181, T185 |
| 11 | `DeliverabilityCheck` (`deliver`) | before a mail channel is enabled | DNS I/O via egress | Unknown blocks enabling the channel | 04 | - |
| 12 | `TrackerHook` (`track`) | status events | pure | hook error is logged, never blocks the status write | 07 | T187 |
| 13 | `EvidenceSink` / `Telemetry` (`evidence`) | everywhere | async, bounded | drop-and-count; never blocks | 08 | - |
| 14 | `position.Resolver` (owned by the provider research, **not** user-pluggable) | after ingest store | DB transaction | queued merge candidate, never auto-merge when uncertain | - | providers T188..T219 |
| 15 | `Strategy[C]` (selection among applicable plugins) | channel, ATS profile, format, Listing of a Position | pure | `-1` + reason when none acceptable | - | 22 |

Ports 1-13 and the strategy get full signatures and reference implementations; the rest follow the same template (21).

## 5. Pipeline and where each standard plugs in

```
 fetch --------> normalise -------> enrich ---------> classify/map -> eligibility -> score -> triage
 SourceAdapter   StandardReader      Enricher          (area rules)    EligibilityRule  ScoreFeature  (human)
 JSON/RSS/ATS    JSON-LD, HR Open    ESCO tags, scam   data YAML       RemoteMode,      keyword+syn,
 board APIs      per-provider map    flags (T185)                      work-auth, tz,   seniority ...
                                                                       lang, salary
                                                                            |
                      +-----------------------------------------------------+
                      v
 create application -> draft ---------------------> approve (human) ---------> handoff ------> track
                       Knockout pre-flight (T177)   Eligibility RE-RUN (T182)   ApplicationChannel  TrackerHook
                       Renderer/FormatProfile       ComplianceGate              Prepare() only      reply/ghost
                       ATSProfile.Check (T176)      posting hash pin (T186)     (no Send in v1)     metrics
                       Language rule (T167)         step-up, caps, kill switch
```

Plug-in points by concern (the table the operator asked for):

| Concern | Port(s) | Examples |
|---|---|---|
| Ingest time | SourceAdapter, StandardReader, ComplianceGate(`robots`, `tos`) | Greenhouse/Lever/Ashby boards, JSON-LD page, RSS |
| Normalisation | StandardReader, Enricher | schema.org `JobPosting`, HR Open, ESCO skills |
| Matching | MatchRule, ScoreFeature | token+synonym keyword feature, area overlap margin |
| Eligibility | EligibilityRule | RemoteMode, work-auth, timezone overlap, language, salary floor |
| Drafting | KnockoutHandler, language rule (`internal/draft/language.go`) | knockout checklist, placeholder gate |
| Rendering/packaging | FormatProfile, DocumentRenderer, ATSProfile | PDF links/tagging, PDF/A, DOCX, TXT, Europass, per-country CV |
| Submission/handoff | ApplicationChannel, DeliverabilityCheck | open-url, mailto draft, SMTP/OAuth hand-off, ATS prefill assist, manual |
| Tracking | TrackerHook | reply detection, ghosting timer, funnel metrics |
| Compliance | ComplianceGate | GDPR retention, EU AI Act transparency flags, ToS allow-list, robots, kill switch |

## 6. Design rules

1. **Ports are small and pure where possible.** Rules take values and return a `Decision`; I/O ports (source, channel, deliverability) receive an `*egress.Client` and a `context.Context`, never a raw `http.Client`.
2. **Three-valued, never boolean, for anything that can block.** `Yes | No | Unknown`; `Unknown` blocks exactly like `No` at every gate, and the reason says which data was missing. This is the Go rendering of the repo-wide "0 pass / 1 finding / 2 cannot determine" convention (`gates/run_all.sh:2-3`).
3. **Every verdict is explainable.** A `Decision` carries `[]Reason{Code, Refs}`; `Code` must exist in the closed vocabulary (`internal/audit/reasons.tsv` gains a `plugin` family), `Refs` are ids and hashes only (the evidence rule at `internal/audit/audit.go:99-139`). Overlay values never appear.
4. **Deterministic ordering.** Plugins run by `(Priority, ID)`; combination is order-independent for `Combine` (any No wins, else any Unknown, else Yes) so ordering affects only the reason trail, not the verdict.
5. **Fail closed.** Unregistered, unconfigured, panicking or timed-out gate plugins yield `Unknown`. A registry that has zero active plugins for a gate port yields `Unknown` (no rules is not consent). Channels declaring `direct_send` are rejected at registration (FR-027).
6. **Registration is code, activation is data.** One `Register` line per plugin in the composition root; on/off, config and priority overrides come from the `setting` table and `rules/*.yaml` (22, 23). No dynamic loading, no reflection scanning, no `init()` side-effects.
7. **Versioned verdicts.** Every stored verdict carries `(plugin_id, plugin_version, ruleset_sha)` so a stale decision is detectable (generalises `score.ruleset_sha`, `0001_init.sql:58`).
8. **Conformance is part of the plugin.** A plugin package ships fixtures, a `conformance_test.go` calling the kernel suite, and a mutation pair; the gate `gates/verify_plugins.sh` refuses a registered plugin without them (25).
9. **No network in tests.** Source adapters are tested against recorded bodies in `testdata/`; the live path is a separate `live_test.go` guarded as the existing providers do (`internal/providers/live_test.go`).

## 7. What is deliberately NOT pluggable

| Item | Reason |
|---|---|
| The human-approval requirement and the `Approved` token | FR-026/FR-027; no plugin can mint or bypass it (21 section 9). |
| The audit hash chain and reason vocabulary mechanism | tamper-evidence must have one implementation (`internal/audit/audit.go:84`). |
| The egress allow-list and IP denylist | FR-009: policy is data, enforcement is one client. Plugins add hosts only through `deploy/egress-allowlist.txt`. |
| Encryption and key handling | `internal/crypto`; plugins receive plaintext views they must not persist. |
| Generative drafting | FR-029 forbids it; renderers fill typed slots only. |
| Application state set | `internal/fsm`; plugins attach to transitions, they do not add states (a new state is a migration plus a spec change). |

## 8. Package layout

```
internal/ext/                 kernel: verdict.go registry.go settings.go policy.go conformance.go
internal/ext/source/          port + shared helpers (HTTP get wrapper over egress, pagination)
internal/ext/source/greenhouse|lever|ashby|rss|jsonldpage/   adapters
internal/ext/reader/          port; jsonld/ hropen/ esco/
internal/ext/eligibility/     port; remotemode/ workauth/ timezone/ language/ salary/ blocked/
internal/ext/match/           port; keyword/ area_overlap/
internal/ext/render/          port (profiles); pdflinks/ pdftag/ docx/ txt/ europass/
internal/ext/ats/             port; greenhouse/ lever/ workday/ parsersim/
internal/ext/channel/         port; openurl/ mailto/ smtp/ prefill/ manual/
internal/ext/compliance/      port; gdpr/ tos/ robots/ aiact/
internal/ext/deliver/         port; spf/ dkim/ dmarc/
internal/ext/track/           port; replydetect/ ghosting/
internal/ext/evidence/        port; sink/
```

`internal/providers` keeps working unchanged: section 24.2 shows the thin shim that registers the three existing providers as `SourceAdapter`s so the migration is incremental and never breaks `cmd/jsobserve`.

## 9. Reading order

[21](21-extension-interfaces.md) interfaces and lifecycle, [22](22-strategy-and-policy-model.md) selection and policy, [23](23-data-and-migrations.md) storage, [24](24-code-level-integration-guide.md) cookbook and the first six integrations, [25](25-conformance-and-test-strategy.md) tests and gates, [26](26-roadmap-and-task-map.md) tasks and proposed FRs/SCs.
