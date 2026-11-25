#!/usr/bin/env bash
# hardening_mutation.sh - JSK-T107 (FR-074, section 1.1): per-package mutation pairs. For every row of
# tests/mutations.tsv the gate copies the source tree to a throwaway directory, requires the named packages to
# PASS unmodified (control), applies exactly one literal mutation, and requires the same packages to FAIL
# (mutation killed). A pattern that does not match exactly once, a mutant that does not compile, a malformed
# row or a survivor is a failure. 0 all killed, 1 survivor/invalid/non-assertion kill, 2 cannot determine.
#
# G09 hardening (docs/operator/gap-register-2026-11-24.md):
#  (a) rows are parsed by python3 on a literal TAB split and handed to the shell on the non-whitespace unit
#      separator 0x1f, so leading/consecutive tabs can no longer collapse. A row must have EXACTLY 5 fields
#      (id, packages, file, old, new); a tab inside a pattern is written as the two characters \t. Anything
#      else is MALFORMED (a failure), never silently re-split.
#  (b) a kill is classified: KILLED = an assertion failed (`--- FAIL:` outnumbers unrecovered panics);
#      CRASH = only an unrecovered panic/fatal error; TIMEOUT = the package hung with no assertion failing; NOASSERT = non-zero exit
#      with no failing test. Only KILLED and KILLED-RACE count as genuine kills; CRASH/TIMEOUT/NOASSERT fail
#      the gate (a hang or a crash proves the mutant is noticed, not that a property is asserted).
#  (c) a mutant that survives the CGO_ENABLED=0 run is re-run with CGO_ENABLED=1 -race. A DATA RACE there is
#      KILLED-RACE (control re-checked under -race first). If the race pass cannot run (no gcc, or
#      MUT_NO_RACE=1) the survivor is UNDETERMINED (rc 2), never a pass.
# --prove-failure: runs a throwaway module through the real run_table and requires every behaviour above.
# --dump-tuples [table]: print the parsed (id,pk,file,old,new) tuples, 0x1f separated (parse-equivalence proof).
# --only <id>: run one row. The real tree is never modified. MUT_TIMEOUT (default 120s) bounds a package run.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null && command -v python3 >/dev/null || { echo "UNDETERMINED: go or python3 missing"; exit 2; }
table="$here/tests/mutations.tsv"; only=""; mode=""
case "${1:-}" in
  --only) only="${2:-}";;
  --prove-failure) mode=prove;;
  --dump-tuples) mode=dump; [ -n "${2:-}" ] && table="$2";;
esac
race_ok=1; { command -v gcc >/dev/null && [ "${MUT_NO_RACE:-}" != 1 ]; } || race_ok=0

# parse_table FILE: one record per row on stdout, fields separated by 0x1f; a row that is not exactly 5
# TAB-separated fields with non-empty id/packages/file/pattern is emitted as "<id>\x1f!MALFORMED\x1f<why>".
parse_table() {
  python3 - "$1" <<'PY'
import sys
for n, line in enumerate(open(sys.argv[1], encoding='utf-8').read().split('\n'), 1):
    if not line.strip() or line.startswith('#'):
        continue
    f = line.split('\t')
    rid = f[0] if f[0].strip() else 'line%d' % n
    if len(f) != 5:
        print('%s\x1f!MALFORMED\x1fline %d has %d tab-separated fields, want exactly 5 (write a tab inside a pattern as \\t)' % (rid, n, len(f)))
    elif any(not f[i].strip() for i in (0, 1, 2, 3)) or f[0] != f[0].strip():
        print('%s\x1f!MALFORMED\x1fline %d has an empty or padded id/packages/file/pattern field' % (rid, n))
    else:
        print('\x1f'.join(f))
PY
}

# classify OUTPUT -> KILLED | CRASH | TIMEOUT | NOASSERT | BUILD
classify() {
  local o="$1" nfail ncrash
  printf '%s\n' "$o" | grep -qE '\[build failed\]|\[setup failed\]' && { echo BUILD; return; }
  nfail="$(printf '%s\n' "$o" | grep -cE '^--- FAIL:')"
  ncrash="$(printf '%s\n' "$o" | grep -E '^(panic:|fatal error:)' | grep -vc '^panic: test timed out')"
  if [ "$nfail" -gt "$ncrash" ]; then echo KILLED   # an assertion failed, even if a later test then hung
  elif printf '%s\n' "$o" | grep -qE '^panic: test timed out'; then echo TIMEOUT
  elif [ "$ncrash" -gt 0 ] || printf '%s\n' "$o" | grep -qE '^signal: '; then echo CRASH
  else echo NOASSERT; fi
}

