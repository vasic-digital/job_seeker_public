#!/usr/bin/env bash
# hardening_challenges.sh - JSK-T110 (FR-074, section 11.4.169): runs the challenge bank tools/challenges/banks/job_seeker.yaml
# through the Challenges dependency (resolved by scripts/resolve_deps.sh, a separate Go module at tools/challenges) against the REAL
# internal/ packages. Needs challenges + containers resolvable (env JSK_DEP_*, parent umbrella submodules/, or siblings; bash scripts/resolve_deps.sh bootstrap).
# Exit 0 every challenge passed, 1 a challenge failed, 2 cannot determine (dependency absent, build failed). 2 is never a pass.
# --prove-failure (section 1.1): rebuilds the runner with `go build -overlay` against mutated copies of production
# source and requires EVERY mutant to make the bank FAIL (exit 1); the unmutated control must pass:
#   classifier forced to map an area for an empty candidate list      -> jsk-invent-no-area
#   live-mode check removed from Approve                              -> jsk-no-send-without-approval
#   hash-mismatch outcome swallowed in Approve                        -> jsk-hash-mismatch
#   the no-personal-data scanner neutered (scanBytes returns nothing) -> jsk-no-personal-data (scanner control)
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
bash "$here/scripts/resolve_deps.sh" link >/dev/null 2>&1; [ -f "$here/.deps/challenges/go.mod" ] && [ -f "$here/.deps/containers/go.mod" ] || { echo "UNDETERMINED: challenges or containers dependency not resolvable (bash scripts/resolve_deps.sh verify | bootstrap)"; exit 2; }
t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
build() { # out [overlay.json]
  local o="$1"; shift
  (cd "$here/tools/challenges" && go build "$@" -o "$o" ./cmd/jschallenge) 2>"$t/build.err" || { echo "UNDETERMINED: build failed: $(head -c 300 "$t/build.err")"; return 2; }
}
if [ "${1:-}" = "--prove-failure" ]; then
  build "$t/control" || exit 2
  "$t/control" -repo "$here" >/dev/null 2>&1 || { echo "PROVE-FAILURE BAD: control (unmutated) bank does not pass"; exit 1; }
  bad=0
  mut() { # name file python-replace(old,new) expected-failing-id
    local name="$1" f="$here/$2" old="$3" new="$4" id="$5"
    python3 - "$f" "$t/$name.go" "$old" "$new" <<'PY' || { echo "PROVE-FAILURE BAD: mutation $name did not match exactly once"; bad=1; return; }
import sys
s=open(sys.argv[1]).read(); old=sys.argv[3].encode().decode('unicode_escape'); new=sys.argv[4].encode().decode('unicode_escape')
if s.count(old)!=1: sys.exit(3)
open(sys.argv[2],'w').write(s.replace(old,new,1))
PY
    printf '{"Replace":{"%s":"%s"}}' "$f" "$t/$name.go" > "$t/$name.json"
    build "$t/$name" -overlay "$t/$name.json" || { bad=1; return; }
    out="$("$t/$name" -repo "$here" 2>&1)"; rc=$?
    if [ $rc -eq 1 ] && echo "$out" | grep -qE "^(FAILED|ERROR) +$id"; then echo "  mutant $name: killed ($id failed)"; else echo "  mutant $name: SURVIVED or wrong failure (rc=$rc)"; bad=1; fi
  }
  mut classifier-forces-area internal/match/classifier.go 'if len(cands) == 0 {\n\t\treturn m\n\t}' 'if len(cands) == 0 {\n\t\tm.Status, m.AreaID, m.Confidence = StatusMapped, "go", "high"\n\t\treturn m\n\t}' jsk-invent-no-area
  mut live-check-removed internal/draft/service.go 'if !s.Gate.LiveMode() {\n\t\treturn ErrLiveModeOff' 'if false {\n\t\treturn ErrLiveModeOff' jsk-no-send-without-approval
  mut hash-mismatch-swallowed internal/draft/service.go 'outErr = ErrApprovalHashMismatch\n\t\t\treturn nil\n\t\t}\n\t\tif in.Live {' 'outErr = nil\n\t\t\treturn nil\n\t\t}\n\t\tif in.Live {' jsk-hash-mismatch
  mut scanner-neutered internal/pdgate/pdgate.go 'func scanBytes(path string, b []byte, ns []needle) []Finding {\n\tvar out []Finding' 'func scanBytes(path string, b []byte, ns []needle) []Finding {\n\tif true {\n\t\treturn nil\n\t}\n\tvar out []Finding' jsk-no-personal-data
  [ $bad -eq 0 ] && { echo "PROVE-FAILURE OK: control passes, 4 production mutants each make the bank fail on the intended challenge"; exit 0; }
  exit 1
fi
build "$t/jschallenge" || exit 2
mkdir -p "${JSK_EVIDENCE_DIR:-$t}"
"$t/jschallenge" -repo "$here" -out "${JSK_EVIDENCE_DIR:-$t}"
