#!/usr/bin/env bash
# verify_shadow_parity.sh - JSK-T244 / SC-034 gate: on every gold row (real text of the private corpus) crossed with a
# fixed grid of structured facets and overlays, the plugin eligibility No-set must equal the legacy ScorePosting gate set
# for ALL seven legacy gates, with no permissive row, NO STRICTER ROW (per-row blocked equality) and no outcome changed by the shadow. Absent data is neutral
# in the plugin path (operator decision 2026-11-25) and is REPORTED as rule-unknown=N, never hidden. A gate that never fires is UNDETERMINED (a vacuous run is not a pass).
# Corpus absent or hash-mismatched => exit 2 (never a pass). Exit 0 pass, 1 finding, 2 cannot determine.
# --prove-failure: control passes; a plugin set with junior_or_intern dropped must FAIL (rc 1); a tampered corpus rc 2.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
corpus="${JS_GOLD_CORPUS:-$HOME/.local/share/job_seeker/corpus/gold-v1}"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
[ -f "$corpus/corpus.jsonl" ] || { echo "UNDETERMINED: private corpus absent ($corpus); shadow parity cannot run"; exit 2; }
bin="$(mktemp -d)"; trap 'rm -rf -- "$bin"' EXIT
(cd "$here" && go build -o "$bin/jsgold" ./cmd/jsgold) || { echo "UNDETERMINED: build failed"; exit 2; }
man="$here/evidence/JSK-T057/manifest.tsv"; lab="$here/evidence/JSK-T057/labels.tsv"
run() { "$bin/jsgold" shadow "$1" "$man" "$lab" "$here/rules" "${@:2}"; }
if [ "${1:-}" = "--prove-failure" ]; then
  run "$corpus" >/dev/null 2>&1; c=$?
  run "$corpus" --drop-rule junior_or_intern >/dev/null 2>&1; d=$?
  run "$corpus" --drop-rule duplicate >/dev/null 2>&1; e=$?
  run "$corpus" --drop-rule blocked_company >/dev/null 2>&1; h=$?
  run "$corpus" --strict-unknown >/dev/null 2>&1; u=$?
  mkdir "$bin/c"; cp "$corpus/corpus.jsonl" "$bin/c/corpus.jsonl"; sed -i '0,/"title":"/s//"title":"X/' "$bin/c/corpus.jsonl"
  run "$bin/c" >/dev/null 2>&1; t=$?
  if [ "$c" -eq 0 ] && [ "$d" -eq 1 ] && [ "$e" -eq 1 ] && [ "$u" -eq 1 ] && [ "$h" -eq 1 ] && [ "$t" -eq 2 ]; then
    echo "PROVE-FAILURE OK: control rc=0; dropped junior_or_intern rc=1; dropped duplicate rc=1; strict-unknown (Unknown blocks) rc=1; dropped blocked_company (also the approve-time HardGates shadow) rc=1; tampered corpus rc=2"; exit 0; fi
  echo "PROVE-FAILURE BAD: control=$c drop-junior=$d drop-duplicate=$e strict-unknown=$u drop-blocked=$h tampered=$t (want 0 1 1 1 1 2)"; exit 1
fi
run "$corpus"
