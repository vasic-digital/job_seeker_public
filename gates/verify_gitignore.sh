#!/usr/bin/env bash
# verify_gitignore.sh — JSK-T003 / FR-053, FR-077.
# Asserts that private/data-at-rest path patterns are git-ignored and that
# `.env.example` is tracked-able and carries NAMES ONLY (every value empty).
# Exit: 0 all hold, 1 a pattern is not ignored or a value is present, 2 cannot determine.
# --prove-failure: plants a throwaway repo whose .gitignore lacks the patterns and
# requires this gate to FAIL against it (paired mutation, §1.1).
set -u
root="${1:-$(cd "$(dirname "$0")/.." && pwd)}"
if [ "${1:-}" = "--prove-failure" ]; then
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  git -C "$tmp" init -q || { echo "UNDETERMINED: git init failed"; exit 2; }
  : > "$tmp/.gitignore"; printf 'JOBSEEKER_DB_KEY=\n' > "$tmp/.env.example"
  bash "$0" "$tmp" >/dev/null 2>&1; rc=$?
  if [ "$rc" -eq 1 ]; then echo "PROVE-FAILURE OK: empty .gitignore is refused (rc=1)"; exit 0; fi
  echo "PROVE-FAILURE BAD: gate returned rc=$rc against an empty .gitignore"; exit 1
fi
command -v git >/dev/null || { echo "UNDETERMINED: git missing"; exit 2; }
[ -d "$root" ] || { echo "UNDETERMINED: root missing"; exit 2; }
probes=(.env prod.env .env.local data/job.db job.db job.db-wal job.db-shm job.sqlite backups/x.bak
        certs/ca.pem server.key tls.crt corpora/gold.json snapshots/s.json career_private/overlay.yaml
        .cache-career/x evidence/private/x secrets/token a.passphrase)
fail=0
for p in "${probes[@]}"; do
  if git -C "$root" check-ignore -q --no-index -- "$p"; then echo "ignored  $p"; else echo "NOT IGNORED: $p"; fail=1; fi
done
ex="$root/.env.example"
[ -f "$ex" ] || { echo "UNDETERMINED: .env.example missing"; exit 2; }
if git -C "$root" check-ignore -q --no-index -- .env.example; then echo "NOT TRACKABLE: .env.example is ignored"; fail=1; fi
bad="$(grep -vE '^\s*(#|$)' "$ex" | grep -E '=.+' | sed 's/=.*/=<value>/' )"
if [ -n "$bad" ]; then echo ".env.example carries values (names only allowed):"; echo "$bad"; fail=1; fi
[ "$fail" -eq 0 ] && { echo "OK: all probes ignored, .env.example names-only"; exit 0; }
exit 1
