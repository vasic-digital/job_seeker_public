# Contributing

This repository is source-available under an all-rights-reserved [LICENSE](LICENSE). It is not open source,
and that determines how contributions work.

- **Permission first.** Do not send a pull request unprompted. Open a GitHub issue describing the change and
  ask for permission. Pull requests without prior written agreement will be closed unread.
- **CLA required.** Because the Owner keeps all rights, any accepted contribution needs a written contributor
  licence agreement (or an explicit written grant on the issue) assigning or licensing it to the Owner. The
  agreement text is provided on request in the issue; none is pre-signed by opening a PR.
- **No personal data.** Never include real CVs, contact details, credentials or application history in an
  issue or PR. The repository keeps such data out of git by design (see README, "Privacy rules").
- **Process.** Work is PR-driven from a task branch with an independent review; merges are merge-commits
  and history is never force-pushed. Local gates (`make gates`) must pass.
- **Security issues** go through [SECURITY.md](SECURITY.md), not issues.
- **Evidence is normalised at write time.** Anything written under `evidence/` goes through the shared normaliser
  (`evnorm.WriteFile` in Go, `bash scripts/evnorm.sh -w <path>` in shell, `make evidence-normalise`) so no home path,
  checkout path, user or host name reaches git. See `docs/public-readiness/EVIDENCE-NORMALISATION.md`.
