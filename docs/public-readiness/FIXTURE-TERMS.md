# Fixture terms review (public readiness)

Reviewed 2026-11-24 by reading each provider's published terms page (research only, no private data sent).
Question per fixture: may a verbatim capture of that provider's listings live in a PUBLIC repository?
Rule applied: anything not clearly permitted is replaced by a synthetic fixture of the same shape.

| Fixture | Provider terms read | What the terms say | Verdict | Action taken |
|---|---|---|---|---|
| `remotive_sample.json` | Remotive API page (github.com/remotive-com/remote-jobs-api) | Link back to the Remotive URL and mention Remotive as a source; do not "submit Remotive jobs to third Party websites"; listings delayed 24 h; at most about 4 requests per day. No licence to republish listing text. | Not clearly OK (third-party redistribution restricted) | Replaced by synthetic records (ids 9000001/2, fictional companies) |
| `remoteok_sample.json` | RemoteOK API page (remoteok.com/api) | Link back with follow links and mention Remote OK as a source; logo needs written permission. Redistribution of listing text not addressed. | Not clearly OK (silent on redistribution; company descriptions are third-party text) | Replaced by synthetic records |
| `himalayas_sample.json` | Himalayas jobs API docs (himalayas.app/docs/remote-jobs-api) | Visible source credit; apply links must use `applicationLink`; "Do not pass them on to other job boards, aggregators, Google Jobs or LinkedIn Jobs"; full terms at himalayas.app/terms#api. | Not clearly OK (redistribution restricted; full terms not confirmed) | Replaced by synthetic records |
| `ashby_Ashby_golden.json` | Ashby public job posting API docs (developers.ashbyhq.com) | No usage or redistribution terms on the page; the board was a real company's postings. | Not clearly OK (no licence found) | Replaced by a synthetic board written to the documented field shape |
| `lever_leverdemo_golden.json` | Lever postings-api repository README (github.com/lever/postings-api) | No licence or redistribution statement for the API or the `leverdemo` site; postings in `published` state are "publicly viewable" only. The capture also named an individual in demo text. | Not clearly OK (no licence; named a person) | Replaced by synthetic records |
| `hn_comments_golden.json`, `hn_gold.tsv` | Hacker News data via Algolia | Comments belong to individual posters; the capture held usernames, portfolios and contact blurbs of private people. | Replace | Replaced by 30 synthetic comments, identity fields dropped |
| `hn_thread_search_raw.json` | Hacker News public thread record | Official `whoishiring` bot post: rules text and comment ids only, no individual's data. | Keep, attribute Hacker News | Kept as is |
| `greenhouse_stripe_trim.json` (removed) | Greenhouse Job Board API docs (developers.greenhouse.io) | Public read API; no redistribution licence for listing text found; the capture held a real company's posting text and URLs. | Not clearly OK | Replaced by `greenhouse_synthetic_shape.json` (synthetic records, same keys; shape confirmed by one live read, evidence/JSK-T197b) |
| `greenhouse_docderived.json`, `ba_memoryderived.json`, `jsonld_page_docderived.html` | n/a | Hand-written from public field documentation, not captured. | OK | Kept |
| `internal/position/testdata/ats_captures.tsv` | n/a | Stores job URLs only (facts), never payloads. | OK | Kept |

Consequence: the repository contains no verbatim third-party listing text. The `*.provenance.json` files that remain describe the original one-time shape-discovery reads only, and say so.
Live runs still honour each provider's attribution duties through the adapters' `LegalNotice` / attribution code; that is runtime behaviour, not fixture content.

Honest boundary: this is a reading of public terms pages by an engineer, not legal advice. Himalayas' full terms page (terms#api) was not retrievable and Remotive's own site page was unreachable from the fetch tool, so the repository README of its API was used for Remotive.
