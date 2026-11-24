#!/usr/bin/env bash
# hardening_sbom.sh - JSK-T113 (FR-052): generates a CycloneDX SBOM of the module graph with cyclonedx-gomod
# (`go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest`) into evidence/JSK-T113/sbom.cdx.json, then
# CHECKS it: every module that `go list -deps` of the shipped binaries pulls in must appear as a component with its exact
# version. rc 0 complete, 1 a built module is missing/wrong version, 2 tool missing or output unparsable.
# (`mod` mode: the module graph; `app` mode needs a reachable git tag for the main module and the repo has none.)
# --prove-failure: deletes one component from a copy and bumps another's version; both must be refused, control green.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
out="${SBOM_OUT:-$here/evidence/JSK-T113/sbom.cdx.json}"
tool="$(command -v cyclonedx-gomod || true)"
for d in "${GOBIN:-}" "$HOME/.cache/claude-tmp/gobin" "$(go env GOPATH 2>/dev/null)/bin"; do [ -z "$tool" ] && [ -x "$d/cyclonedx-gomod" ] && tool="$d/cyclonedx-gomod"; done
needed() { (cd "$here" && go list -deps -f '{{if .Module}}{{.Module.Path}}@{{.Module.Version}}{{end}}' ./cmd/jobseekerd ./cmd/jsctl | sort -u | grep -v "^github.com/vasic-digital/job_seeker@"); }
verify() { # sbom-file
  needed > "$here/.sbom_needed.tmp" 2>/dev/null
  python3 - "$1" "$here/.sbom_needed.tmp" <<'PY'
import json,sys
try: d=json.load(open(sys.argv[1]))
except Exception as e: print("UNPARSABLE:",e); sys.exit(2)
have={}
def walk(cs):
    for c in cs or []:
        name=c.get("name"); ver=c.get("version")
        have[(name)]=ver
        walk(c.get("components"))
walk(d.get("components"))
want=[l.strip() for l in open(sys.argv[2]) if l.strip()]
if not want: print("UNPARSABLE: no needed modules computed"); sys.exit(2)
bad=0
for w in want:
    p,v=w.rsplit("@",1)
    if have.get(p)!=v: print(f"MISSING/WRONG {p}@{v} (sbom has {have.get(p)})"); bad=1
print(f"sbom: {len(want)} built modules checked, {len(have)} components, bformat={d.get('bomFormat')} spec={d.get('specVersion')}")
sys.exit(1 if bad else 0)
PY
  r=$?; rm -f "$here/.sbom_needed.tmp"; return $r
}
if [ "${1:-}" = "--prove-failure" ]; then
  [ -f "$out" ] || { echo "UNDETERMINED: run the gate once to produce $out"; exit 2; }
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  verify "$out" >/dev/null || { echo "PROVE-FAILURE BAD: control SBOM not green"; exit 1; }
  python3 - "$out" "$t" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); cs=d["components"]
a=json.loads(json.dumps(d)); a["components"]=cs[1:]; json.dump(a,open(sys.argv[2]+"/drop.json","w"))
b=json.loads(json.dumps(d)); b["components"][0]["version"]="v0.0.0-tampered"; json.dump(b,open(sys.argv[2]+"/ver.json","w"))
PY
  verify "$t/drop.json" >/dev/null; x=$?; verify "$t/ver.json" >/dev/null; y=$?
  [ $x -eq 1 ] && [ $y -eq 1 ] && { echo "PROVE-FAILURE OK: dropped component and wrong version both refused, control green"; exit 0; }
  echo "PROVE-FAILURE BAD: drop=$x ver=$y"; exit 1
fi
[ -n "$tool" ] || { echo "UNDETERMINED: cyclonedx-gomod not installed"; exit 2; }
mkdir -p "$(dirname "$out")"
(cd "$here" && CGO_ENABLED=0 "$tool" mod -json -licenses=false -output "$out" . 2>/dev/null) || { echo "UNDETERMINED: cyclonedx-gomod failed"; exit 2; }
verify "$out"
