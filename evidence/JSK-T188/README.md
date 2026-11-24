# JSK-T188 evidence: ATS URL identity table from real captures

Single reads on 2026-11-24 (one GET per ATS, polite UA, no auth), payloads NOT stored, only public job URLs:

| ATS | Endpoint | HTTP | Outcome |
|---|---|---|---|
| Greenhouse | `GET boards-api.greenhouse.io/v1/boards/gitlab/jobs` | 200 | `absolute_url` = `https://job-boards.greenhouse.io/gitlab/jobs/<id>`: pattern ENABLED |
| Lever | `GET api.lever.co/v0/postings/leverdemo?mode=json` | 200 | `hostedUrl` + `applyUrl` (`/apply` suffix): ENABLED |
| Ashby | `GET api.ashbyhq.com/posting-api/job-board/ashby` | 200 | `jobUrl` + `applyUrl` (`/application` suffix), tenant case is `Ashby` (compared lower-case): ENABLED |
| Workable | `GET apply.workable.com/api/v1/widget/accounts/workable` | 404 | no capture: pattern stays DISABLED |
| SmartRecruiters | `GET api.smartrecruiters.com/v1/companies/smartrecruiters/postings` | 200 | payload carries an API `ref` and no public job URL: pattern stays DISABLED |

Goldens: `internal/position/testdata/ats_captures.tsv` (label real-capture). Table: `internal/position/ats_patterns.tsv`.
Gate: `position.Validate` (enabled row without a real capture fails; capture that does not extract fails), each with a paired mutation test.
`red.txt` is the RED run (symbols undefined before the implementation), `green.txt` the GREEN run.
Honest boundary: `boards.greenhouse.io`, the EU hosts and the `gh_jid` company-page form were NOT captured and stay disabled.
