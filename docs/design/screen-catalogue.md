# Console screen catalogue (JSK-T081, §11.4.219)

Honesty markers per screen: BUILT-EVIDENCED (route exists and the real-browser journey asserts it), BUILT-PARTIAL, PLANNED. Evidence: `evidence/JSK-T083/journey.log`, `axe-*.json` (light and dark), `bypass-codes.json`.

Surface: one (server-rendered web, desktop and 200% zoom/reflow checked). No native or mobile surface exists in v1.

| Screen | Route | Purpose | Marker | Notes |
|---|---|---|---|---|
| Sign in | `/login` | passkey sign-in and first-passkey bootstrap | BUILT-EVIDENCED | virtual authenticator only; real authenticator is JSK-T120 |
| Inbox | `/` | postings with state, language, source, score-band filters, due follow-ups | BUILT-EVIDENCED | htmx filters, `j`/`k`/`/` shortcuts |
| Review queue | `/queue` | unmapped or ambiguous postings with matcher evidence and a human map action | BUILT-EVIDENCED | `matchstore.ReviewQueue` / `MapHuman` |
| Posting | `/postings/:id` | sanitized text, source link, score breakdown, shortlist, dismiss | BUILT-EVIDENCED | |
| Applications | `/applications` | application list with state and tracking link | BUILT-EVIDENCED | |
| Draft review | `/applications/:id/draft` | PDF viewer with hashes, outbound text, approve and reject, language confirm, live handoff | BUILT-EVIDENCED | all 8 refusal codes asserted over HTTP |
| Tracking | `/applications/:id/tracking` | timeline, status record, follow-up date | BUILT-EVIDENCED | stale-edit conflict asserted |
| Settings and provider health | `/settings` | kill switch, live mode, caps, providers, legal diff, retention | BUILT-EVIDENCED | |
| Audit log | `/audit`, `/audit.jsonl` | hash-chained transitions and export | BUILT-EVIDENCED | chain order asserted |
| Export, delete, backup, restore | n/a | data lifecycle | PLANNED as console screens | CLI only (`jsctl`); no console screen |
| Passkey management | n/a | list and reset passkeys | PLANNED as a screen | `jsctl passkeys reset` only |
| RU and SR posting text | n/a | non-English posting branches | BLOCKED | no RU/SR posting corpus (T057 data) |

Per-platform chrome variants: none (single web surface). Empty, error and refusal states are covered by the journey: empty queue, refused approvals, empty tracker timeline.

## Motion tokens (§11.4.221)

The console defines no motion of its own: `console.css` has zero `transition` or `animation` declarations (VERIFIED by grep) and a global `@media (prefers-reduced-motion: reduce)` rule that disables animation and transition. All durations and easings that exist come from `web/static/design/motion/animations.css` and `components-extended.css` (upstream, hash-pinned). The journey asserts that under `prefers-reduced-motion: reduce` no element has a transition or animation longer than 10 ms. Motion duration and easing token names are the upstream design-system's; this repository does not restate them.

## Open-first tooling record (§11.4.220)

Tooling used: Playwright (Apache-2.0) with Chromium, axe-core (MPL-2.0), vendored htmx 2.0.4 (BSD-2-Clause, SHA-256 pinned), self-hosted OFL fonts. No hosted design platform and no proprietary design tool is used or required. The design source is the umbrella's open CSS token files.

## Internal-tool exemption

Stays OPEN until the operator ratifies Q3 (spec C1). Nothing here claims an exemption.
