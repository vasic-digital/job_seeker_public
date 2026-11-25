# Operator guide (skeleton)

Grows with the implementation. Each section states what is built today; nothing here promises a feature that does not run.

## What it is
A private, single-operator job-search assistant. See the root README for scope.

## Privacy rules
- No personal value in git: overlay values live only in `career_private/` (read through `CAREER_PRIVATE_DIR`, memory only).
- Secrets are read by env-var NAME; `.env` is git-ignored and mode 0600; nothing is printed or logged.
- `JOBSEEKER_DATA_DIR` must be outside any git work tree (the check exists as `config.CheckDataDirOutsideGit`; it is wired into boot by JSK-T013/T025).
- Evidence directories hold hashes and command output only (see `evidence/README.md`).

## Human in the loop
Dry-run is the default at every layer. A real handoff needs explicit per-application operator approval; no scheduler, token or CLI path can approve or send.

## Git hooks
See [git-hooks.md](git-hooks.md): `bash scripts/install-hooks.sh` installs the commit-msg, pre-commit and pre-push wrappers.

## Running the gates
`make gates`, `make test-all`, `make full` (see the root README).

## Open-question ratification status (specs/001-job-seeker-mvp/analysis.md)
| Q | Topic | Status |
|---|---|---|
| Q1 | Go module path / GitHub org | provisional: `github.com/vasic-digital/job_seeker` (docs/decisions/0001) |
| Q2 | Trusted certificate source (private CA) | decided by operator: LAN-only with private CA; certificate material not yet provisioned |
| Q3 | Console design exemption | decided by operator: full design compliance, no Override |
| Q4, Q6, Q7, Q8, Q12, Q13 | disk encryption, Himalayas key, hosted-endpoint terms, translation host, baseline rule, plaintext limit | OPEN |
| Q5 | Retention defaults | decided by operator: postings 90d, snapshots 14d, audit for DB life, logs 30d |
