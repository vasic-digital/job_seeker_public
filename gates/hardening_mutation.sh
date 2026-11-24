#!/usr/bin/env bash
# hardening_mutation.sh - JSK-T107 (FR-074, section 1.1): per-package mutation pairs. For every row of
# tests/mutations.tsv the gate copies the source tree to a throwaway directory, requires the named packages to
# PASS unmodified (control), applies exactly one literal mutation, and requires the same packages to FAIL
# (mutation killed). A pattern that does not match exactly once, a mutant that does not compile, or a
# survivor is a failure. 0 all killed, 1 survivor/invalid, 2 cannot determine.
# --prove-failure: feeds a behaviour-neutral mutation (comment only) and a non-matching pattern and requires
# the gate to report SURVIVED and INVALID respectively, and the real table's control run to be green.
# --only <id>: run one row. The real tree is never modified.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null && command -v python3 >/dev/null || { echo "UNDETERMINED: go or python3 missing"; exit 2; }
table="$here/tests/mutations.tsv"; only=""
[ "${1:-}" = "--only" ] && only="${2:-}"

run_table() { # table-file -> prints per-row verdict, returns 1 when any row is not KILLED
  local tf="$1" bad=0 killed=0 total=0 id pk f old new tmp rc
  while IFS=$'\t' read -r id pk f old new; do
    case "$id" in ''|\#*) continue;; esac
    [ -n "$only" ] && [ "$only" != "$id" ] && continue
    total=$((total+1))
    tmp="$(mktemp -d)"
    (cd "$here" && tar --exclude=.git --exclude=evidence -cf - .) | tar -xf - -C "$tmp"
    if ! (cd "$tmp" && CGO_ENABLED=0 go test -count=1 $pk >/dev/null 2>&1); then
      echo "CONTROL-RED  $id: $pk does not pass unmodified"; bad=1; rm -rf "$tmp"; continue
    fi
    if ! python3 - "$tmp/$f" "$old" "$new" <<'PY'
import sys
p,old,new=sys.argv[1],sys.argv[2].encode().decode('unicode_escape'),sys.argv[3].encode().decode('unicode_escape')
s=open(p).read()
if s.count(old)!=1: sys.exit(3)
open(p,'w').write(s.replace(old,new,1))
PY
    then echo "INVALID      $id: pattern does not match exactly once in $f"; bad=1; rm -rf "$tmp"; continue; fi
    out="$(cd "$tmp" && CGO_ENABLED=0 go test -count=1 -timeout 120s $pk 2>&1)"; rc=$?
    if [ $rc -eq 0 ]; then echo "SURVIVED     $id"; bad=1
    elif printf '%s\n' "$out" | grep -qE '^(--- FAIL|FAIL\s)|panic:|timed out' && ! printf '%s\n' "$out" | grep -qE '\[build failed\]|\[setup failed\]'; then echo "KILLED       $id"; killed=$((killed+1))
    else echo "INVALID      $id: mutant does not compile"; bad=1; fi
    rm -rf "$tmp"
  done < "$tf"
  echo "mutations: $killed killed of $total"
  return $bad
}

if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp)"; trap 'rm -f -- "$t"' EXIT
  printf 'neutral-comment\t./internal/fsm\tinternal/fsm/fsm.go\t// PostingCan reports\t// PostingCan reports (neutral)\nnomatch\t./internal/fsm\tinternal/fsm/fsm.go\tTHIS_TEXT_DOES_NOT_EXIST\tx\n' > "$t"
  o="$(run_table "$t")"; rc=$?
  echo "$o"
  if [ $rc -eq 1 ] && echo "$o" | grep -q '^SURVIVED     neutral-comment' && echo "$o" | grep -q '^INVALID      nomatch'; then
    echo "PROVE-FAILURE OK: a neutral mutant is reported SURVIVED and an unmatched pattern INVALID"; exit 0; fi
  echo "PROVE-FAILURE BAD"; exit 1
fi
run_table "$table"; exit $?
