# Contract: HTTP surface

Covers FR-037..FR-048. All browser routes require a valid session except `GET /healthz`, `GET /readyz`, `GET /login`, `POST /login/*` (WebAuthn ceremony) and `GET /static/*`. Every response carries the header set of FR-039; HTML and PDF and API responses carry `Cache-Control: no-store`; `/static/*` hashed names carry `public, max-age=31536000, immutable`.

## Error model (all non-2xx JSON and htmx fragments)

```json
{ "error": { "code": "provider_limit", "message": "i18n key id", "request_id": "r_01H...", "next_allowed_at": "optional RFC 3339" } }
```

Closed code set (extended only by amending this file): `company_confirmation_failed`, `approval_expired`, `duplicate_send`, `language_confirmation_required`, `live_mode_off`, `overlay_absent`, `unauthenticated`, `forbidden_origin`, `bad_host`, `csrf_failed`, `rate_limited`, `step_up_required`, `validation`, `not_found`, `illegal_transition`, `approval_required`, `approval_hash_mismatch`, `kill_switch_on`, `cap_exceeded`, `slot_missing`, `provider_limit`, `egress_denied`, `not_supported`, `cover_stale`, `duplicate_company`, `templates_stale`, `posting_closed`, `gate_undetermined`, `gate_regressed`, `internal`.

## Routes

| Method + path | Purpose | Notes |
|---|---|---|
| GET `/healthz` | liveness | `{ok, version, proto, source_commit, source_dirty}` |
| GET `/readyz` | readiness | 503 with reason codes when DB key, overlay, allow-list or migrations fail (fail closed) |
| GET/POST `/login/...` | WebAuthn begin/finish, bootstrap registration | bootstrap only while zero passkeys; rate-limited |
| POST `/logout` | end session | |
| GET `/` | Inbox | filters: area, language, state, provider, score band |
| GET `/queue` | Review queue (unmapped, ambiguous) | POST `/queue/{posting}/map` body `{area_id}` -> audited `mapped_by=human` |
| GET `/postings/{id}` | Job detail | sanitized text, provider attribution link (rel noreferrer noopener), score explain |
| POST `/postings/{id}/shortlist` / `dismiss` | operator triage | audited |
| POST `/applications/{id}/prepare` | assemble template draft | 409 `slot_missing` if absent |
| GET `/applications/{id}/draft` | Draft review page | shows hashes, outbound action, language decision |
| GET `/applications/{id}/files/{kind}` | stream CV or cover PDF | `inline`, `nosniff`, `no-store`; verifies hash equals frozen hash |
| POST `/applications/{id}/language` | confirm language (required for de / override) | |
| POST `/applications/{id}/approve` | approve | dry-run default; live needs global live mode on, `live=1`, typed company name, fresh step-up (within 5 min AT APPROVAL), caps, kill switch off, overlay present; result `live_approved` (handoff pending). A draft-hash change supersedes the approval and returns to `draft_prepared` |
| POST `/applications/{id}/reject` | reject | reason required |
| POST `/applications/{id}/handoff-complete` | operator records `sent` + confirmation reference | re-checks at this moment: kill switch off, live mode on, draft hash equals approved hash, a non-superseded approval exists, and caps (count of `sent` plus `live_approved` for the day and host must be < cap, otherwise `cap_exceeded`); step-up is NOT required again (it was bound to the approval) |
| GET `/applications` ; POST `/applications/{id}/tracking` | tracker | follow-up date edit |
| GET `/settings` ; POST `/settings/...` | providers, caps, retention, language, kill switch, live-mode toggle | step-up for kill-switch OFF-to-ON is not required, ON-to-OFF is |
| POST `/settings/providers/{name}/refresh` | manual refresh | passes limiter; 429 `provider_limit` with `next_allowed_at` |
| GET `/audit` ; GET `/audit.jsonl` | audit view and export | |
| POST `/data/export` ; POST `/data/delete` ; POST `/data/delete/{kind}/{id}` | export / crypto-shred / per-record delete | delete requires step-up and typed confirmation |
| POST `/settings/passkeys` ; POST `/settings/tokens` ; DELETE `/settings/tokens/{id}` ; POST `/settings/sources/{name}/purge` | second passkey, script tokens, purge source cache | step-up for token creation and purge |
| GET `/api/v1/status` | script status (scope `read`) | Bearer token, no session |
| GET `/metrics` | **separate internal listener only** | no personal labels |
| GET `/static/*` | tokens, components, htmx, fonts, Brotli pre-compressed siblings | |

## Transport

- Listener pair: TCP TLS (h2, h1.1) and UDP QUIC on the same port number; `Alt-Svc: h3=":<boundport>"; ma=86400` only when both binds succeeded; never advertised on failure.
- Content negotiation: `Accept-Encoding` parsed per RFC 9110 (q-values); `Vary: Accept-Encoding`; Brotli ETag suffix; no compression on 206/Range; 304 and HEAD correct.
- 0-RTT disabled (replay of non-idempotent POSTs).

## CSRF and origin

Non-GET browser requests need header `X-CSRF-Token` (htmx `hx-headers`) or form field `_csrf`, matching the session's synchronizer token; `Origin` must equal a configured origin and `Sec-Fetch-Site` must not be `cross-site`; `Host` must be in `JOBSEEKER_ALLOWED_HOSTS` (else 421).

## Script API (scoped tokens)

`Authorization: Bearer <token>`; scope `read` = GET status, queue counts, provider health; scope `dryrun` = additionally POST `/applications/{id}/prepare` and dry-run approvals are NOT allowed; a token can never approve, send, or change settings. A contract test enumerates every route and asserts tokens are refused where required.
