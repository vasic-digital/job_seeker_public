#!/usr/bin/env bash
# hardening_vuln.sh - JSK-T113 (FR-052, FR-074): govulncheck over every package, symbol-level. A finding whose vulnerable
# symbol is CALLED from this code fails the gate (rc 1): a non-stdlib module finding is a dependency bump, a stdlib finding
# is a Go-toolchain bump (the `fixed_version` is printed). Findings in imported-but-uncalled code are listed, never hidden.
# rc 2: govulncheck absent, vulnerability database unreachable, or unparsable output (never a pass).
# govulncheck is found on PATH or at $GOBIN or ~/.cache/claude-tmp/gobin; install: `go install golang.org/x/vuln/cmd/govulncheck@latest`.
# --from-json <file>: classify a saved `govulncheck -format json` stream instead of running it (used by --prove-failure).
# --prove-failure: synthetic streams prove (a) a called non-stdlib finding fails, (b) a called stdlib finding fails and names the
# toolchain, (c) an uncalled-only stream passes, (d) garbage is rc 2.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
classify() { python3 - "$1" <<'PY'
import json,sys
txt=open(sys.argv[1]).read(); dec=json.JSONDecoder(); i=0; called={}; uncalled={}
try:
    while True:
        while i<len(txt) and txt[i].isspace(): i+=1
        if i>=len(txt): break
        o,i=dec.raw_decode(txt,i)
        f=o.get("finding")
        if not f: continue
        tr=f.get("trace") or [{}]; t0=tr[0]
        key=(f["osv"],t0.get("module",""),f.get("fixed_version",""))
        (called if t0.get("function") else uncalled)[key]=1
except Exception as e:
    print("UNPARSABLE:",e); sys.exit(2)
if not called and not uncalled and "config" not in txt: print("UNPARSABLE: no govulncheck messages"); sys.exit(2)
for (o,m,fx) in sorted(uncalled): print(f"NOTE  uncalled {o} in {m} (fixed {fx})")
bad=0
for (o,m,fx) in sorted(called):
    kind="TOOLCHAIN bump needed" if m=="stdlib" else "DEPENDENCY bump needed"
    print(f"FAIL  called {o} in {m}, fixed in {fx}: {kind}"); bad=1
print(f"govulncheck: {len(called)} called finding(s), {len(uncalled)} uncalled")
sys.exit(1 if bad else 0)
PY
}
if [ "${1:-}" = "--from-json" ]; then classify "$2"; exit $?; fi
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  echo '{"config":{"protocol_version":"v1.0.0"}}' > "$t/ok.json"
  echo '{"config":{}}{"finding":{"osv":"GO-X-1","fixed_version":"v1.2.3","trace":[{"module":"example.com/m","version":"v1.0.0","package":"p"}]}}' > "$t/uncalled.json"
  echo '{"config":{}}{"finding":{"osv":"GO-X-2","fixed_version":"v1.2.3","trace":[{"module":"example.com/m","package":"p","function":"F"}]}}' > "$t/dep.json"
  echo '{"config":{}}{"finding":{"osv":"GO-X-3","fixed_version":"go1.99.9","trace":[{"module":"stdlib","package":"net/http","function":"Get"}]}}' > "$t/std.json"
  echo 'not json at all' > "$t/garbage.json"
  classify "$t/ok.json" >/dev/null; a=$?; classify "$t/uncalled.json" >/dev/null; b=$?
  classify "$t/dep.json" | grep -q 'DEPENDENCY bump'; c=${PIPESTATUS[0]}
  classify "$t/std.json" | grep -q 'TOOLCHAIN bump'; d=${PIPESTATUS[0]}
  classify "$t/garbage.json" >/dev/null; e=$?
  if [ $a -eq 0 ] && [ $b -eq 0 ] && [ $c -eq 1 ] && [ $d -eq 1 ] && [ $e -eq 2 ]; then echo "PROVE-FAILURE OK: clean 0, uncalled 0, called dependency 1, called stdlib 1 (toolchain), garbage 2"; exit 0; fi
  echo "PROVE-FAILURE BAD: clean=$a uncalled=$b dep=$c std=$d garbage=$e"; exit 1
fi
gv="$(command -v govulncheck || true)"
for d in "${GOBIN:-}" "$HOME/.cache/claude-tmp/gobin" "$(go env GOPATH 2>/dev/null)/bin"; do [ -z "$gv" ] && [ -x "$d/govulncheck" ] && gv="$d/govulncheck"; done
[ -n "$gv" ] || { echo "UNDETERMINED: govulncheck not installed"; exit 2; }
out="$(mktemp)"; trap 'rm -f -- "$out"' EXIT
(cd "$here" && "$gv" -format json ./... > "$out" 2>"$out.err"); rc=$?
if [ $rc -ne 0 ] && [ $rc -ne 3 ]; then echo "UNDETERMINED: govulncheck rc=$rc: $(head -c 300 "$out.err")"; rm -f "$out.err"; exit 2; fi
rm -f "$out.err"; if [ -n "${JSK_VULN_SAVE:-}" ]; then cp "$out" "$JSK_VULN_SAVE"; bash "$here/scripts/evnorm.sh" -w "$JSK_VULN_SAVE" || { echo "UNDETERMINED: could not normalise $JSK_VULN_SAVE"; exit 2; }; fi   # shared evidence normaliser
classify "$out"
