# Git hooks (JSK-T005)

Local enforcement replaces CI/CD (section 11.4.156). Three hooks live in `scripts/hooks/` and are installed into the repository's **shared** hooks dir (one dir for every worktree) by an explicit operator action.

| Hook | Refuses |
|---|---|
| `commit-msg` | not exactly one `Roadmap-Item: JSK-0NN` (an epic id named in `specs/*/tasks.md`, read at commit time; unreadable data refuses with rc 2); two or more task ids without `Multi-Task-Reason:`; a staged path outside `scope-paths.txt` without a closed-list `Out-of-Scope-Reason:`; any CI/CD file |
| `pre-commit` | staged evidence that is not normalised (home or checkout path, user or host name, temp dir); fix with `bash scripts/evnorm.sh -w <path>` and re-add |
| `pre-push` | a non-fast-forward update (never skippable); runs `make gates` (minutes) unless `JSK_SKIP_GATES=1`; applies the upstream guard to non-GitHub remotes |

## Install and remove

```bash
bash scripts/install-hooks.sh              # idempotent
bash scripts/install-hooks.sh --uninstall  # removes only wrappers carrying the marker (and our own legacy links)
```

Installed hooks are small **wrappers**, not symlinks. Each carries the marker `JSK-HOOK-WRAPPER v1` and, at run time, executes `scripts/hooks/<hook>` from the **current worktree's toplevel**; if that file is absent (a removed worktree, a branch without the hook) it falls back to the main checkout recorded at install time (for a submodule or separate-git-dir checkout the work tree, not the git dir); if neither exists the commit or push is **refused** (fail closed). A foreign hook already in the way is never overwritten or removed (`REFUSED`, exit 1).

Because the wrapper resolves per worktree, a change to a hook is live in a worktree as soon as that worktree has it, and the main checkout's copy is the safety net.

## Skips

`JSK_SKIP_EVIDENCE_CHECK=1` skips pre-commit, `JSK_SKIP_GATES=1` skips the gates inside pre-push. `git commit --no-verify` is not used in this project.

## Proof

`bash gates/verify_install_hooks.sh` runs 25 cases in throwaway repositories (never the real hooks dir): wrappers, refusal and acceptance through the installed wrapper, pre-commit, per-worktree resolution, fallback, fail-closed, dangling legacy link, foreign hook, uninstall. `--prove-failure` runs nine mutated installers that must each fail it. `gates/verify_hooks.sh` covers the hook logic itself. Both run in `make gates`.
