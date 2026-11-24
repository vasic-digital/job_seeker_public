# 0001 Go module path

| | |
|---|---|
| Status | ACCEPTED (provisional until operator ratifies Q1) |
| Date | 2026-11-24 |
| Task | JSK-T002 |

Decision: the module path is `github.com/vasic-digital/job_seeker`, matching the repository's
current GitHub home (`gh repo view vasic-digital/job_seeker`). The plan's earlier
`github.com/milos85vasic/job_seeker` is superseded by the repository transfer (umbrella commit 98094d1).
Changing it later is a mechanical `go mod edit -module` plus an import rewrite.
