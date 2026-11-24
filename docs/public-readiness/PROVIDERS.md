# Publication providers - readiness (measured 2026-11-24)

Nothing was created, flipped or pushed to produce this page. Every probe was a read-only `GET` (or a
local `command -v` / "is the env var non-empty"); no token value was printed or stored. Credentials are
named by environment-variable NAME only. Re-measure rather than trusting a dated table.

Publication to any host except the existing GitHub development remote is refused by
`scripts/upstream_guard.sh` until `docs/public-readiness/AUDIT-GO` exists (written by the separate audit
step, not by this task). m3w is additionally private-only. See `upstreams/README.md`.

## CLIs on this host

| Tool | Present | Auth state (no token shown) |
|---|---|---|
| `gh` | yes | keyring account with `repo`, `read:org`; the `GITHUB_TOKEN` env var is a different, narrower credential that cannot see `vasic-digital/job_seeker` (404) - always run `env -u GITHUB_TOKEN gh ...` |
| `glab` | yes | gitlab.com, user logged in via `GITLAB_TOKEN` |
| `tea` | NO | not needed; Gitea/Forgejo are driven with `curl` + token env var |
| `curl` | yes | used for Codeberg, m3w, GitFlic, GitVerse |
| `go-licenses` | NO | notices are derived by `scripts/gen_third_party_notices.py` instead |

## Per provider

| Provider | Owner to use | Create repo + set visibility (API) | Credential env var NAME | Available now |
|---|---|---|---|---|
| GitHub | org `vasic-digital` | repo EXISTS, currently **private**. Flip: `env -u GITHUB_TOKEN gh api -X PATCH repos/vasic-digital/job_seeker -F private=false` (operator step, after AUDIT-GO) | gh keyring auth (no env var) | yes |
| GitLab | group `vasic-digital` (exists, public) | create: `glab api -X POST projects -f name=job_seeker -f path=job_seeker -F namespace_id=<group id> -f visibility=public`; visibility: `glab api -X PUT projects/vasic-digital%2Fjob_seeker -f visibility=public`. Project absent today (404) | `GITLAB_TOKEN` (or glab auth) | yes |
| Codeberg (Forgejo) | org `vasic-digital` (user is a member; user login is `milosvasic`) | `POST https://codeberg.org/api/v1/orgs/vasic-digital/repos` body `{"name":"job_seeker","private":false}`; visibility: `PATCH /api/v1/repos/vasic-digital/job_seeker {"private":false}`. Repo absent today (404) | `CODEBERG_GITEA_TOKEN` | token set and authenticates (HTTP 200 on `/user`); the write scope was NOT tested |
| m3w (Gitea, `https://git.m3w.org/`) - PRIVATE ONLY | org `vasic-digital` ASSUMED (account is a member of `helix`, `m3w` and `vasic-digital`; which is "the operator's organization" is UNCONFIRMED) | `POST /api/v1/orgs/vasic-digital/repos` body `{"name":"job_seeker","private":true}`; never `private:false`. Repo absent today (404) | `M3W_GITEA_TOKEN` | token set and authenticates (user login `milos85vasic`); write scope NOT tested; SSH port/host key for `git@git.m3w.org` NOT tested |
| GitFlic | owner UNCONFIRMED: user `milosvasic` authenticates; no organisation was found by the one company probe made (404) | API `POST https://api.gitflic.ru/project` (fields UNCONFIRMED against current docs); project absent today (404) | `GITFLIC_TOKEN` | token set and authenticates (HTTP 200 on `/user/me`); create call NOT tested |
| GitVerse | org `vasic-digital` (listed in the account's organisations; user login `milosvasic`) | create-repo endpoint and visibility field UNCONFIRMED (API needs header `Accept: application/vnd.gitverse.object+json;version=1`; a request without it returns HTTP 415) | `GITVERSE_TOKEN` | token set and authenticates; create call NOT tested |

## Missing or unconfirmed (for the operator)

1. m3w: which organisation is the intended one (`vasic-digital` assumed; `helix` and `m3w` are also available).
2. GitFlic: the organisation/owner alias to publish under (user `milosvasic` used in the disabled recipe).
3. Write scopes of the Codeberg, m3w, GitFlic and GitVerse tokens were not exercised (only identity reads); the
   first real create call is the test.
4. GitFlic and GitVerse create-repository API shapes need confirmation against their current docs.
5. SSH push access (keys/host keys/ports) to codeberg.org, git.m3w.org, gitflic.ru and gitverse.ru was not tested.
6. `docs/public-readiness/AUDIT-GO` is intentionally absent; the audit step creates it.