run_table() { # table-file source-root -> prints per-row verdict; returns 1 any failure, 2 only undetermined
  local mtimeout="${MUT_TIMEOUT:-120s}" parsed tf="$1" src="$2" bad=0 undet=0 killed=0 total=0 id pk f old new tmp rc out kind
  parsed="$(mktemp)"
  if ! parse_table "$tf" > "$parsed"; then echo "UNDETERMINED: the mutation table could not be parsed (invalid UTF-8 or python error)"; rm -f "$parsed"; return 2; fi
  while IFS=$'\x1f' read -r id pk f old new; do
    [ -n "$only" ] && [ "$only" != "$id" ] && continue
    total=$((total+1))
    if [ "$pk" = "!MALFORMED" ]; then echo "MALFORMED    $id: $f"; bad=1; continue; fi
    tmp="$(mktemp -d)"
    (cd "$src" && tar --exclude=.git --exclude=evidence -cf - .) | tar -xf - -C "$tmp"
    if ! (cd "$tmp" && CGO_ENABLED=0 go test -count=1 $pk >/dev/null 2>&1); then
      echo "CONTROL-RED  $id: $pk does not pass unmodified"; bad=1; rm -rf "$tmp"; continue
    fi
    cp "$tmp/$f" "$tmp/$f.orig-g09"
    if ! python3 - "$tmp/$f" "$old" "$new" <<'PY'
import sys
p,old,new=sys.argv[1],sys.argv[2].encode().decode('unicode_escape'),sys.argv[3].encode().decode('unicode_escape')
s=open(p).read()
if s.count(old)!=1: sys.exit(3)
open(p,'w').write(s.replace(old,new,1))
PY
    then echo "INVALID      $id: pattern does not match exactly once in $f"; bad=1; rm -rf "$tmp"; continue; fi
    out="$(cd "$tmp" && CGO_ENABLED=0 go test -count=1 -timeout "$mtimeout" $pk 2>&1)"; rc=$?
    if [ $rc -eq 0 ]; then
      # survived the CGO_ENABLED=0 run: only the race detector could still see it
      if [ "$race_ok" -ne 1 ]; then
        echo "UNDETERMINED $id: survived without -race and the race pass cannot run (gcc missing or MUT_NO_RACE=1)"; undet=1
      else
        out="$(cd "$tmp" && CGO_ENABLED=1 go test -race -count=1 -timeout "$mtimeout" $pk 2>&1)"; rc=$?
        if [ $rc -ne 0 ] && printf '%s\n' "$out" | grep -q 'DATA RACE'; then
          cp "$tmp/$f" "$tmp/$f.mut-g09"; cp "$tmp/$f.orig-g09" "$tmp/$f"
          if (cd "$tmp" && CGO_ENABLED=1 go test -race -count=1 -timeout "$mtimeout" $pk >/dev/null 2>&1); then
            echo "KILLED-RACE  $id"; killed=$((killed+1))
          else echo "CONTROL-RED  $id: $pk does not pass unmodified under -race"; bad=1; fi
        elif [ $rc -ne 0 ]; then echo "SURVIVED     $id (fails only under -race, no data race reported; $(classify "$out"))"; bad=1
        else echo "SURVIVED     $id"; bad=1; fi
      fi
    else
      kind="$(classify "$out")"
      case "$kind" in
        KILLED)   echo "KILLED       $id"; killed=$((killed+1));;
        CRASH)    echo "CRASH        $id: only an unrecovered panic/fatal error, no assertion failed (not a genuine kill)"; bad=1;;
        TIMEOUT)  echo "TIMEOUT      $id: the package hung, no assertion failed (not a genuine kill)"; bad=1;;
        NOASSERT) echo "NOASSERT     $id: non-zero exit but no failing test (not a genuine kill)"; bad=1;;
        *)        echo "INVALID      $id: mutant does not compile"; bad=1;;
      esac
    fi
    rm -rf "$tmp"
  done < "$parsed"
  rm -f "$parsed"
  echo "mutations: $killed genuinely killed of $total"
  [ "$total" -gt 0 ] || { echo "UNDETERMINED: no row was run (empty table or unknown --only id); a vacuous run is never a pass"; return 2; }
  [ $bad -eq 1 ] && return 1
  [ $undet -eq 1 ] && return 2
  return 0
}

prove_failure() {
  local d t o rc fails=0 want
  d="$(mktemp -d)"; t="$(mktemp)"; trap 'rm -rf -- "$d" "$t"' EXIT
  mkdir -p "$d/mm"; printf 'module mm\ngo 1.22\n' > "$d/go.mod"
  cat > "$d/mm/mm.go" <<'GO'
package mm

import "sync"

func Add(a, b int) int {
	return a + b
}

var x int

func Set(mu *sync.Mutex) {
	mu.Lock()
	x = 1
	mu.Unlock()
}

func Get() int { return x }
GO
  cat > "$d/mm/mm_test.go" <<'GO'
package mm

import (
	"sync"
	"testing"
)

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("add")
	}
}

