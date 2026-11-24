#!/usr/bin/env bash
# verify_ledger.sh - JSK-T007 (FR-074, section 11.4.169). tests/ledger.tsv must carry, for every feature
# it names, a row for each of the 13 test types. A row is valid when:
#   PLANNED: evidence_or_task names a JSK-Tnnn id;  PRESENT: the evidence path exists;
#   SKIP: reason_class in {topology,hardware,credential} AND a non-empty reason.
#   SKIP with reason_class operator_attended (JSK-T119, SC-022): the single human-attended live handoff run. At most ONE
#   such row may exist, its reason must say operator_attended, and it is reported separately: it is never an automated PASS.
# Missing type / unknown state / bad SKIP => rc 1. Unreadable ledger => rc 2.
# --prove-failure (JSK-T119 per-type paired mutation): strips EACH of the 13 types in turn and requires rc 1 for
# each; breaks EACH PRESENT row's evidence path in turn and requires rc 1; breaks a SKIP; and proves the
# operator_attended accounting (one row accepted, two refused, wrong wording refused).
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
TYPES="unit integration e2e full-automation challenges helixqa ddos-load security stress-chaos concurrency-atomicity race-deadlock memory benchmark"
check() { # ledger root
  awk -F'\t' -v types="$TYPES" -v root="$2" '
    BEGIN{n=split(types,T," "); bad=0}
    /^#/ || /^feature\t/ || NF==0 {next}
    { f=$1; t=$2; s=$3; e=$4; rc=$5; r=$6; seen[f SUBSEP t]=1; feat[f]=1
      if (s=="PLANNED") { if (e !~ /^JSK-T[0-9][0-9][0-9]$/) {print "BAD PLANNED row " f "/" t ": needs task id"; bad=1} }
      else if (s=="PRESENT") { cmd="test -e \"" root "/" e "\""; if (system(cmd)!=0) {print "BAD PRESENT row " f "/" t ": evidence path missing"; bad=1} }
      else if (s=="SKIP") { if (rc !~ /^(topology|hardware|credential|operator_attended)$/ || r=="") {print "BAD SKIP row " f "/" t ": closed reason class and reason required"; bad=1}
                            if (rc=="operator_attended") { att++; if (r !~ /operator_attended/) {print "BAD operator_attended row " f "/" t ": reason must be worded operator_attended"; bad=1} } }
      else {print "BAD state \"" s "\" for " f "/" t; bad=1} }
    END{ if (att>1) {print "BAD: " att " operator_attended rows; the single attended handoff run is the only one allowed"; bad=1}
         nf=0; for (f in feat) { nf++; for (i=1;i<=n;i++) if (!((f SUBSEP T[i]) in seen)) {print "MISSING type " T[i] " for feature " f; bad=1} }
         if (nf==0) {print "NO FEATURES in ledger"; bad=1}
         exit bad }' "$1"
}
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  check "$here/tests/ledger.tsv" "$here" >/dev/null || { echo "PROVE-FAILURE BAD: control (real ledger) is not green"; exit 1; }
  for ty in $TYPES; do
    grep -v -P "\t$ty\t" "$here/tests/ledger.tsv" > "$t/a.tsv"
    check "$t/a.tsv" "$here" >/dev/null && { echo "PROVE-FAILURE BAD: stripped type $ty accepted"; exit 1; }
  done
  n=0
  while IFS=$'\t' read -r f ty st ev rest; do
    [ "$st" = PRESENT ] || continue
    awk -F'\t' -v OFS='\t' -v ty="$ty" '$2==ty && $3=="PRESENT"{$4="evidence/DOES-NOT-EXIST"} {print}' "$here/tests/ledger.tsv" > "$t/p.tsv"
    check "$t/p.tsv" "$here" >/dev/null && { echo "PROVE-FAILURE BAD: PRESENT row $ty with a missing evidence path accepted"; exit 1; }
    n=$((n+1))
  done < <(grep -v '^#' "$here/tests/ledger.tsv")
  sed -E 's/^(001-job-seeker-mvp\tmemory\t)[A-Z]+\t[^\t]*\t[^\t]*\t.*/\1SKIP\t\tlaziness\tx/' "$here/tests/ledger.tsv" > "$t/b.tsv"
  check "$t/b.tsv" "$here" >/dev/null && { echo "PROVE-FAILURE BAD: SKIP without closed reason accepted"; exit 1; }
  # operator_attended accounting: one accepted, two refused, wrong wording refused
  base() { grep -v -P '\t(e2e)\t' "$here/tests/ledger.tsv"; }
  { base; printf '001-job-seeker-mvp\te2e\tSKIP\t\toperator_attended\toperator_attended live handoff acceptance run\n'; } > "$t/o1.tsv"
  check "$t/o1.tsv" "$here" >/dev/null || { echo "PROVE-FAILURE BAD: the single operator_attended row was refused"; exit 1; }
  { base | grep -v -P '\tmemory\t'; printf '001-job-seeker-mvp\te2e\tSKIP\t\toperator_attended\toperator_attended a\n001-job-seeker-mvp\tmemory\tSKIP\t\toperator_attended\toperator_attended b\n'; } > "$t/o2.tsv"
  check "$t/o2.tsv" "$here" >/dev/null && { echo "PROVE-FAILURE BAD: two operator_attended rows accepted"; exit 1; }
  { base; printf '001-job-seeker-mvp\te2e\tSKIP\t\toperator_attended\tsome other wording\n'; } > "$t/o3.tsv"
  check "$t/o3.tsv" "$here" >/dev/null && { echo "PROVE-FAILURE BAD: operator_attended with wrong wording accepted"; exit 1; }
  echo "PROVE-FAILURE OK: control green; 13 stripped types, $n broken PRESENT paths, bad SKIP, and operator_attended accounting (1 ok, 2 refused, wording enforced) all behave"; exit 0
fi
[ -r "$here/tests/ledger.tsv" ] || { echo "UNDETERMINED: tests/ledger.tsv unreadable"; exit 2; }
if check "$here/tests/ledger.tsv" "$here"; then
  echo "OK: all 13 test types accounted for ($(grep -c -P '\toperator_attended\t' "$here/tests/ledger.tsv") operator_attended SKIP-justified, never an automated PASS; $(grep -c -P '\tPRESENT\t' "$here/tests/ledger.tsv") PRESENT, $(grep -c -P '\tPLANNED\t' "$here/tests/ledger.tsv") PLANNED, $(grep -c -P '\tSKIP\t' "$here/tests/ledger.tsv") SKIP)"
else exit 1; fi
