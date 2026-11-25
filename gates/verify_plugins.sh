#!/usr/bin/env bash
# verify_plugins.sh - JSK-T232 plugin completeness gate (FR-100, SC-031). Every extension plugin package (a package
# under internal/ext that declares `Info() ext.Info`, registered or not) MUST ship:
#   1. a test file (the conformance/unit run),
#   2. at least one mutation pair in tests/mutations.tsv whose target file lies inside the package (the weakened-copy proof),
#   3. only reason codes that exist in internal/ext/reasons.tsv,
#   4. an Info id that is lower_snake and unique within its kind, and a semver Info version.
# Also: every plugin registered in internal/ext/builtin/builtin.go resolves to such a package.
# Exit 0 pass, 1 finding, 2 cannot determine (missing/unreadable tree, python3 missing).
# --prove-failure: builds throwaway copies of the tree and requires the control to pass and each of five defects
# (mutation rows removed, test file removed, unknown reason code, duplicate id, unreadable tree) to be caught.
# Unless --static is given, the gate also RUNS one mutation row per plugin package through gates/hardening_mutation.sh of the
# checked tree and requires KILLED (a surviving, invalid or non-compiling mutant fails the package; a row that cannot run is rc 2).
# --kill-filter REGEX limits that run to matching package paths (used by the proof). --root DIR checks another tree.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
root="$here"; prove=0
static=0; killfilter=""
while [ $# -gt 0 ]; do case "$1" in --root) root="${2:-}"; shift;; --prove-failure) prove=1;; --static) static=1;; --kill-filter) killfilter="${2:-}"; shift;; esac; shift; done
command -v python3 >/dev/null || { echo "UNDETERMINED: python3 missing"; exit 2; }

check() { KILLFILE="$(mktemp)"; export KILLFILE; python3 - "$1" <<'PY'
import os, re, sys
root = sys.argv[1]
def die(msg, rc=2):
    print(("UNDETERMINED: " if rc == 2 else "FAIL: ") + msg); sys.exit(rc)
ext = os.path.join(root, "internal/ext")
for need in ("internal/ext/builtin/builtin.go", "internal/ext/reasons.tsv", "tests/mutations.tsv"):
    if not os.path.isfile(os.path.join(root, need)):
        die("cannot read " + need)
try:
    reasons = {l.split("\t")[0] for l in open(os.path.join(ext, "reasons.tsv")) if l.strip() and not l.startswith("#")}
    muts = [l.rstrip("\n").split("\t") for l in open(os.path.join(root, "tests/mutations.tsv")) if l.strip() and not l.startswith("#")]
    builtin = open(os.path.join(ext, "builtin/builtin.go")).read()
except OSError as e:
    die("unreadable: %s" % e.filename)
mutfiles = [m[2] for m in muts if len(m) >= 3]
sem = re.compile(r"^\d+\.\d+\.\d+$"); snake = re.compile(r"^[a-z][a-z0-9_]{0,47}$")
info = re.compile(r"\)\s*Info\(\)\s*ext\.Info\s*\{(.*?)\n\}", re.S)
findings, pkgs, seen = [], {}, {}
for dp, dn, fn in os.walk(ext):
    rel = os.path.relpath(dp, root).replace(os.sep, "/")
    if "/testdata" in rel or rel.endswith("testdata"):
        continue
    src = [f for f in fn if f.endswith(".go") and not f.endswith("_test.go")]
    tst = [f for f in fn if f.endswith("_test.go")]
    plug = []
    for f in src:
        t = open(os.path.join(dp, f)).read()
        for m in info.finditer(t):
            body = m.group(1)
            i = re.search(r'ID:\s*(?:"([^"]*)"|l\.p\.Name\(\))', body); k = re.search(r'Kind:\s*"([^"]*)"', body); v = re.search(r'Version:\s*"([^"]*)"', body)
            plug.append((f, i.group(1) if i and i.group(1) is not None else None, k.group(1) if k else None, v.group(1) if v else None))
    if not plug or rel == "internal/ext":
        continue
    pkgs[rel] = (plug, src, tst)
if not pkgs:
    die("no plugin package found under internal/ext: the gate would be vacuous")
for rel, (plug, src, tst) in sorted(pkgs.items()):
    if not tst:
        findings.append("%s: no test file" % rel)
    if not any(f.startswith(rel + "/") for f in mutfiles):
        findings.append("%s: no mutation pair in tests/mutations.tsv" % rel)
    for f in src:
        t = open(os.path.join(root, rel, f)).read()
        for code in re.findall(r'Code:\s*"([a-z0-9_]+)"', t) + re.findall(r'\bno\("([a-z0-9_]+)"', t):
            if code not in reasons:
                findings.append("%s/%s: reason code %r is not in reasons.tsv" % (rel, f, code))
    for f, pid, kind, ver in plug:
        if pid is None:
            continue  # id derived from data (legacy source shim): the registry rejects an invalid one at registration
        if not snake.match(pid):
            findings.append("%s: id %r is not lower_snake" % (rel, pid))
        if not ver or not sem.match(ver):
            findings.append("%s: version %r is not semver" % (rel, ver))
        key = (kind, pid)
        if key in seen:
            findings.append("%s: duplicate %s plugin id %r (also %s)" % (rel, kind, pid, seen[key]))
        seen[key] = rel
# registered plugins resolve to a checked package
imports = dict((a or p.rsplit("/", 1)[1], p) for a, p in re.findall(r'^\s*(?:(\w+)\s+)?"github.com/vasic-digital/job_seeker/(internal/ext/[^"]+)"', builtin, re.M))
regs = re.findall(r"\.Register\((\w+)\.\w+\{", builtin)
if not regs:
    die("no registration line recognised in builtin.go: the gate would be vacuous")
for pk in regs:
    p = imports.get(pk)
    if not p or p not in pkgs:
        findings.append("builtin.go registers %s.* but no plugin package %s was found" % (pk, p))
if findings:
    print("FAIL: %d finding(s)" % len(findings))
    for f in findings: print("  - " + f)
    sys.exit(1)
with open(os.environ["KILLFILE"], "w") as kf:
    for rel in sorted(pkgs):
        row = next((m[0] for m in muts if len(m) >= 3 and m[2].startswith(rel + "/")), None)
        if row: kf.write("%s\t%s\n" % (rel, row))
print("OK: %d plugin package(s), %d registered plugin(s): tests, mutation pairs, closed reason codes, ids and versions" % (len(pkgs), len(regs)))
PY
rc=$?; if [ $rc -ne 0 ]; then rm -f "$KILLFILE"; return $rc; fi
if [ "$static" -eq 0 ]; then kill_rows "$1" "$KILLFILE"; rc=$?; fi
rm -f "$KILLFILE"; return $rc
}

# kill_rows ROOT FILE: run one mutation row per plugin package (3 at a time) and require KILLED. rc 0 all killed, 1 a mutant
# survived / was invalid, 2 the mutation gate could not run.
kill_rows() {
  local r="$1" f="$2" out n=0 bad=0 und=0 line rel id
  [ -x "$r/gates/hardening_mutation.sh" ] || [ -f "$r/gates/hardening_mutation.sh" ] || { echo "UNDETERMINED: gates/hardening_mutation.sh missing, mutants cannot be run"; return 2; }
  out="$(mktemp -d)"
  while IFS=$'\t' read -r rel id; do
    [ -n "$killfilter" ] && ! printf '%s' "$rel" | grep -Eq "$killfilter" && continue
    n=$((n+1)); printf '%s\t%s\n' "$rel" "$id"
  done < "$f" | xargs -P 3 -L 1 bash -c 'rel="$0"; id="$1"; bash "'"$r"'/gates/hardening_mutation.sh" --only "$id" 2>&1 | head -1 > "'"$out"'/$id.res"' 
  for line in "$out"/*.res; do
    [ -e "$line" ] || continue
    id="$(basename "$line" .res)"; v="$(cat "$line")"
    case "$v" in KILLED*) ;; SURVIVED*|INVALID*|CONTROL-RED*|CRASH*|TIMEOUT*|NOASSERT*|MALFORMED*) echo "FAIL: mutant $id not killed: $v"; bad=1;; *) echo "UNDETERMINED: mutant $id could not run: $v"; und=1;; esac
  done
  n=$(ls "$out" | wc -l); rm -rf "$out"
  [ "$n" -gt 0 ] || { echo "UNDETERMINED: no mutation row was run (kill check would be vacuous)"; return 2; }
  [ $bad -eq 0 ] || return 1
  [ $und -eq 0 ] || return 2
  echo "OK: $n mutant(s), one per plugin package, KILLED"; return 0
}

if [ "$prove" -eq 0 ]; then check "$root"; exit $?; fi

t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
mk() { mkdir -p "$t/$1"; (cd "$here" && tar --exclude=.git --exclude=evidence --exclude=web --exclude=docs --exclude=specs -cf - internal/ext tests/mutations.tsv) | tar -xf - -C "$t/$1"; }
bad=""
mk control; static=1; check "$t/control" >/dev/null; c=$?
mk m1; python3 - "$t/m1/tests/mutations.tsv" <<'PY'
import sys
p=sys.argv[1]; open(p,"w").write("".join(l for l in open(p) if "internal/ext/match/keyword/" not in l))
PY
check "$t/m1" >/dev/null; [ $? -eq 1 ] || bad="$bad no-mutation-row"
mk m2; rm -f "$t"/m2/internal/ext/channel/openurl/*_test.go
check "$t/m2" >/dev/null; [ $? -eq 1 ] || bad="$bad no-test-file"
mk m3; sed -i 's/Code: "keyword_hit"/Code: "keyword_hit_typo"/' "$t/m3/internal/ext/match/keyword/keyword.go"
cmp -s "$here/internal/ext/match/keyword/keyword.go" "$t/m3/internal/ext/match/keyword/keyword.go" && bad="$bad m3-changed-nothing"
check "$t/m3" >/dev/null; [ $? -eq 1 ] || bad="$bad unknown-reason-code"
mk m4; sed -i 's/ID: "openurl"/ID: "mailto"/;s/ID: "provider_url"/ID: "mailto"/' "$t/m4/internal/ext/channel/openurl/openurl.go"
check "$t/m4" >/dev/null; [ $? -eq 1 ] || bad="$bad duplicate-id"
mk m5; rm -f "$t/m5/internal/ext/builtin/builtin.go"
check "$t/m5" >/dev/null; [ $? -eq 2 ] || bad="$bad unreadable-tree-not-2"
# kill-check defects run on full copies of the tree, limited to one cheap package
mkfull() { mkdir -p "$t/$1"; (cd "$here" && tar --exclude=.git --exclude=evidence --exclude=web --exclude=docs --exclude=specs -cf - .) | tar -xf - -C "$t/$1"; }
static=0; killfilter='ext/match/keyword'
mkfull k0; check "$t/k0" >/dev/null; [ $? -eq 0 ] || bad="$bad kill-control"
mkfull k1; python3 - "$t/k1/tests/mutations.tsv" <<'PY'
import sys
p=sys.argv[1]; out=[]
for l in open(p):
    f=l.rstrip("\n").split("\t")
    if len(f)>=5 and f[2].startswith("internal/ext/match/keyword/"): f[4]=f[3]   # neutral mutant: must SURVIVE
    out.append("\t".join(f)+"\n")
open(p,"w").write("".join(out))
PY
check "$t/k1" >/dev/null; [ $? -eq 1 ] || bad="$bad surviving-mutant"
mkfull k2; python3 - "$t/k2/tests/mutations.tsv" <<'PY'
import sys
p=sys.argv[1]; out=[]
for l in open(p):
    f=l.rstrip("\n").split("\t")
    if len(f)>=5 and f[2].startswith("internal/ext/match/keyword/"): f[3]="THIS-PATTERN-DOES-NOT-EXIST-ANYWHERE"
    out.append("\t".join(f)+"\n")
open(p,"w").write("".join(out))
PY
check "$t/k2" >/dev/null; [ $? -eq 1 ] || bad="$bad invalid-mutant"
mkfull k3; rm -f "$t/k3/gates/hardening_mutation.sh"; check "$t/k3" >/dev/null; [ $? -eq 2 ] || bad="$bad mutation-gate-missing-not-2"
if [ "$c" -eq 0 ] && [ -z "$bad" ]; then echo "PROVE-FAILURE OK: control rc=0; 4 static defects (mutation pair removed, test file removed, unknown reason code, duplicate id) rejected rc=1, unreadable tree rc=2; kill check: control KILLED rc=0, surviving mutant rc=1, invalid mutant rc=1, mutation gate absent rc=2"; exit 0; fi
echo "PROVE-FAILURE BAD: control=$c survivors:$bad"; exit 1
