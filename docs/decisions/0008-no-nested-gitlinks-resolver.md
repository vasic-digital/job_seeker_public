# ADR 0008: no nested own-org gitlinks; dependencies resolved by scripts/resolve_deps.sh

* Status: accepted (2026-11-24, operator decision: resolve cascade check C7 by removing the four nested gitlinks)
* Supersedes the layout (not the pins or the catalogue verdicts) of ADR 0003 and ADR 0007.

## Problem

`job_seeker` is itself an owned submodule of the `vasic` umbrella, and it declared four own-org gitlinks
(containers, challenges, qa = HelixQA, security). Constitution §11.4.28(C) forbids an owned submodule nesting
own-org submodules; the umbrella's `scripts/verify-governance-cascade.sh` check C7 reported FAIL for exactly
those four.

## Decision

* `.gitmodules` and the four gitlinks are removed; the index records no mode-160000 entry.
* `helix-deps.yaml` stays the single manifest: `name`, `ssh_url`, `ref` (pinned SHA), `dir` (sibling directory name).
* `scripts/resolve_deps.sh` finds each dependency in this order: (1) env `JSK_DEP_<NAME>`, (2) the parent
  umbrella's `submodules/<name>`, (3) sibling directory `../<dir>`. A candidate is accepted only if its HEAD
  contains the pinned ref; otherwise it is reported as DRIFT, never silently used. An env override that does not
  resolve is an error with no fallthrough.
* `scripts/resolve_deps.sh bootstrap` clones any missing dependency as a sibling over public https and checks out
  the pinned SHA detached. It never touches an existing directory.
* `scripts/resolve_deps.sh link` (`make deps`) creates the gitignored `.deps/<name>` symlinks that the
  `replace` directives of `tools/containers` and `tools/challenges` point at.
* `gates/verify_pins.sh` now asserts no gitlink / no tracked `.gitmodules`, and that every manifest ref is
  contained in its resolved checkout; rc 2 when a dependency cannot be resolved (never a pass).
  Both scripts carry `--prove-failure` paired mutations.

## Consequence

A consumer checkout needs the dependencies side by side or in the umbrella; the umbrella's own
`submodules/<name>` must be at or past the pin, else the resolver reports DRIFT and `bootstrap` (siblings) or an env override is the way out.