func TestAddHang(t *testing.T) {
	if Add(7, 7) != 14 {
		t.Fatal("add7")
	}
}

func TestSet(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); Set(&mu) }()
	}
	wg.Wait()
	if Get() != 1 {
		t.Fatal("set")
	}
}
GO
  r() { printf '%s\t./mm\tmm/mm.go\t%s\t%s\n' "$1" "$2" "$3"; }
  {
    r neutral-comment 'func Get() int' '// neutral\nfunc Get() int'
    r nomatch 'THIS_TEXT_DOES_NOT_EXIST' x
    r assert-kill 'return a + b' 'return a - b'
    r tab-escape-kill 'mu.Lock()\n\tx = 1' 'mu.Lock()\n\tx = 2'
    r assert-then-hang 'return a + b' 'if a == 2 { return 0 }; if a == 7 { for {} }; return a + b'
    r crash-only 'return a + b' 'var m map[int]int; m[0] = 1; return a + b'
    r hang-only 'return a + b' 'for {}; return a + b'
    r race-only 'mu.Lock()\n\tx = 1\n\tmu.Unlock()' 'x = 1'
    printf 'malformed-consecutive-tabs\t./mm\tmm/mm.go\t\treturn a + b\t\treturn a - b\n'
    printf 'malformed-leading-tab\t\t./mm\tmm/mm.go\treturn a + b\treturn a - b\n'
    printf 'malformed-empty-pattern\t./mm\tmm/mm.go\t\treturn a - b\n'
  } > "$t"
  o="$(MUT_TIMEOUT=8s race_ok=$race_ok run_table "$t" "$d")"; rc=$?
  echo "$o"
  chk() { echo "$o" | grep -q "^$1" || { echo "PROVE-FAILURE BAD: expected '$1'"; fails=$((fails+1)); }; }
  chk 'SURVIVED     neutral-comment'; chk 'INVALID      nomatch'; chk 'KILLED       assert-kill'
  chk 'KILLED       tab-escape-kill'; chk 'KILLED       assert-then-hang'
  chk 'CRASH        crash-only'; chk 'TIMEOUT      hang-only'
  chk 'MALFORMED    malformed-consecutive-tabs'; chk 'MALFORMED    malformed-leading-tab'
  chk 'MALFORMED    malformed-empty-pattern'
  [ $rc -eq 1 ] || { echo "PROVE-FAILURE BAD: table with failures returned rc=$rc, want 1"; fails=$((fails+1)); }
  if [ "$race_ok" -eq 1 ]; then chk 'KILLED-RACE  race-only'
    # the same race-only row with the race pass disabled must be UNDETERMINED, rc 2, never a pass
    printf '%s' "$(r race-only 'mu.Lock()\n\tx = 1\n\tmu.Unlock()' 'x = 1')" > "$t"; echo >> "$t"
    o2="$(race_ok=0 MUT_TIMEOUT=8s run_table "$t" "$d")"; rc2=$?; echo "$o2"
    { echo "$o2" | grep -q '^UNDETERMINED race-only' && [ $rc2 -eq 2 ]; } || { echo "PROVE-FAILURE BAD: race-less run not UNDETERMINED rc 2 (rc=$rc2)"; fails=$((fails+1)); }
  else echo "$o" | grep -q '^UNDETERMINED race-only' || { echo "PROVE-FAILURE BAD: race-only row not UNDETERMINED without gcc"; fails=$((fails+1)); }
    echo "PROVE-FAILURE UNDETERMINED: gcc missing, the race-kill path was not exercised"; exit 2
  fi
  # parse equivalence: the shipped table parses with no MALFORMED record
  if ! parse_table "$here/tests/mutations.tsv" > "$t.p" || [ ! -s "$t.p" ] || grep -q '!MALFORMED' "$t.p"; then rm -f "$t.p"; echo "PROVE-FAILURE BAD: shipped table unparsable, empty or malformed"; fails=$((fails+1)); fi; rm -f "$t.p"
  printf '\xff\xfe\n' > "$t"; o3="$(run_table "$t" "$d")"; rc3=$?; echo "$o3"
  { [ $rc3 -eq 2 ] && echo "$o3" | grep -q '^UNDETERMINED'; } || { echo "PROVE-FAILURE BAD: unparsable table not rc 2 (rc=$rc3)"; fails=$((fails+1)); }
  if [ $fails -eq 0 ]; then
    echo "PROVE-FAILURE OK: neutral SURVIVED, unmatched INVALID, malformed rows MALFORMED, crash CRASH, hang TIMEOUT, race-only KILLED-RACE (UNDETERMINED rc 2 without the race pass)"; exit 0; fi
  exit 1
}

case "$mode" in
  dump) parse_table "$table"; exit 0;;
  prove) prove_failure;;
esac
run_table "$table" "$here"; exit $?
