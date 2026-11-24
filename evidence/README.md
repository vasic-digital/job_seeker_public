# evidence/

Captured runtime evidence per task, `evidence/<task-id>/`.

**Content rule (FR-077).** Provenance manifests, command output and hashes only.
NEVER real posting bodies, CV pixels, template PDFs, overlay values, credentials,
or any personal value. A reviewer rejects a PR that adds such content; `.gitignore`
blocks `evidence/private/` and `evidence/**/*.pdf`.
