# Evidence normalised for publication

Paths normalised for publication: developer home and sibling-worktree paths in `evidence/*`, `docs/design/design-toolkit-assessment.md` are replaced by `$HOME`, `<repo>` and `<umbrella>` placeholders; the meaning of each record is unchanged.
`evidence/JSK-T113/govulncheck.json` had the `credits` arrays (advisory reporters' names and email addresses) removed from its `osv` records; every finding and every other field is unchanged, and a leading `publication_note` object says so. `gates/hardening_vuln.sh --from-json` still classifies it as 0 called / 0 uncalled.
The gate `gates/verify_public_readiness.sh` keeps these classes of data out of the tree.

## Normalise at write time (how agents and scripts must write evidence)

Evidence that carries a developer path turned the readiness gate RED twice just before a public snapshot. The fix is at the source: every evidence writer passes its output through ONE shared normaliser, and a gate refuses anything that slipped past.

Placeholders (the user and host names are scrubbed only in identity contexts: `name@`, `@host`, `USER=`, `HOSTNAME=`; bare words stay as prose): `<repo>` (this checkout), `<umbrella>` (the directory holding the checkout and its sibling worktrees), `$HOME` (any `/home/<u>` or `/Users/<u>` path), `<USER>`, `<HOST>`, `<TMP>` (a per-run temp directory such as `/tmp/tmp.Ab12Cd` or a `t.TempDir()`; a fixed name like `/tmp/.probe` is not machine-specific and is kept). Normalisation is idempotent.

| Where you write evidence | Do this |
| --- | --- |
| Go test / Go code | `evnorm.WriteFile(path, data, perm)` from `internal/evnorm` instead of `os.WriteFile` |
| Shell gate / script | before the script ends (a `trap ... EXIT` is best), `bash scripts/evnorm.sh -w <file-or-dir>`; treat rc 2 as UNDETERMINED |
| A pipeline or ad-hoc output | `... \| bash scripts/evnorm.sh > evidence/JSK-TNNN/out.txt` (filter mode) |
| Hand edit / agent-written file | `bash scripts/evnorm.sh -w evidence/JSK-TNNN` (or `make evidence-normalise`) before `git add` |

Enforcement, earliest first: `scripts/hooks/pre-commit` (install with `bash scripts/install-hooks.sh`) refuses a commit that stages un-normalised evidence; `gates/verify_evidence_normalised.sh` (part of `make gates`, hence the pre-push hook; `make evidence-check`) scans every tracked and untracked-not-ignored file under `evidence/`; `gates/verify_public_readiness.sh` stays the last line. `bash gates/verify_evidence_normalised.sh --prove-failure` plants each leak class (home path, macOS path, temp dir, checkout path, user, host) in a throwaway repo and requires a finding, requires the placeholders to pass, requires `-w` to clear a finding, and requires rc 2 when it cannot determine. The check prints `file:line` only, never the leaked text.
