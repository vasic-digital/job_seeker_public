# upstreams/ - publication recipes

`install_upstreams.sh` / `push_all.sh` (constitution toolkit) turn every `upstreams/*.sh` into a git remote.

| File | State | Target | Visibility |
|---|---|---|---|
| `GitHub.sh` | active | `vasic-digital/job_seeker` on GitHub | private today; public only after the audit GO and an operator decision |
| `gitlab.sh.disabled` | disabled | `gitlab.com/vasic-digital/job_seeker_public` | public |
| `codeberg.sh.disabled` | disabled | `codeberg.org/vasic-digital/job_seeker_public` | public |
| `gitflic.sh.disabled` | disabled | `gitflic.ru` (owner unconfirmed) | public |
| `gitverse.sh.disabled` | disabled | `gitverse.ru/vasic-digital/job_seeker_public` | public |
| `m3w.sh.disabled` | disabled | `git.m3w.org/vasic-digital/job_seeker_public` | **PRIVATE ONLY** |

Disabled recipes are inert by file name. `bash scripts/upstream_guard.sh enable NAME` renames one to `.sh` only if
(1) `docs/public-readiness/AUDIT-GO` exists and is non-empty, (2) the target repository already exists, and
(3) for m3w, it is verified private. The same guard runs from `scripts/hooks/pre-push` for every non-GitHub remote.
Proof: `bash scripts/upstream_guard.sh --prove-failure` (hermetic, no network; 18 scenarios + 7 source mutations).
No recipe contains a credential; tokens are read by env-var NAME only (`docs/public-readiness/PROVIDERS.md`).

## Publication model (2026-11-24): snapshot-only, never dev history

The five non-GitHub recipes target `job_seeker_public`, a SEPARATE repository with a fresh one-commit history. It is
fed only by `scripts/publish_snapshot.sh` (export of github/main, gate + secret scan, one new commit per update, plain
fast-forward push, never forced). The `.disabled` recipes are deliberately NOT enabled in the dev repo: `push_all.sh`
would push the private dev history to a public host. They exist to document the targets and remotes the publisher uses.
Proof: `bash scripts/publish_snapshot.sh --prove-failure`.
