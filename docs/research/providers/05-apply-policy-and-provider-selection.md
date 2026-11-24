# 05 - Apply policy and provider selection

| | |
|---|---|
| Status | Design proposal (docs-only) |
| Builds on | `internal/draft` (gate: live mode, kill switch, caps `cap_daily`=5 / `cap_host`=2 defaults), `internal/fsm` (application states), `internal/tracker`, FR-026 (8 conditions), FR-036 (kill switch) |
| Core promise | Policy is a **restriction layered on top of** FR-026. It can only make an apply *less* possible. It never grants, skips or weakens any of the 8 conditions, never auto-submits, and a human approves every Listing separately. |

## 1. Terms

- Position P has Listings L1..Ln, each on a provider with a capability profile.
- An application is always against exactly one Listing (today's `application.posting_id UNIQUE`).
- "Eligible" = policy permits preparing and approving an application for that Listing now.

## 2. Modes (setting `apply_mode`, default `all_aware`)

| Mode | Meaning | Eligible Listings of P |
|---|---|---|
| `all_aware` (default) | We know every Listing and can apply through each supported one, each separately human-approved and tracked | all Listings whose provider has a supported handoff, subject to duplicate warnings and employer policy |
| `single_auto` | Enforce one provider per Position, chosen automatically | exactly one: the best-ranked capable Listing (section 3); if it expires or is withdrawn the next-best becomes eligible |
| `single_favourites` | Enforce one provider per Position by the operator's ordered favourites (`provider_favourites`, JSON list) | the first Listing in favourites order; providers not listed follow in auto order unless `favourites_strict=1` (then they are never eligible) |

Per-Position override (`position.apply_override`, with mandatory reason, audited): `inherit` (default), `all` (allow all Listings for this Position even in a single mode), `single` + `apply_override_listing` (force a specific Listing), `none` (do not apply to this Position at all; equals dismiss for apply purposes but keeps it visible). Changing an override on a Position that has a `live_approved` application is refused (409), because the approved decision was taken under the old policy.

## 3. Provider quality ranking

Per-provider capability profile (`provider_profile`, plus measured stats). Deterministic score `R(provider)` in [0,1], computed from data, recorded with the decision:

| Component | Weight | Source |
|---|---|---|
| Apply directness: `ats_native` 1.0, `direct_form` 0.8, `redirect` 0.5, `email` 0.4, `none/unknown` 0 | 0.30 | `provider_profile.apply_kind` (human-set from the cards in `02`) |
| Employer-origin: the Listing's `apply_url_canon` host is the employer's ATS or own domain (1) vs a board redirector page (0) | 0.20 | resolver (`ats_kind` present or employer-owned host) |
| Freshness: median lag between `posted_at` and `first_seen` across the provider's Listings, mapped 0..1 (0 days = 1, 14 days = 0) | 0.15 | `posting` |
| Reliability: success ratio of the last 30 days of `provider_call` and circuit state | 0.10 | `provider_call`, `provider.circuit_state` |
| Data completeness: share of `salary`, `remote_mode`, `countries`, `apply_url` present | 0.10 | `posting` |
| Our history: Wilson 95% lower bound of reply-or-better rate from `tracking`, only with n >= 10 applications else 0.5 neutral (stated as "insufficient history", never extrapolated) | 0.10 | `tracking` |
| Favourite bonus | not in R | favourites are a separate mode; in `single_auto` they are ignored |

Tie-break: provider name ascending. Weights are data (versioned `ranking_version`) and initial proposals, to be tuned by the operator; none claimed measured. Attribution/ToS **never** ranks a provider down silently, but a provider whose terms forbid acting on the data (for example Habr Career, NO-listed) simply has no supported handoff and is not eligible at all.

"Capable" = `apply_kind` not `none/unknown`, provider circuit not `open`, Listing not expired, and attribution link available.

## 4. Gates at each step

The new gate **G9 provider-policy** (name proposed) is evaluated, in this order, at: `prepare` (soft: produces warnings and may block with 409), `approve` (hard), and `handoff-complete` (hard re-check, like conditions 1, 3, 5, 6).

```text
G0 kill switch off                      (existing, first)
G1..G8 FR-026 conditions                (existing, unchanged)
G9a mode eligibility                    listing in Eligible(P) under apply_mode/override       -> else 409 policy_not_eligible
G9b single-provider lock                in single modes: INSERT position_apply_lock(position_id,...) in the approve transaction;
                                         PK conflict -> 409 policy_position_taken (race-proof)
G9c employer policy                     one_per_position, max_per_window within window_days, cooldown_days since last sent   -> 409 policy_employer_limit
G9d duplicate acknowledgement           if P already has another application in {draft_prepared..live_approved, sent, tracked}
                                         the approve request MUST carry ack_duplicate=1 (all_aware only)           -> 409 duplicate_unacknowledged
```

Rules:
- G9c and G9d never relax anything; `blocked_companies` and salary/tz overlay checks remain in the existing gate.
- `policy_sha` = sha256 of (apply_mode, override, favourites, ranking_version, chosen listing id, employer policy values) is stored on the application at approval and in the audit evidence; at `handoff-complete` the policy is recomputed: if the decision would now differ the handoff is refused with `policy_changed` and the operator re-approves (no silent override).
- Caps: `cap_daily`/`cap_host` count `sent` + `live_approved` per Listing as today. The new employer caps are additional.
- Dry-run is unaffected in effect but still records which Listing and policy decision it simulated; G9b lock is **not** taken in dry-run (it is a live-only resource), but eligibility is still shown, so dry-run previews the live decision.
- Withdrawal: if an application on P reaches `tracked` with status `replied`/`interview`/`offer`, the console proposes (never performs) withdrawal of pending applications on the other Listings; the existing `withdrawn` state is used by the operator action.

## 5. The duplicate-application warning (default mode)

Default mode deliberately does not hide duplicates: the operator may legitimately apply through two providers (for example one is an ATS-native form and another a referral channel). The system's job is to make duplicates *visible and deliberate*:

- Position page shows a Listings table: provider chip, apply kind, rank R, state of any application on it, apply link (attribution), and a red banner when more than one application is live/sent: "N applications to this employer for this job".
- Draft review for Listing L shows "Same job is also listed on: A, B. Already applied via: A (sent 2026-11-20)".
- Employer policy defaults (settings, per employer or global default): `one_per_position=0`, `max_per_window` unset. A recommended starter profile is offered in settings (one application per Position per 30 days, 3 per employer per 30 days) but is **not** enabled silently: defaults of FR-026 style are opt-in restrictions chosen by the operator.
- UNCONFIRMED (not researched): employers' own tolerance for duplicate submissions. The guidance above is a product decision, not a finding about employers.

## 6. Interaction matrix with existing machinery

| Existing | Interaction |
|---|---|
| Hash approval | unchanged; one approval per application; the approval record adds `listing_id`, `policy_sha` in evidence |
| Typed company-name confirmation | unchanged; shows employer name from the Position and the provider host |
| Kill switch | wins over everything; policy never reads it, only the gate does |
| Live mode reset at restart | unchanged |
| `position_apply_lock` | released when its application reaches a terminal state other than `sent`/`tracked` (`rejected_by_human`, `expired`, `failed_terminal`, `withdrawn`), in the same transaction as the transition, so single modes fall back to the next Listing |
| Tracker/follow-up | per application as today; Position page aggregates and shows the latest status over all Listings |
| Overlay (blocked companies, salary floor, tz window) | evaluated on the merged view and on each Listing; a block from either blocks |

## 7. Console UX (pages and fields)

- **Inbox (`GET /`)**: rows are Positions. Columns: score, title, employer, area, provider chips (all Listings, best-ranked first, struck through if expired), apply-state badge. Filter `provider` matches any Listing; new filter `dups` (multi-Listing only).
- **Position detail (`GET /positions/{id}`, replaces `/postings/{id}` as the main page; the old URL redirects to its Position)**: merged fields with per-field provenance; Listings table; apply-policy box ("Mode: single_favourites; eligible: Greenhouse (rank 0.82); override: none" with a change control); merge/split controls; history of resolution decisions.
- **Queue**: new tab `merges` (`GET /queue/merges`) next to unmapped/ambiguous.
- **Settings**: "Providers" (profile, favourites drag-order, strict toggle, enable/disable), "Apply policy" (mode, employer defaults), "Employer policies" (per-employer table).
- **Draft review**: warnings from section 5; shows the Listing and its provider host in "will hand off to".
- i18n: all new strings through `internal/i18n` in all UI languages (FR-068..070); RTL checked for the Listings table.

## 8. Audit reason codes (new)

`apply_mode_changed`, `provider_favourites_changed`, `position_override_set`, `employer_policy_changed`, `policy_blocked` (evidence: gate id G9a-d, listing, position), `duplicate_acknowledged`, `policy_changed_at_handoff`, `position_apply_lock_taken`, `position_apply_lock_released`. Evidence never includes body text or employer contact data.

## 9. Privacy

New data is metadata about employers and our own choices: `employer`, aliases, policies, favourites, rank stats. No new third-party personal data (recruiter names/emails are not stored; the sanitizer drops them as today). Body-derived signatures are sealed (E). Export (`/data/export`) includes the new tables; `data_delete` per record removes Listing-derived links and signatures; employer rows with no remaining reference are pruned by retention. Tracking history used for the "our history" rank is the operator's own and stays local.

## 10. Test plan

1. Unit/table tests for each gate G9a-d with all three modes x override values; ranking determinism (fixed fixtures, weights from data) and `Wilson` bound; tie-break.
2. **Bypass attempts (extends SC-005's pattern)**: one named attempt per new gate - ineligible Listing approved, second Listing approved while lock held, employer cap exceeded, duplicate without ack, policy changed between approve and handoff, override changed with `live_approved` present - each refused with zero provider-side mutation and an audit row.
3. **Concurrency**: two simultaneous approvals of different Listings of one Position in `single_auto` -> exactly one succeeds (race test on the PK); crash between lock and application update leaves no orphan lock (txcheck).
4. **Fall-back**: expire the best Listing -> next becomes eligible; lock released on `withdrawn`.
5. **E2E (Playwright, dry-run)**: Position with 2 Listings (labelled synthetic pair built from a real posting) in all three modes; screenshots in all UI languages light/dark, vision-checked; real-corpus Positions included wherever the corpus yields real multi-Listing Positions.
6. **Live evidence**: provider ranking inputs come from real `provider_call` and `tracking` rows; with n < 10 the history component must render "insufficient history", asserted.
7. Ranking weights are never claimed validated by user outcomes until n is sufficient; SC states this as PARTIAL until then.
