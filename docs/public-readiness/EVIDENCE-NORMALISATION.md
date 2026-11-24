# Evidence normalised for publication

Paths normalised for publication: developer home and sibling-worktree paths in `evidence/*`, `docs/design/design-toolkit-assessment.md` are replaced by `$HOME`, `<repo>` and `<umbrella>` placeholders; the meaning of each record is unchanged.
`evidence/JSK-T113/govulncheck.json` had the `credits` arrays (advisory reporters' names and email addresses) removed from its `osv` records; every finding and every other field is unchanged, and a leading `publication_note` object says so. `gates/hardening_vuln.sh --from-json` still classifies it as 0 called / 0 uncalled.
The gate `gates/verify_public_readiness.sh` keeps these classes of data out of the tree.
