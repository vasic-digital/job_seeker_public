# JSK-016 captured / derived fixtures

| File | Kind | Source | Retrieved |
|---|---|---|---|
| greenhouse_docderived.json | DOC-DERIVED, hand-written, not captured | docs.greenhouse.io/job-board.html field list; the one live read (`vaulttec`) was HTTP 404 | 2026-11-24 |

Each captured file has a `*.provenance.json` (CallProof of the one read: status, resolved IP, TLS subject, response sha256 of the full untrimmed body).
Label: `fixture-input` (section 11.4.289(C)(3)): input to translator unit tests, never evidence that a provider works.
Recorder: `JS_CAPTURE=name|host|url go test ./internal/providers -run TestCaptureSample` (one GET through the production egress client).
| lever_leverdemo_golden.json | ANONYMISED SYNTHETIC (2026-11-24 public-readiness pass); hand-written to the Lever postings shape first discovered from one read of the docs example site; no captured text retained | https://github.com/lever/postings-api (field list) | 2026-11-24 (lever_leverdemo.provenance.json describes the original shape-discovery read only) |
| hn_thread_search_raw.json | captured whole (6581 bytes) | GET https://hn.algolia.com/api/v1/search_by_date?tags=story,author_whoishiring&query=%22Who%20is%20hiring%22&hitsPerPage=1 | 2026-11-24 (hn_thread_search.provenance.json; 200, ip 34.160.168.181, TLS hn.algolia.com) |
| hn_comments_golden.json | ANONYMISED SYNTHETIC: 30 hand-written comments (8 job headers, 1 seeker post, 21 replies) in the documented HN "Who is hiring?" shape; ids 90000001..90000030; no author, author_* tag or captured text retained | HN "Company \| Role \| Location \| ..." header convention; Algolia search_by_date response shape | 2026-11-24 (hn_comments.provenance.json describes the original shape-discovery read only) |
| hn_gold.tsv | hand-labelled expectations over the 30 synthetic comments | labelled by reading, not by running the parser | 2026-11-24 |
HN used TWO reads (one per distinct endpoint: thread discovery and comment page); this is the stated exception to one read per provider, because the adapter cannot work with fewer.
| ashby_Ashby_golden.json | ANONYMISED SYNTHETIC (2026-11-24 public-readiness pass); hand-written to the Ashby posting-api shape first discovered from one read of the docs example board; no captured text retained | developers.ashbyhq.com/docs/public-job-posting-api (field list) | 2026-11-24 (ashby_Ashby.provenance.json describes the original shape-discovery read only) |

remotive_sample.json, remoteok_sample.json, himalayas_sample.json are likewise ANONYMISED SYNTHETIC (2026-11-24): the providers' terms restrict passing their listings to third parties, so no captured listing text is redistributed here. See docs/public-readiness/FIXTURE-TERMS.md.
| arbeitnow_synthetic.json, jobicy_synthetic.json | ANONYMISED SYNTHETIC (JSK-T203): hand-written to the field shapes read from one live GET each on 2026-11-24 (keys and types only; no captured text retained); ids/slugs 900001.., fictional companies | arbeitnow.com/api/job-board-api; jobicy.com/api/v2/remote-jobs | 2026-11-24 |
| workable_synthetic.json, smartrecruiters_synthetic.json, recruitee_synthetic.json, personio_synthetic.xml, teamtailor_synthetic.rss | SYNTHETIC (JSK-T212): hand-written to the vendors' documented/community-described public field shapes (UNCONFIRMED against live responses: NO read was made, no tenant is operator-selected); fictional employer "Examplecorp" on reserved names, ids 9000xxx; no third-party text | docs/research/providers/02-provider-scorecards.md (Workable, SmartRecruiters, Recruitee, Personio, Teamtailor sections) | 2026-11-24 |
