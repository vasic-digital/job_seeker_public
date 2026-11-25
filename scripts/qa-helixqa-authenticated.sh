#!/usr/bin/env bash
# qa-helixqa-authenticated.sh - vision-verified autonomous QA of the AUTHENTICATED console (JSK-T084/T111, umbrella 11.4.278).
#
# What it does: boots the REAL jobseekerd (host localhost, private CA, synthetic persona rows), signs in with a CDP virtual
# authenticator, writes the session cookie to a 0600 file (never printed, never committed), and runs
#   helixqa autonomous -web-ca-file <ca> -web-cookie-file <cookies> -platforms web
# over qa/web/console_authenticated.yaml (Inbox, Review queue, Applications, Add posting, Settings, Audit log, Position page).
# Every step is vision-gated against a LOCAL keyless vision endpoint; scripts/qa-helixqa-verify.py then requires every step
# verified on a frame that exists on disk with the cited sha256, and several DISTINCT frames.
#
# Usage:  scripts/qa-helixqa-authenticated.sh            run, verify, write evidence/JSK-T084-auth
#         scripts/qa-helixqa-authenticated.sh --prove-failure   paired mutation proof over the committed evidence
# Needs (env, by name): JSK_HELIXQA_BIN (helixqa built from HelixQA main with the structured-frame fix, PR #14, or later),
#   E2E_NODE_MODULES (a node_modules dir with playwright), HELIX_VISION_ENDPOINT (loopback; scripts/vision-ollama.sh env),
#   HELIX_VISION_MODEL (default qwen2.5vl:3b). Output dir: EVID (default evidence/JSK-T084-auth).
# Exit: 0 verified, 1 a requirement failed, 2 cannot determine (missing tool / endpoint). 2 is never a pass.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"; ROOT="$(cd "$HERE/.." && pwd)"
EVID="${EVID:-$ROOT/evidence/JSK-T084-auth}"
VERIFY="$HERE/qa-helixqa-verify.py"

