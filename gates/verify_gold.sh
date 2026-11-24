#!/usr/bin/env bash
# verify_gold.sh - JSK-T058/T059 gate: benchmarks the classifier on the hand-labelled REAL-posting gold set and
# re-runs the determinism check. Enforces SC-003 where it is enforceable (areas with >=30 gold items; the
# non-engineering corpus when >=50) and fails on any regression against evidence/JSK-T058/baseline.json.
# The corpus lives in the PRIVATE data dir (JS_GOLD_CORPUS, default ~/.local/share/job_seeker/corpus/gold-v1);
# only evidence/JSK-T057/{manifest,labels}.tsv are tracked. Corpus absent or hash-mismatched => exit 2 (never a pass).
# Exit 0 pass, 1 finding, 2 cannot determine. --prove-failure: control passes; a rules copy with the
# non-engineering title tokens removed, and a baseline demanding higher precision, must each FAIL (rc 1);
# a tampered corpus must be UNDETERMINED (rc 2).
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
corpus="${JS_GOLD_CORPUS:-$HOME/.local/share/job_seeker/corpus/gold-v1}"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
[ -f "$corpus/corpus.jsonl" ] || { echo "UNDETERMINED: private corpus absent ($corpus); gold gate cannot run"; exit 2; }
bin="$(mktemp -d)"; trap 'rm -rf -- "$bin"' EXIT
(cd "$here" && go build -o "$bin/jsgold" ./cmd/jsgold) || { echo "UNDETERMINED: build failed"; exit 2; }
man="$here/evidence/JSK-T057/manifest.tsv"; lab="$here/evidence/JSK-T057/labels.tsv"; base="$here/evidence/JSK-T058/baseline.json"
bench() { "$bin/jsgold" bench "$1" "$man" "$lab" "$2" "$3" >"$bin/out.txt" 2>&1; }
if [ "${1:-}" = "--prove-failure" ]; then
  mkdir "$bin/r" "$bin/c"; cp "$here"/rules/*.yaml "$bin/r/"
  python3 -I - "$bin/r/area_rules.yaml" <<'PY'
import sys,re
p=sys.argv[1]; s=open(p).read()
a=s.index("non_engineering_title_tokens:"); b=s.index("areas:")
open(p,"w").write(s[:a]+"non_engineering_title_tokens:\n  - recruiter\n"+s[b:])
PY
  sed 's/"Precision": 1,/"Precision": 1.5,/' "$base" >"$bin/base_high.json"
  cp "$corpus/corpus.jsonl" "$bin/c/corpus.jsonl"; sed -i '0,/"title":"/s//"title":"X/' "$bin/c/corpus.jsonl"
  bench "$corpus" "$here/rules" "$base"; c=$?
  bench "$corpus" "$bin/r" "$base"; n=$?
  bench "$corpus" "$here/rules" "$bin/base_high.json"; h=$?
  bench "$bin/c" "$here/rules" "$base"; t=$?
  if [ "$c" -eq 0 ] && [ "$n" -eq 1 ] && [ "$h" -eq 1 ] && [ "$t" -eq 2 ]; then
    echo "PROVE-FAILURE OK: control rc=0; removed non-engineering tokens rc=1; precision regression rc=1; tampered corpus rc=2"; exit 0; fi
  echo "PROVE-FAILURE BAD: control=$c no-noneng=$n regression=$h tampered=$t (want 0 1 1 2)"; exit 1
fi
bench "$corpus" "$here/rules" "$base"; rc=$?; cat "$bin/out.txt"
[ "$rc" -eq 0 ] || { echo "gold bench rc=$rc"; exit "$rc"; }
"$bin/jsgold" determinism "$corpus" "$man" "$lab" "$here/rules"; exit $?
