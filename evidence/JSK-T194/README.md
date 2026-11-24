# JSK-T194 evidence: resolver on real postings (FR-082, FR-083, SC-024)

Run 2026-11-24, resolver_version r1 (initial, uncalibrated proposals; see resolver_weights.tsv).

| Input | Postings | Positions | Auto-merges | Queued |
|---|---|---|---|---|
| private real corpus gold-v1 (remotive 15, remoteok 100), `resolver-real-corpus.txt` | 115 | 115 | 0 | 0 |
| copy of the 24 h observer database run-20261124 (`resolver-observer-copy.txt`; original never opened for writing) | 115 | 115 | 0 | 0 |

The observer database holds the same 115 postings as the corpus, so it is NOT a larger sample. Himalayas data is still gated by the ToS/key decision (not run).
Both runs are deterministic (the test runs the corpus twice and compares byte for byte).

Independent adjudication (a separate agent, blind to the resolver, read the private corpus pair by pair): **0 SAME pairs, 1 UNSURE pair**
(remotive:2091166 / remotive:2091177: same employer and title, bodies differ strongly, possibly two parallel programs; same provider, so the resolver
never soft-compares it: invariant 5). Repeated-employer pairs (remoteok:1137869/1137870, 1137868/1137918, 1138068/1138070, 1137871/1137872, 1137972/1137973,
1137974/1137975, remotive:2091135/2091136/2091137, 1680495/1749306) were judged DIFFERENT (different country, level or role); a body-similarity-only
resolver would have falsely merged the template-sharing ones. Measured cross-provider overlap on this corpus: **0 of 15 remotive postings (0%)**.

Reading: zero auto-merges and zero false merges against zero true duplicates. This is **PARTIAL** for SC-024: it shows the resolver does not
false-merge on real data, it does NOT show recall, because the corpus contains no real cross-provider pair. Recall is exercised only by labelled
synthetic pairs (evidence/JSK-T195), which never close a success criterion (FR-083). No finding was raised (T220..T224 stay unused).