prove_failure() {
  [ -f "$EVID/report/pipeline-report.json" ] || { echo "cannot determine: no committed evidence at $EVID" >&2; exit 2; }
  W="$(mktemp -d)" || exit 2
  trap 'rm -rf -- "${W:?}"' EXIT
  fail=0
  mk() { rm -rf -- "${W:?}/c"; mkdir -p "$W/c/session-x/screenshots"; cp "$EVID/report/pipeline-report.json" "$W/c/session-x/"; cp "$EVID"/frames/*.png "$W/c/session-x/screenshots/"; cp "$EVID/helixqa.rc" "$W/c/"; }
  expect() { # name want-rc
    python3 "$VERIFY" "$W/c" >"$W/o.txt" 2>&1; rc=$?
    if [ "$rc" = "$2" ]; then echo "PASS $1 (rc $rc)"; else echo "FAIL $1: want rc $2, got $rc"; sed 's/^/   /' "$W/o.txt"; fail=1; fi; }
  mk; expect "control: committed evidence verifies" 0
  mk; rm -f "$W/c/session-x/screenshots/"$(ls "$W/c/session-x/screenshots" | head -1); expect "M1 a cited frame file is missing" 1
  mk; python3 - "$W/c/session-x/pipeline-report.json" <<'PY'
import json,sys
p=sys.argv[1]; r=json.load(open(p)); r["structured"]["cases"][2]["verdict"]="FAILED"; json.dump(r,open(p,"w"))
PY
  expect "M2 one case FAILED" 1
  mk; python3 - "$W/c/session-x/pipeline-report.json" <<'PY'
import json,sys
p=sys.argv[1]; r=json.load(open(p)); s=r["structured"]["cases"][0]["steps"][0]["frame_sha256"]
for c in r["structured"]["cases"]:
    for st in c["steps"]: st["frame_sha256"]=s
json.dump(r,open(p,"w"))
PY
  expect "M3 every step cites the SAME frame (no navigation)" 1
  mk; python3 - "$W/c/session-x/pipeline-report.json" <<'PY'
import json,sys
p=sys.argv[1]; r=json.load(open(p)); r["structured"]["cases"][1]["steps"][0]["vision_status"]="unverified"; json.dump(r,open(p,"w"))
PY
  expect "M4 a step not verified by vision" 1
  mk; echo 1 >"$W/c/helixqa.rc"; expect "M5 helixqa exited non-zero" 1
  mk; rm -f "$W/c/helixqa.rc"; expect "M6 no exit code recorded is undetermined, not a pass" 2
  [ "$fail" = 0 ] && echo "ALL MUTATIONS CAUGHT" && exit 0
  exit 1
}
[ "${1:-}" = "--prove-failure" ] && prove_failure

for v in JSK_HELIXQA_BIN E2E_NODE_MODULES HELIX_VISION_ENDPOINT; do [ -n "${!v:-}" ] || { echo "cannot determine: set $v" >&2; exit 2; }; done
[ -x "$JSK_HELIXQA_BIN" ] || { echo "cannot determine: $JSK_HELIXQA_BIN not executable" >&2; exit 2; }
export HELIX_VISION_MODEL="${HELIX_VISION_MODEL:-qwen2.5vl:3b}"
curl -fsS --max-time 3 "${HELIX_VISION_ENDPOINT%/v1/chat/completions}/api/version" >/dev/null || { echo "cannot determine: vision endpoint down (scripts/vision-ollama.sh start)" >&2; exit 2; }
OUT="$(mktemp -d "${TMPDIR:-/tmp}/jsk-auth-qa.XXXXXX")" || exit 2
trap 'rm -rf -- "${OUT:?}"' EXIT
( cd "$ROOT/tests/e2e" && JSK_QA_OUT="$OUT" JSK_QA_EXTRA="${JSK_QA_EXTRA:--curiosity=false}" go test -run TestHelixQAAuthenticated -count=1 . ) || { echo "run could not be set up" >&2; exit 2; }
rm -rf -- "${EVID:?}"; mkdir -p "$EVID/frames" "$EVID/report"
python3 "$VERIFY" "$OUT" --table "$EVID/summary.tsv" | tee "$EVID/verify.txt"; vrc=${PIPESTATUS[0]}
SESS="$(ls -d "$OUT"/session-* | sort | tail -1)"
cp "$SESS/pipeline-report.json" "$EVID/report/"; cp "$OUT/helixqa.rc" "$EVID/"; cp "$OUT/helixqa.log" "$EVID/helixqa.log"
# only the frames the verdicts cite
python3 - "$SESS" "$EVID" <<'PY'
import json,glob,hashlib,os,shutil,sys
sess,evid=sys.argv[1:3]
r=json.load(open(os.path.join(sess,"pipeline-report.json")))
cited={s["frame_sha256"] for c in r["structured"]["cases"] for s in c["steps"]}
for f in glob.glob(os.path.join(sess,"screenshots","*.png")):
    h=hashlib.sha256(open(f,"rb").read()).hexdigest()
    if h in cited and not os.path.exists(os.path.join(evid,"frames",h[:12]+".png")):
        shutil.copy(f,os.path.join(evid,"frames",h[:12]+".png"))
PY
# the table names saved files; rewrite to the committed names
python3 - "$EVID" <<'PY'
import sys,os
evid=sys.argv[1]; p=os.path.join(evid,"summary.tsv"); L=open(p).read().splitlines()
out=[L[0]]
for l in L[1:]:
    c=l.split("\t"); c[3]=c[2][:12]+".png"; out.append("\t".join(c))
open(p,"w").write("\n".join(out)+"\n")
PY
printf 'vision model: %s (local, loopback only)\nhelixqa: %s\n' "$HELIX_VISION_MODEL" "$("$JSK_HELIXQA_BIN" version 2>&1 | head -1)" >"$EVID/MODEL.txt"
bash "$HERE/evnorm.sh" -w "$EVID" || exit 2
( cd "$EVID" && find . -type f ! -name SHA256SUMS | sort | xargs sha256sum >SHA256SUMS )
exit "$vrc"
