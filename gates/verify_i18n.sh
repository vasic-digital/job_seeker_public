#!/usr/bin/env bash
# verify_i18n.sh - FR-069 / JSK-T125: every SHIPPED console language has every English key, none untranslated, same printf
# verbs, a recorded status, and "reviewed" only with a PASS verdict from a reviewer different from the translator.
# Exit: 0 shipped catalogues clean, 1 defect, 2 could not determine. --require-all turns "pending languages" into rc 2.
# --prove-failure: seeds each defect into a throwaway copy and requires rc=1 for each, rc=0 for the control.
set -u
cd "$(dirname "$0")/.."
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
bin="$(mktemp -d)"; trap 'rm -rf "$bin"' EXIT
go build -o "$bin/i18naudit" ./cmd/i18naudit || { echo "UNDETERMINED: build failed"; exit 2; }
if [ "${1:-}" = "--prove-failure" ]; then
  src=internal/i18n/messages
  mk() { rm -rf "$bin/m" "$bin/ev"; mkdir -p "$bin/m" "$bin/ev"; cp "$src/en.json" "$bin/m/"; cp "$src/_invariant.json" "$bin/m/";
    python3 - "$bin/m" <<'P'
import json,sys
d=sys.argv[1]; en=json.load(open(d+"/en.json"))
json.dump({k:"[x] "+v for k,v in en.items()},open(d+"/ru.json","w"),ensure_ascii=False)
json.dump({"ru":{"state":"machine_pending_review"}},open(d+"/_status.json","w"))
P
  }
  chk() { "$bin/i18naudit" -dir "$bin/m" -evidence "$bin/ev" >/dev/null 2>&1; echo $?; }
  fail=0
  want() { got="$(chk)"; [ "$got" = "$2" ] && echo "ok   $1 rc=$got" || { echo "BAD  $1 rc=$got want $2"; fail=1; }; }
  mk; want "control (complete ru)" 0
  mk; python3 -c "
import json,sys;p='$bin/m/ru.json';d=json.load(open(p));d.pop(sorted(d)[0]);json.dump(d,open(p,'w'),ensure_ascii=False)"; want "M1 missing key" 1
  mk; python3 -c "
import json;p='$bin/m/ru.json';d=json.load(open(p));k=sorted(d)[0];d[k]=json.load(open('$bin/m/en.json'))[k];json.dump(d,open(p,'w'),ensure_ascii=False)"; want "M2 untranslated key" 1
  mk; python3 -c "
import json;p='$bin/m/ru.json';d=json.load(open(p));d['zz.extra']='x';json.dump(d,open(p,'w'),ensure_ascii=False)"; want "M3 extra key" 1
  mk; python3 -c "
import json;p='$bin/m/ru.json';d=json.load(open(p));d['score.total']='[x] only';json.dump(d,open(p,'w'),ensure_ascii=False)"; want "M4 placeholder verbs differ" 1
  mk; rm "$bin/m/_status.json"; want "M5 shipped without status" 1
  mk; echo '{"ru":{"state":"reviewed"}}' > "$bin/m/_status.json"; want "M6 reviewed without verdict" 1
  mk; echo '{"ru":{"state":"reviewed"}}' > "$bin/m/_status.json"; mkdir -p "$bin/ev/ru"; echo '{"verdict":"PASS","translator":"m1","reviewer":"m1"}' > "$bin/ev/ru/verdict.json"; want "M7 reviewer == translator" 1
  mk; echo '{"ru":{"state":"reviewed"}}' > "$bin/m/_status.json"; mkdir -p "$bin/ev/ru"; echo '{"verdict":"PASS","translator":"m1","reviewer":"m2"}' > "$bin/ev/ru/verdict.json"; want "control reviewed with distinct reviewer" 0
  mk; echo '{' > "$bin/m/ru.json"; want "M8 unparseable catalogue" 1
  mk; rm "$bin/m/en.json"; want "M9 source missing" 1
  mk; "$bin/i18naudit" -dir "$bin/m" -evidence "$bin/ev" -require-all >/dev/null 2>&1; r=$?; [ "$r" = 2 ] && echo "ok   require-all with pending langs rc=2" || { echo "BAD require-all rc=$r"; fail=1; }
  [ "$fail" = 0 ] && echo "PROVE-FAILURE OK" || echo "PROVE-FAILURE BAD"; exit "$fail"
fi
"$bin/i18naudit" "$@"
