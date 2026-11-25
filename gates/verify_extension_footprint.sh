#!/usr/bin/env bash
# verify_extension_footprint.sh - JSK-T244 / SC-030 (amended 2026-11-25): the production footprint of adding one reference
# eligibility rule, reader or score feature to an EXISTING extension kind. A change range may touch only:
#   1. files inside a plugin kind directory internal/ext/{ats,channel,compliance,deliver,eligibility,match,reader,render,source}/
#      (the plugin package, its tests and testdata fixtures); kernel directories (exttest, verdictstore, track, builtin/*.go
#      other than builtin.go) are NOT allowed;
#   2. internal/ext/builtin/builtin.go, only import lines and `.Register(` lines, and internal/ext/builtin/builtin_test.go;
#   3. the data rows rules/policy.yaml, internal/ext/reasons.tsv, tests/mutations.tsv, deploy/egress-allowlist.txt;
#   4. non-production paths: evidence/, docs/, specs/, tests/ (other than mutations.tsv above), *_test.go, testdata/.
# Any other production file (a new Profile accessor in internal/ext/ports.go, internal/match, a new registry in builtin.go)
# means the change is a KERNEL or INPUT change, not a plugin addition, and is reported as a violation.
# Usage: verify_extension_footprint.sh [BASE..HEAD | COMMIT]   (a bare COMMIT means COMMIT^..COMMIT)
#        no argument: checks the pinned reference commits in gates/extension_footprint_refs.tsv (expected rc per commit).
#        --prove-failure: seeds defects into a throwaway repository and requires each to be caught.
# Exit 0 clean, 1 violation, 2 cannot determine (commit not in this checkout, git/python3 missing). 2 is never a pass.
set -u
cd "$(dirname "$0")/.."
command -v python3 >/dev/null && command -v git >/dev/null || { echo "UNDETERMINED: python3 or git missing"; exit 2; }
classify() { # $1 = repo dir, $2 = range ; prints violations, rc 0/1/2
python3 - "$1" "$2" <<'PY'
import re, subprocess, sys
repo, rng = sys.argv[1], sys.argv[2]
def git(*a):
    return subprocess.run(["git", "-C", repo, *a], capture_output=True, text=True)
if ".." not in rng:
    rng = rng + "^.." + rng
r = git("diff", "--name-only", rng)
if r.returncode != 0:
    print("UNDETERMINED: git diff %s: %s" % (rng, r.stderr.strip())); sys.exit(2)
DATA = {"rules/policy.yaml", "internal/ext/reasons.tsv", "tests/mutations.tsv", "deploy/egress-allowlist.txt",
        "internal/ext/builtin/builtin_test.go"}
bad = []
files = [f for f in r.stdout.split("\n") if f]
for f in files:
    if f in DATA: continue
    if re.match(r"(evidence|docs|specs|tests)/", f) or f.endswith("_test.go") or "/testdata/" in f: continue
    if re.match(r"internal/ext/(ats|channel|compliance|deliver|eligibility|match|reader|render|source)/", f): continue
    if f == "internal/ext/builtin/builtin.go":
        d = git("diff", "-U0", rng, "--", f)
        if d.returncode != 0:
            print("UNDETERMINED: git diff %s" % f); sys.exit(2)
        for ln in d.stdout.split("\n"):
            if not ln or ln[0] not in "+-" or ln[:3] in ("+++", "---"): continue
            t = ln[1:].strip()
            if t == "" or t.startswith("//") or re.match(r'^"[\w./-]+"$', t) or ".Register(" in t: continue
            bad.append("%s: line is not an import or Register line: %s" % (f, ln.strip()[:100]))
        continue
    bad.append("%s: production file outside the plugin package, data rows and builtin registration" % f)
for b in bad: print("VIOLATION " + b)
print("%d file(s) in %s, %d violation(s)" % (len(files), rng, len(bad)))
sys.exit(1 if bad else 0)
PY
}
prove() {
  t="$(mktemp -d)"; trap 'rm -rf "$t"' EXIT; fail=0
  g() { git -C "$t" "$@" >/dev/null 2>&1; }
  g init -q; g config user.email a@b.c; g config user.name t
  mkdir -p "$t/internal/ext/builtin" "$t/internal/ext/reader/x" "$t/internal/match" "$t/rules" "$t/tests"
  printf 'import (\n\t"a"\n)\nfunc f() {\n\treg(s.Readers.Register(a.R{}))\n}\n' > "$t/internal/ext/builtin/builtin.go"
  echo x > "$t/internal/match/score.go"; echo p > "$t/rules/policy.yaml"; g add .; g commit -qm base
  want() { out="$(classify "$t" "$2")"; rc=$?; [ "$rc" = "$3" ] && echo "ok   $1 rc=$rc" || { echo "BAD  $1 rc=$rc want $3: $out"; fail=1; }; }
  next() { g add .; g commit -qm "$1"; }
  mkdir -p "$t/internal/ext/verdictstore" "$t/internal/ext/reader/y"; echo y > "$t/internal/ext/reader/y/y.go"; echo q >> "$t/rules/policy.yaml"
  sed -i 's/Register(a.R{})/Register(a.R{}),reg(s.Readers.Register(b.R{}))/' "$t/internal/ext/builtin/builtin.go"; next control
  want "control: package + policy line + Register line" HEAD 0
  echo z >> "$t/internal/match/score.go"; next m1; want "M1 production file outside the allowed set" HEAD 1
  printf 'func g() {}\n' >> "$t/internal/ext/builtin/builtin.go"; next m2; want "M2 builtin.go gains non-registration code" HEAD 1
  printf 'x\n' > "$t/internal/ext/verdictstore/v.go"; next m4; want "M4 kernel directory internal/ext/verdictstore" HEAD 1
  want "M3 unknown commit is rc 2, not a pass" deadbeefdeadbeef 2
  [ "$fail" = 0 ] && echo "PROVE-FAILURE OK: control rc=0; 3 defects rejected rc=1; unknown commit rc=2" || echo "PROVE-FAILURE FAILED"
  return $fail
}
if [ "${1:-}" = "--prove-failure" ]; then prove; exit $?; fi
if [ -n "${1:-}" ]; then classify . "$1"; exit $?; fi
worst=0
while IFS=$'\t' read -r c want why; do
  case "$c" in ''|'#'*) continue;; esac
  out="$(classify . "$c")"; rc=$?
  if [ "$rc" = 2 ]; then echo "UNDETERMINED $c: $out"; [ "$worst" = 0 ] && worst=2; continue; fi
  if [ "$rc" = "$want" ]; then echo "OK   $c rc=$rc as pinned ($why)"; else echo "FAIL $c rc=$rc, pinned $want ($why)"; echo "$out" | sed 's/^/     /'; worst=1; fi
done < gates/extension_footprint_refs.tsv
exit $worst
