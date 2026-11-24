#!/usr/bin/env bash
# verify_rawpage.sh - JSK-T019 (FR-054, FR-056, SC-010). Runs the raw-page inspection test: sensitive
# columns must be ciphertext in the DB file, its WAL and its backups, with a plaintext needle control.
# --prove-failure runs the same test with the encryption step MUTATED away and requires it to FAIL.
# Exit 0 pass, 1 fail, 2 cannot determine (no Go toolchain).
set -u
cd "$(dirname "$0")/.."
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
if [ "${1:-}" = "--prove-failure" ]; then
  if JS_RAWPAGE_MUTATE=1 go test -count=1 -run TestRawPageEncryptedColumnsAreCiphertext ./internal/store >/dev/null 2>&1; then
    echo "PROVE-FAILURE BAD: plaintext in an (E) column was NOT detected"; exit 1
  fi
  echo "PROVE-FAILURE OK: a skipped encryption step is detected by the raw-page scan"; exit 0
fi
out="$(go test -count=1 -run TestRawPageEncryptedColumnsAreCiphertext -v ./internal/store 2>&1)"; rc=$?
echo "$out" | tail -5
[ "$rc" -eq 0 ] && { echo "OK: E columns are ciphertext in db, wal and backups; needle control hit"; exit 0; }
exit 1
