# Test Coverage Checklist (§11.4.169): job_seeker MVP

| # | Type | Owner tasks | Evidence form | Honest SKIP conditions |
|---|---|---|---|---|
| 1 | Unit | every [TDD] task, JSK-T107 | test report, mutation pairs | none |
| 2 | Integration | JSK-T108 | provider_call rows, DB counts | provider down: `topology_unsupported` |
| 3 | E2E | JSK-T083 | transcripts, state dumps, screenshots | none |
| 4 | Full-automation | JSK-T109 | run ledger, identical row hashes x3 | none |
| 5 | Challenges | JSK-T110 | per-challenge evidence | none: REQUIRED submodule; unreachable repo is a blocker item (JSK-T006) |
| 6 | HelixQA | JSK-T084 (bank), JSK-T111 (autonomous session) | wire evidence, vision frames | none: REQUIRED submodule; unreachable repo is a blocker item |
| 7 | DDoS | JSK-T112 | throughput, latency, error rate | none |
| 8 | Security | JSK-T113 | scanner output, needle controls | none |
| 9 | Stress + chaos | JSK-T114 | recovery categories | disk-full needs a scratch fs: `topology_unsupported` if absent (topology absence only) |
| 10 | Concurrency | JSK-T115 | row counts, constraint errors | none |
| 11 | Race/deadlock | JSK-T116 | detector output | none |
| 12 | Memory | JSK-T117 | RSS series | none |
| 13 | Benchmark | JSK-T118 | baseline delta | none |

The ledger gate (JSK-T007) re-derives this mapping and fails on drift; per-type mutations are JSK-T119.
