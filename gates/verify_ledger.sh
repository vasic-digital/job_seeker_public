#!/usr/bin/env bash
# verify_ledger.sh - JSK-T007 (FR-074, section 11.4.169), hardened by G08 (section 11.4.273 measuring-instrument verification).
# tests/ledger.tsv must carry, for every feature it names, a row for each of the 13 test types. A row is valid when:
#   PLANNED: evidence_or_task names a JSK-Tnnn id;
#   PRESENT: the evidence is GENUINE (see below), not merely a path that resolves;
#   SKIP: reason_class in {topology,hardware,credential} AND a non-empty reason.
#   SKIP with reason_class operator_attended (JSK-T119, SC-022): the single human-attended live handoff run. At most ONE
#   such row may exist, its reason must say operator_attended, and it is reported separately: it is never an automated PASS.
# GENUINE evidence (every clause must hold; the first failing clause is named):
#   1. the path exists and is non-empty (a directory: holds at least one genuine file, empty files do not count);
#   2. text/json evidence is normalised (shared normaliser cmd/evnorm: no home or checkout path, user, host, temp dir);
#   3. kind-specific pass signal that is not contradicted:
#        *_test.go   at least one `func Test|Benchmark|Fuzz` declaration;
#        *.json      parses; no ok/pass/passed/success=false, no verdict/status of fail|failed|error (object, or list of objects);
#        *.txt|*.log at least one PASS-signal line (--- PASS, PASS, ok <pkg>, OK:) and NO FAIL/panic line;
#                    a capture that only says SKIP / NOT RUN / BLOCKED carries no pass signal and is therefore NOT PRESENT:
#                    such a row is SKIP with a closed reason class instead.
#        other kinds (png, csv, tsv ...) are non-empty only.
# Missing type / unknown state / bad row => rc 1. Unreadable ledger or no normaliser => rc 2 (never a pass).
# --prove-failure (per-type paired mutation + JSK-T119 accounting + G08 genuineness mutations): see the bottom.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
TYPES="unit integration e2e full-automation challenges helixqa ddos-load security stress-chaos concurrency-atomicity race-deadlock memory benchmark"
EVBIN=""; EVDIR=""
cleanup_ev() { [ -n "$EVDIR" ] && rm -rf -- "$EVDIR"; }
ensure_evnorm() {
  [ -n "$EVBIN" ] && return 0
  command -v go >/dev/null || return 1
  EVDIR="$(mktemp -d)" || return 1
  ( cd "$here" && CGO_ENABLED=0 go build -o "$EVDIR/evnorm" ./cmd/evnorm ) 2>/dev/null || return 1
  EVBIN="$EVDIR/evnorm"
}
json_verdict() { # file -> rc 0 parses and uncontradicted, 1 contradicted, 2 does not parse
  python3 -I - "$1" <<'PY'
import json, sys
try:
    d = json.load(open(sys.argv[1], encoding="utf-8"))
except Exception:
    sys.exit(2)
def bad(o):
    if not isinstance(o, dict):
        return False
    for k in ("ok", "pass", "passed", "success"):
        if o.get(k) is False:
            return True
    for k in ("verdict", "status", "result"):
        v = o.get(k)
        if isinstance(v, str) and v.strip().lower() in ("fail", "failed", "failure", "error"):
            return True
    return False
objs = d if isinstance(d, list) else [d]
sys.exit(1 if any(bad(o) for o in objs) else 0)
PY
}
genuine_file() { # file -> rc 0 genuine; prints the reason on failure
  local f="$1" rc
  [ -s "$f" ] || { echo "empty or missing file"; return 1; }
  case "$f" in
    *_test.go)
      grep -qE '^func (Test|Benchmark|Fuzz)[A-Za-z0-9_]*\(' "$f" || { echo "test file declares no Test/Benchmark/Fuzz function"; return 1; } ;;
    *.json)
      ensure_evnorm || { echo "UNDET no normaliser"; return 2; }
      "$EVBIN" -root "$here" -check "$f" >/dev/null 2>&1 || { echo "un-normalised evidence (home/checkout path, user, host or temp dir)"; return 1; }
      json_verdict "$f"; rc=$?
      [ "$rc" -eq 2 ] && { echo "json does not parse"; return 1; }
      [ "$rc" -eq 1 ] && { echo "json carries a failing verdict"; return 1; } ;;
    *.txt|*.log)
      ensure_evnorm || { echo "UNDET no normaliser"; return 2; }
      "$EVBIN" -root "$here" -check "$f" >/dev/null 2>&1 || { echo "un-normalised evidence (home/checkout path, user, host or temp dir)"; return 1; }
      grep -qE '^\s*(--- FAIL|FAIL($|[ \t:])|panic:)' "$f" && { echo "capture carries a FAIL/panic line"; return 1; }
      grep -qE '^\s*(--- PASS|PASS($|[ \t:])|ok[ \t]|OK:)' "$f" || { echo "no pass-signal line (a SKIP/NOT RUN/BLOCKED-only capture is not PRESENT)"; return 1; } ;;
    *.tsv|*.csv|*.md)
      ensure_evnorm || { echo "UNDET no normaliser"; return 2; }
      "$EVBIN" -root "$here" -check "$f" >/dev/null 2>&1 || { echo "un-normalised evidence"; return 1; } ;;
  esac
  return 0
}
genuine() { # path -> rc 0 genuine; prints reason on failure
  local p="$1" f r last="no non-empty file in directory" rc=1
  if [ -d "$p" ]; then
    while IFS= read -r -d '' f; do
      [ -s "$f" ] || continue
      r="$(genuine_file "$f")"; rc=$?
      [ "$rc" -eq 0 ] && return 0
      [ "$rc" -eq 2 ] && { echo "$r"; return 2; }
      last="$r ($(basename "$f"))"
    done < <(find "$p" -type f -print0 | sort -z)
    echo "directory holds no genuine file: $last"; return 1
  elif [ -e "$p" ]; then genuine_file "$p"
  else echo "evidence path missing"; return 1; fi
}
check() { # ledger root
  local ledger="$1" root="$2" bad=0 att=0 line f t s e rc r why n i
  local -A seen feat
  local -a T; read -r -a T <<<"$TYPES"
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in '#'*|feature$'\t'*|'') continue;; esac
    IFS=$'\037' read -r f t s e rc r <<<"$(printf '%s' "$line" | tr '\t' '\037')"   # US separator: tab would collapse empty fields
    seen["$f|$t"]=1; feat["$f"]=1
    case "$s" in
      PLANNED) [[ "$e" =~ ^JSK-T[0-9][0-9][0-9]$ ]] || { echo "BAD PLANNED row $f/$t: needs task id"; bad=1; } ;;
      PRESENT)
        why="$(genuine "$root/$e")"; n=$?
        if [ "$n" -eq 2 ]; then echo "UNDETERMINED row $f/$t: $why"; return 2
        elif [ "$n" -ne 0 ]; then echo "BAD PRESENT row $f/$t: $why ($e)"; bad=1; fi ;;
      SKIP)
        if ! [[ "$rc" =~ ^(topology|hardware|credential|operator_attended)$ ]] || [ -z "$r" ]; then echo "BAD SKIP row $f/$t: closed reason class and reason required"; bad=1; fi
        if [ "$rc" = operator_attended ]; then att=$((att+1)); [[ "$r" == *operator_attended* ]] || { echo "BAD operator_attended row $f/$t: reason must be worded operator_attended"; bad=1; }; fi ;;
      *) echo "BAD state \"$s\" for $f/$t"; bad=1 ;;
    esac
  done <"$ledger"
  [ "$att" -gt 1 ] && { echo "BAD: $att operator_attended rows; the single attended handoff run is the only one allowed"; bad=1; }
  n=0
  for f in "${!feat[@]}"; do n=$((n+1)); for t in "${T[@]}"; do [ -n "${seen["$f|$t"]:-}" ] || { echo "MISSING type $t for feature $f"; bad=1; }; done; done
  [ "$n" -eq 0 ] && { echo "NO FEATURES in ledger"; bad=1; }
  return "$bad"
}
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"; cleanup_ev' EXIT
  ensure_evnorm || { echo "UNDETERMINED: cannot build cmd/evnorm"; exit 2; }
  L="$here/tests/ledger.tsv"
  check "$L" "$here" >/dev/null; [ $? -eq 0 ] || { echo "PROVE-FAILURE BAD: control (real ledger) is not green"; exit 1; }
  nf=$(grep -v '^#' "$L" | grep -v '^feature' | cut -f1 | sort -u | wc -l)
  for ty in $TYPES; do
    grep -v -P "^[^\t]*\t$ty\t" "$L" > "$t/a.tsv"
    check "$t/a.tsv" "$here" >/dev/null && { echo "PROVE-FAILURE BAD: stripped type $ty accepted"; exit 1; }
  done
  # every feature in turn loses one type: a missing type in ANY feature is refused
  for ft in $(grep -v '^#' "$L" | grep -v '^feature' | cut -f1 | sort -u); do
    grep -v -P "^$ft\tmemory\t" "$L" > "$t/a.tsv"
    check "$t/a.tsv" "$here" >/dev/null && { echo "PROVE-FAILURE BAD: feature $ft without memory row accepted"; exit 1; }
  done
  n=0
  while IFS=$'\t' read -r f ty st ev rest; do
    [ "$st" = PRESENT ] || continue
    awk -F'\t' -v OFS='\t' -v f="$f" -v ty="$ty" '$1==f && $2==ty && $3=="PRESENT"{$4="evidence/DOES-NOT-EXIST"} {print}' "$L" > "$t/p.tsv"
    check "$t/p.tsv" "$here" >/dev/null && { echo "PROVE-FAILURE BAD: PRESENT row $f/$ty with a missing evidence path accepted"; exit 1; }
    n=$((n+1))
  done < <(grep -v '^#' "$L")
  sed -E 's/^(001-job-seeker-mvp\tmemory\t)[A-Z]+\t[^\t]*\t[^\t]*\t.*/\1SKIP\t\tlaziness\tx/' "$L" > "$t/b.tsv"
  check "$t/b.tsv" "$here" >/dev/null && { echo "PROVE-FAILURE BAD: SKIP without closed reason accepted"; exit 1; }
  # operator_attended accounting: one accepted, two refused, wrong wording refused
  base() { grep -v -P '^001-job-seeker-mvp\t(e2e)\t' "$L"; }
  { base; printf '001-job-seeker-mvp\te2e\tSKIP\t\toperator_attended\toperator_attended live handoff acceptance run\n'; } > "$t/o1.tsv"
  check "$t/o1.tsv" "$here" >/dev/null || { echo "PROVE-FAILURE BAD: the single operator_attended row was refused"; exit 1; }
  { base | grep -v -P '^001-job-seeker-mvp\tmemory\t'; printf '001-job-seeker-mvp\te2e\tSKIP\t\toperator_attended\toperator_attended a\n001-job-seeker-mvp\tmemory\tSKIP\t\toperator_attended\toperator_attended b\n'; } > "$t/o2.tsv"
  check "$t/o2.tsv" "$here" >/dev/null && { echo "PROVE-FAILURE BAD: two operator_attended rows accepted"; exit 1; }
  { base; printf '001-job-seeker-mvp\te2e\tSKIP\t\toperator_attended\tsome other wording\n'; } > "$t/o3.tsv"
  check "$t/o3.tsv" "$here" >/dev/null && { echo "PROVE-FAILURE BAD: operator_attended with wrong wording accepted"; exit 1; }
  # G08 genuineness mutations: a one-feature ledger whose e2e row points at a synthetic evidence file under a throwaway root.
  # Every mutation is paired with a control of the same kind that must be green, so a refusal is about the content only.
  R="$t/root"; mkdir -p "$R/ev"
  mini() { # evidence-path -> ledger on stdout
    printf 'feature\ttest_type\tstate\tevidence_or_task\treason_class\treason\n'
    for ty in $TYPES; do
      if [ "$ty" = e2e ]; then printf 'mut\te2e\tPRESENT\t%s\t\t\n' "$1"; else printf 'mut\t%s\tPLANNED\tJSK-T108\t\t\n' "$ty"; fi
    done
  }
  expect() { # want(ok|refuse) label path
    mini "$3" > "$t/m.tsv"; check "$t/m.tsv" "$R" >"$t/m.out" 2>&1; local rc=$?
    if [ "$1" = ok ]   && [ "$rc" -ne 0 ]; then echo "PROVE-FAILURE BAD: control '$2' refused: $(cat "$t/m.out")"; exit 1; fi
    if [ "$1" = refuse ] && [ "$rc" -ne 1 ]; then echo "PROVE-FAILURE BAD: mutation '$2' accepted as PRESENT (rc $rc)"; exit 1; fi
    G=$((G+1))
  }
  G=0
  home_leak="/ho""me/dev/work/x"
  printf -- '--- PASS: TestX (0.01s)\nPASS\nok  \texample.invalid/x\t0.1s\n' > "$R/ev/good.txt"
  printf '{"ok": true, "verdict": "PASS", "n": 3}\n' > "$R/ev/good.json"
  printf 'package x\nimport "testing"\nfunc TestReal(t *testing.T) {}\n' > "$R/ev/good_test.go"
  mkdir -p "$R/ev/gooddir"; : > "$R/ev/gooddir/empty.txt"; cp "$R/ev/good.txt" "$R/ev/gooddir/run.txt"
  expect ok "good txt" ev/good.txt; expect ok "good json" ev/good.json; expect ok "good test.go" ev/good_test.go; expect ok "dir with one genuine file" ev/gooddir
  : > "$R/ev/empty.txt";                                                              expect refuse "empty evidence file" ev/empty.txt
  mkdir -p "$R/ev/emptydir"; : > "$R/ev/emptydir/a.txt";                              expect refuse "directory with only empty files" ev/emptydir
  printf -- '--- PASS: TestA (0.01s)\n--- FAIL: TestB (0.01s)\nFAIL\n' > "$R/ev/fail.txt"; expect refuse "txt with a FAIL line" ev/fail.txt
  printf -- '--- PASS: TestA (0.01s)\npanic: boom\nPASS\n' > "$R/ev/panic.txt";       expect refuse "txt with a panic line" ev/panic.txt
  printf -- '--- PASS: TestA (0.01s)\nsaw %s here\n' "$home_leak" > "$R/ev/leak.txt";  expect refuse "txt with a home path (un-normalised)" ev/leak.txt
  printf -- '--- PASS: TestA (0.01s)\ncd %s/deploy\n' "$here" > "$R/ev/leak2.txt";    expect refuse "txt with the checkout path (un-normalised)" ev/leak2.txt
  printf '{"ok": true, "p": "%s"}\n' "$home_leak" > "$R/ev/leak.json";                expect refuse "json with a home path (un-normalised)" ev/leak.json
  printf '{"ok": true,\n' > "$R/ev/broken.json";                                       expect refuse "json that does not parse" ev/broken.json
  printf '{"ok": false, "n": 1}\n' > "$R/ev/okfalse.json";                             expect refuse "json with ok=false" ev/okfalse.json
  printf '[{"status": "passed"}, {"status": "failed"}]\n' > "$R/ev/listfail.json";    expect refuse "json list with a failed element" ev/listfail.json
  printf 'package x\n// func TestInAComment is not a declaration\nvar _ = 1\n' > "$R/ev/none_test.go"; expect refuse "_test.go with no Test function" ev/none_test.go
  printf '=== RUN   TestLive\n    x_test.go:9: NOT RUN: opt in with X=1\n--- SKIP: TestLive (0.00s)\nSKIP\n' > "$R/ev/skiponly.txt"; expect refuse "honest SKIP-only capture as PRESENT" ev/skiponly.txt
  printf 'JSK-T999 live leg: NOT RUN by design (not a pass, not a failure).\n' > "$R/ev/notrun.txt";  expect refuse "honest NOT RUN note as PRESENT" ev/notrun.txt
  mkdir -p "$R/ev/skipdir"; cp "$R/ev/skiponly.txt" "$R/ev/skipdir/a.txt"; expect refuse "directory holding only a SKIP capture" ev/skipdir
  # the honest alternative to the refused SKIP-only PRESENT row: the same capture's row as SKIP with a closed class is accepted
  { mini ev/skiponly.txt | grep -v -P '^mut\te2e\t'; printf 'mut\te2e\tSKIP\t\ttopology\tlive leg needs an opt-in topology\n'; } > "$t/m.tsv"
  check "$t/m.tsv" "$R" >/dev/null || { echo "PROVE-FAILURE BAD: honest SKIP row with closed class refused"; exit 1; }
  echo "PROVE-FAILURE OK: control green over $nf feature(s); 13 stripped types, per-feature missing type, $n broken PRESENT paths, bad SKIP, operator_attended accounting (1 ok, 2 refused, wording enforced), and $G genuineness cases (4 controls green; empty file/dir, FAIL line, panic line, un-normalised txt and json, unparseable json, ok=false json, no Test func, SKIP-only and NOT-RUN captures all refused as PRESENT) all behave"; exit 0
fi
[ -r "$here/tests/ledger.tsv" ] || { echo "UNDETERMINED: tests/ledger.tsv unreadable"; exit 2; }
trap cleanup_ev EXIT
check "$here/tests/ledger.tsv" "$here"; rc=$?
[ "$rc" -eq 2 ] && exit 2
if [ "$rc" -eq 0 ]; then
  L="$here/tests/ledger.tsv"
  echo "OK: all 13 test types accounted for in $(grep -v '^#' "$L" | grep -v '^feature' | cut -f1 | sort -u | wc -l) feature(s), every PRESENT row checked for genuine evidence ($(grep -c -P '\toperator_attended\t' "$L") operator_attended SKIP-justified, never an automated PASS; $(grep -c -P '\tPRESENT\t' "$L") PRESENT, $(grep -c -P '\tPLANNED\t' "$L") PLANNED, $(grep -c -P '\tSKIP\t' "$L") SKIP)"
else exit 1; fi
