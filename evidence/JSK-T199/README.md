# JSK-T199 Ashby adapter
- red.txt RED first (undefined NewAshby); green.txt GREEN under -race; FuzzAshbyOne 15 s no findings; mutation pairs jsk016-ashby-uuid/-unlisted/-https-only/-salary-sane KILLED (salary-sane survivor fixed by a test that reaches the bound).
- Live: ONE read through the production egress client of api.ashbyhq.com/posting-api/job-board/Ashby (docs' example board): 200, 78 jobs, 4.6 MB; golden = first 3 raw jobs. Resolves the research item "Ashby ID-in-URL UNCONFIRMED": job id (UUID) is the last segment of jobUrl and applyUrl = jobUrl + /application.
- No second read through the adapter. Apply: handoff only.
- Note: golden descriptions are vendor marketing text and name a vendor employee (public vendor job posting); kept as raw capture.
