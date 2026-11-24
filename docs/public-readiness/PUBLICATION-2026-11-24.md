# Public snapshot publication - evidence (2026-11-24)

Public repositories are fed ONLY by `scripts/publish_snapshot.sh` (git archive of github/main, one commit, gate +
secret scan, plain push, never forced). The dev repo's upstream recipes stay `.disabled`; dev history is never pushed to a public host.

Tool proof: `bash scripts/publish_snapshot.sh --prove-failure` -> 30 selftest checks passed, 10/10 mutations caught.
Fixture check: PR #87 replaced the real Greenhouse capture by a synthetic fixture; the snapshot holds only a 404 provenance
record (`greenhouse_vaulttec.provenance.json`, no body) and the doc-derived / synthetic fixtures.

Snapshot `ef648859cd30` (1 commit, 1026 tracked entries, 4 submodule gitlinks to public repos). Fresh clone of EACH host:
`gates/verify_public_readiness.sh` -> "OK: public-readiness scan clean (1026 tracked files, rules H E K C A)" on github, gitlab,
codeberg, gitflic, gitverse, m3w. Boolean history scans over `git log --all -p` on every clone: home paths 0; HN profile URLs 0;
value of any provider-token env var 0; only author identity is the publisher's. Key-format scan over the tree: 0 files.
`go build ./...` on an anonymous https clone of GitHub: rc 0.

| Host | Repository | Provider-reported visibility |
|---|---|---|
| GitHub | https://github.com/vasic-digital/job_seeker_public | public |
| GitLab | https://gitlab.com/vasic-digital/job_seeker_public | public |
| Codeberg | https://codeberg.org/vasic-digital/job_seeker_public | public |
| GitVerse | https://gitverse.ru/vasic-digital/job_seeker_public | public |
| GitFlic | https://gitflic.ru/project/milosvasic/job_seeker_public | private (documented API cannot change visibility; operator step) |
| m3w | https://git.m3w.org/vasic-digital/job_seeker_public | private (permanent, by decision) |

Later updates: `publish_snapshot.sh build --parent <url>` then `push` (new commit on top, fast-forward only).
