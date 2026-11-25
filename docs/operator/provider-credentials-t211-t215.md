# Operator note: what JSK-T211 (Adzuna) and JSK-T215 (hh.ru) need before they can be built

Status 2026-11-24: both tasks stay UNTICKED. Nothing here has been contacted; no API call was made, no key or terms page was fetched, and no adapter exists. This note lists only what the operator must do. Sources: `docs/research/providers/02-provider-scorecards.md` (Adzuna, hh.ru cards).

## JSK-T211 Adzuna
Blocker: the Adzuna terms page returned HTTP 403 to the research fetcher, so limits, caching and attribution rules are UNCONFIRMED.

1. Read, in a browser, the Adzuna developer terms of service (`developer.adzuna.com/docs/terms_of_service`) and the API overview (`developer.adzuna.com/overview`). Record: rate limits, whether results may be stored and for how long, attribution/link-back wording, whether a personal tool qualifies ("free tier meant for evaluation" was seen only in a search snippet).
2. Accept the terms yourself if you agree; register a free application to obtain an `app_id` and `app_key`.
3. Put the credentials in the git-ignored `.env` (mode 0600), by these env-var NAMES (proposed, to be fixed when the adapter is built): `JOBSEEKER_ADZUNA_APP_ID`, `JOBSEEKER_ADZUNA_APP_KEY`. Never commit or paste the values.
4. Record the date you reviewed the terms (`YYYY-MM-DD`) and tell the agent the country codes you want (research lists GB, US, DE, FR, NL; no RS/RU).
5. Add `api.adzuna.com` to `deploy/egress-allowlist.txt` yourself (deliberate per-host decision).

## JSK-T215 hh.ru
Blockers: OAuth app registration and the developer agreement are both unread/unperformed; anonymous search is reported (unverified) to be refused.

1. Read the hh.ru developer agreement (`dev.hh.ru/admin/developer_agreement`) and the API docs (`github.com/hhru/api`, authorization section). Decide whether read-only vacancy search for a personal tool is permitted and what storage/attribution is required.
2. Register an application at `dev.hh.ru` (your own hh.ru account); obtain the client id, client secret and, if required, an application access token.
3. Credentials by env-var NAME in `.env` (mode 0600, proposed): `JOBSEEKER_HHRU_CLIENT_ID`, `JOBSEEKER_HHRU_CLIENT_SECRET`, `JOBSEEKER_HHRU_ACCESS_TOKEN`. Values are never printed or committed.
4. Decide the contact/User-Agent string the API requires (it must not carry personal data that should stay private).
5. Add `api.hh.ru` to `deploy/egress-allowlist.txt` yourself. Scope is read-only search; automated apply/negotiation is out of scope and risks an account ban.
6. Confirm the open question in `docs/research/providers/06-integration-plan-and-backlog.md`: whether RU coverage is worth the registration, or manual intake suffices.

Once an item is done, tell the agent; the adapter then follows the T213 pattern (watchlist/credential gate, robots where applicable, egress allow-list, dry-run default).
