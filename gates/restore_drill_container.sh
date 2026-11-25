#!/usr/bin/env bash
# JSK-T092 restore drill into a scratch CONTAINER, through the Containers Submodule runtime API (section 11.4.76).
# Takes a REAL snapshot with the real jsctl, runs `jsstack drill` (runtime.Run: throwaway container, snapshot mounted
# read-only, restore + smoke checks inside the container), and proves the drill can fail:
#   good snapshot            -> rc 0, container exit 0, integrity_ok=true, wall time recorded
#   MUTANT corrupted snapshot -> rc 1 (integrity_check fails INSIDE the container)
#   MUTANT absent image       -> rc 2 (the drill never ran: cannot determine, never a pass)
# Usage: gates/restore_drill_container.sh [IMAGE]   (default localhost/job_seeker-gate:dev; must exist locally)
# Exit: 0 all three behave as stated, 1 a behaviour is wrong, 2 cannot determine (no runtime / image / build).
set -u
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image="${1:-${JSK_DRILL_IMAGE:-localhost/job_seeker-gate:dev}}"
bash "$root/scripts/resolve_deps.sh" link >/dev/null 2>&1 || { echo "UNDETERMINED: dependencies do not resolve"; exit 2; }
t="$(mktemp -d "${TMPDIR:-/tmp}/jsk-drill.XXXXXX")" || exit 2
trap 'rm -rf -- "$t"' EXIT
export CGO_ENABLED=0
( cd "$root" && go build -o "$t/jsctl" ./cmd/jsctl && go build -o "$t/jsdrill" ./cmd/jsdrill \
  && cd tools/containers && go build -o "$t/jsstack" ./cmd/jsstack ) || { echo "UNDETERMINED: build failed"; exit 2; }
"$t/jsstack" probe >/dev/null 2>&1 || { echo "UNDETERMINED: no container runtime"; exit 2; }
podman image exists "$image" 2>/dev/null || docker image inspect "$image" >/dev/null 2>&1 || { echo "UNDETERMINED: image $image not present locally (nothing is pulled)"; exit 2; }
mkdir -p "$t/cert"; echo example.com > "$t/allow.txt"
export JOBSEEKER_DATA_DIR="$t/data" JOBSEEKER_CERT_DIR="$t/cert" JOBSEEKER_EGRESS_ALLOWLIST="$t/allow.txt" \
  JOBSEEKER_DB_KEY="drill passphrase 0123456789abcdef" JOBSEEKER_SESSION_KEY="drill session key 0123456789abcdef" \
  JOBSEEKER_HTTPS_ADDR=127.0.0.1:0 JOBSEEKER_EXPORT_PASSPHRASE="export passphrase distinct 4242"
"$t/jsctl" migrate >/dev/null && "$t/jsctl" backup >/dev/null || { echo "UNDETERMINED: could not take a snapshot"; exit 2; }
snap="$(ls "$t"/data/backups/*.bak | head -1)"
bad=0
echo "== good snapshot"; "$t/jsstack" drill -image "$image" -drill "$t/jsdrill" -snapshot "$snap"; rc=$?
[ $rc -eq 0 ] && echo "ok   good snapshot rc=0" || { echo "BAD  good snapshot rc=$rc"; bad=1; }
mkdir -p "$t/badsnap"; cp "$snap" "$t/badsnap/corrupt.snapshot.bak"
python3 - "$t/badsnap/corrupt.snapshot.bak" <<'P'
import sys
b = bytearray(open(sys.argv[1], 'rb').read())
for i in range(4096, min(len(b), 4608)): b[i] ^= 0xFF
open(sys.argv[1], 'wb').write(b)
P
echo "== MUTANT corrupted snapshot"; "$t/jsstack" drill -image "$image" -drill "$t/jsdrill" -snapshot "$t/badsnap/corrupt.snapshot.bak"; rc=$?
[ $rc -eq 1 ] && echo "ok   corrupted snapshot rc=1" || { echo "BAD  corrupted snapshot rc=$rc (want 1)"; bad=1; }
echo "== MUTANT absent image"; "$t/jsstack" drill -image localhost/jsk-no-such-image:none -drill "$t/jsdrill" -snapshot "$snap" 2>&1 | cut -c1-200; rc=${PIPESTATUS[0]}
[ $rc -eq 2 ] && echo "ok   absent image rc=2" || { echo "BAD  absent image rc=$rc (want 2)"; bad=1; }
[ $bad -eq 0 ] && { echo "RESTORE-DRILL-CONTAINER OK"; exit 0; }; echo "RESTORE-DRILL-CONTAINER BAD"; exit 1
