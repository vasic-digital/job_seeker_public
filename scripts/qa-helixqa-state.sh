#!/usr/bin/env bash
# qa-helixqa-state.sh - state-changing, vision-gated, read-back-verified HelixQA session over the console (JSK-T084/T111, umbrella 11.4.278).
#
# usage: scripts/qa-helixqa-state.sh [state|stepup]        run and write the evidence directory
#        scripts/qa-helixqa-state.sh --prove-failure       paired mutations over the committed state evidence
# state  : boots the REAL daemon (synthetic rows, dry-run), stages applications through the console endpoints, runs
#          `helixqa autonomous` over qa/web-state/console_state_changing.yaml (taps and typed text, every checkpoint vision-gated) with a
#          window-scoped page video (-web-video-dir), then reads the daemon's database back. Verdict: scripts/qa-state-verify.py.
#          Evidence: evidence/JSK-T084-state (frames the verdicts cite, report, readback.json, video manifest with sha256).
# stepup : boots the daemon, runs scripts/qa-stepup-flow.cjs (CDP virtual authenticator, video, frames; takes about 6 minutes because
#          the passkey check must age past five minutes on the real clock), vision-verifies each frame with helixqa-verify-vision,
#          reads the database back. Evidence: evidence/JSK-T084-stepup.
# Video is NOT committed: it is written under the storage-policy drives ($JSK_QA_VIDEO_ROOT, default /mnt/DATA7TB/claude-work/qa-video/
# job_seeker) and only name, size and sha256 are recorded.
# Needs (env, by name): JSK_HELIXQA_BIN (HelixQA with -web-video-dir, built in place), E2E_NODE_MODULES, HELIX_VISION_ENDPOINT (loopback),
#   JSK_VERIFY_VISION_BIN (stepup only). Optional: HELIX_VISION_MODEL (default qwen2.5vl:3b), JSK_QA_VIDEO_ROOT.
# Exit: 0 verified, 1 a requirement failed, 2 cannot determine. 2 is never a pass.
set -u
HERE="$(cd "$(dirname "$0")" && pwd -P)"; ROOT="$(cd "$HERE/.." && pwd -P)"
VERIFY="$HERE/qa-state-verify.py"
# 11.4.287: SEV and UEV are rm -rf targets, so they are validated before anything runs. A value is accepted only when it is a non-empty,
# absolute, normalised path (no "..", ".", "//" or trailing slash) strictly inside $ROOT/evidence, named JSK-*, with no symlink in it
# (realpath -m must equal the path itself). Anything else (/, $HOME, an ancestor, evidence/ itself, empty, relative, symlink) is refused.
safe_evdir() { # NAME VALUE
  local n="$1" v="$2" ev="$ROOT/evidence"
  refuse() { echo "REFUSED: $n=\"$v\" is not a plain JSK-* directory strictly inside $ev (reason: $1); nothing was deleted" >&2; exit 2; }
  [ -n "$v" ] || refuse "empty"
  case "$v" in /*) ;; *) refuse "not absolute";; esac
  case "$v/" in */../*|*/./*|*//*) refuse "not normalised";; esac
  case "$v" in */) refuse "trailing slash";; esac
  [ ! -L "$ev" ] || refuse "evidence dir is a symlink"
  case "$v" in "$ev"/?*) ;; *) refuse "not strictly inside evidence";; esac
  case "${v##*/}" in JSK-?*) ;; *) refuse "name is not JSK-*";; esac
  [ "$(realpath -m -- "$v")" = "$v" ] || refuse "symlink or non-canonical path"
}
SEV="${SEV-$ROOT/evidence/JSK-T084-state}"; UEV="${UEV-$ROOT/evidence/JSK-T084-stepup}"
safe_evdir SEV "$SEV"; safe_evdir UEV "$UEV"

prove_failure() {
  [ -f "$SEV/readback.json" ] || { echo "cannot determine: no committed evidence at $SEV" >&2; exit 2; }
  W="$(mktemp -d)" || exit 2; trap 'rm -rf -- "${W:?}"' EXIT; fail=0
  mk() { rm -rf -- "${W:?}/c"; mkdir -p "$W/c/session-x/screenshots"; cp "$SEV/report/pipeline-report.json" "$W/c/session-x/"; cp "$SEV"/frames/*.png "$W/c/session-x/screenshots/"; cp "$SEV/helixqa.rc" "$SEV/readback.json" "$W/c/"; }
  expect() { python3 "$VERIFY" "$W/c" >"$W/o.txt" 2>&1; rc=$?; if [ "$rc" = "$2" ]; then echo "PASS $1 (rc $rc)"; else echo "FAIL $1: want rc $2, got $rc"; sed 's/^/   /' "$W/o.txt"; fail=1; fi; }
  jedit() { python3 - "$W/c/$1" "$2" <<'PY'
import json,sys
p,code=sys.argv[1:3]; d=json.load(open(p)); exec(code); json.dump(d,open(p,"w"))
PY
  }
  mk; expect "control: committed evidence verifies" 0
  mk; jedit readback.json 'd["applications"][0]["state"]="awaiting_human_approval"'; expect "M1 approve left no dry_run_recorded row" 1
  mk; jedit readback.json 'd["applications"][1]["state"]="awaiting_human_approval"'; expect "M2 reject left the draft awaiting approval" 1
  mk; jedit readback.json 'd["handoff_rows"]=1'; expect "M3 a handoff row means something was sent" 1
  mk; jedit readback.json 'd["audit_chain_ok"]=False'; expect "M4 audit chain broken" 1
  mk; jedit readback.json 'd["posting_states"]["7"]="scored"'; expect "M5 dismiss left the posting scored" 1
  mk; jedit readback.json 'd["audit_transitions"]=[t for t in d["audit_transitions"] if "rejected_by_human" not in t]'; expect "M6 no audit row names the reject" 1
  mk; jedit session-x/pipeline-report.json 'd["structured"]["cases"][2]["steps"][-1]["vision_status"]="refuted"'; expect "M7 the model refuted the approve frame" 1
  mk; echo 1 >"$W/c/helixqa.rc"; expect "M8 helixqa exited non-zero" 1
  mk; rm -f "$W/c/readback.json"; expect "M9 no read-back is undetermined, not a pass" 2
  [ "$fail" = 0 ] && echo "ALL MUTATIONS CAUGHT" && exit 0
  exit 1
}
[ "${1:-}" = "--prove-failure" ] && prove_failure
MODE="${1:-state}"
for v in JSK_HELIXQA_BIN E2E_NODE_MODULES HELIX_VISION_ENDPOINT; do [ -n "${!v:-}" ] || { echo "cannot determine: set $v" >&2; exit 2; }; done
[ -x "$JSK_HELIXQA_BIN" ] || { echo "cannot determine: $JSK_HELIXQA_BIN not executable" >&2; exit 2; }
export HELIX_VISION_MODEL="${HELIX_VISION_MODEL:-qwen2.5vl:3b}"
curl -fsS --max-time 3 "${HELIX_VISION_ENDPOINT%/v1/chat/completions}/api/version" >/dev/null || { echo "cannot determine: vision endpoint down (scripts/vision-ollama.sh start)" >&2; exit 2; }
RUN="$(date -u +%Y%m%dT%H%M%SZ)"
VROOT="${JSK_QA_VIDEO_ROOT:-/mnt/DATA7TB/claude-work/qa-video/job_seeker}"; VDIR="$VROOT/$MODE-$RUN"; mkdir -p "$VDIR"
OUT="$(mktemp -d "${TMPDIR:-/tmp}/jsk-state-qa.XXXXXX")" || exit 2; trap 'rm -rf -- "${OUT:?}"' EXIT

manifest() { # DIR-with-webm -> video-manifest.json (name, bytes, sha256, frames/duration); never the video itself
  python3 - "$1" "$2" <<'PY'
import glob,hashlib,json,os,subprocess,sys
d,out=sys.argv[1:3]; rows=[]
for f in sorted(glob.glob(os.path.join(d,"*.webm"))):
    h=hashlib.sha256(open(f,"rb").read()).hexdigest()
    pr=subprocess.run(["ffprobe","-v","error","-count_frames","-select_streams","v:0","-show_entries","stream=nb_read_frames,width,height:format=duration","-of","json",f],capture_output=True,text=True)
    j=json.loads(pr.stdout or "{}"); s=(j.get("streams") or [{}])[0]
    rows.append({"file":os.path.basename(f),"bytes":os.path.getsize(f),"sha256":h,"frames":s.get("nb_read_frames"),"width":s.get("width"),"height":s.get("height"),"duration_s":(j.get("format") or {}).get("duration"),"stored_under":"storage-policy drive, not committed"})
json.dump({"videos":rows},open(out,"w"),indent=2); open(out,"a").write("\n")
sys.exit(0 if rows and all(int(r["frames"] or 0)>=10 for r in rows) else 1)
PY
}

if [ "$MODE" = "state" ]; then
  ( cd "$ROOT/tests/e2e" && JSK_QA_STAGE=1 JSK_QA_OUT="$OUT" JSK_QA_BANKS="$ROOT/qa/web-state" JSK_QA_VIDEO_DIR="$VDIR" JSK_QA_EXTRA="${JSK_QA_EXTRA:--curiosity=false}" go test -run TestHelixQAAuthenticated -count=1 . ) || { echo "run could not be set up" >&2; exit 2; }
  safe_evdir SEV "$SEV"; rm -rf -- "${SEV:?}"; mkdir -p "$SEV/frames" "$SEV/report"
  python3 "$VERIFY" "$OUT" --table "$SEV/summary.tsv" | tee "$SEV/verify.txt"; vrc=${PIPESTATUS[0]}
  SESS="$(ls -d "$OUT"/session-* | sort | tail -1)"
  cp "$SESS/pipeline-report.json" "$SEV/report/"; cp "$OUT/helixqa.rc" "$OUT/readback.json" "$SEV/"; cp "$OUT/helixqa.log" "$SEV/helixqa.log"
  python3 - "$SESS" "$SEV" <<'PY'
import json,glob,hashlib,os,shutil,sys
sess,evid=sys.argv[1:3]
r=json.load(open(os.path.join(sess,"pipeline-report.json")))
cited={s["frame_sha256"] for c in r["structured"]["cases"] for s in c["steps"] if s.get("frame_sha256")}
for f in glob.glob(os.path.join(sess,"screenshots","*.png")):
    h=hashlib.sha256(open(f,"rb").read()).hexdigest()
    if h in cited: shutil.copy(f,os.path.join(evid,"frames",h[:12]+".png"))
PY
  python3 - "$SEV" <<'PY'
import sys,os
p=os.path.join(sys.argv[1],"summary.tsv"); L=open(p).read().splitlines(); out=[L[0]]
for l in L[1:]:
    c=l.split("\t"); c[3]=c[2][:12]+".png"; out.append("\t".join(c))
open(p,"w").write("\n".join(out)+"\n")
PY
  manifest "$VDIR" "$SEV/video-manifest.json" || { echo "FAIL: no finalized video with at least 10 frames under $VDIR" >&2; vrc=1; }
  printf 'vision model: %s (local, loopback only)\nhelixqa: %s\n' "$HELIX_VISION_MODEL" "$("$JSK_HELIXQA_BIN" version 2>&1 | head -1)" >"$SEV/MODEL.txt"
  bash "$HERE/evnorm.sh" -w "$SEV" || exit 2
  ( cd "$SEV" && find . -type f ! -name SHA256SUMS | sort | xargs sha256sum >SHA256SUMS )
  exit "$vrc"
fi

if [ "$MODE" = "stepup" ]; then
  [ -n "${JSK_VERIFY_VISION_BIN:-}" ] && [ -x "$JSK_VERIFY_VISION_BIN" ] || { echo "cannot determine: set JSK_VERIFY_VISION_BIN (helixqa-verify-vision)" >&2; exit 2; }
  ( cd "$ROOT/tests/e2e" && JSK_QA_MODE=stepup JSK_QA_OUT="$OUT" go test -run TestHelixQAAuthenticated -count=1 -timeout 20m . ) || { echo "run could not be set up" >&2; exit 2; }
  safe_evdir UEV "$UEV"; rm -rf -- "${UEV:?}"; mkdir -p "$UEV/frames" "$UEV/verdicts"; cp "$OUT"/frames/*.png "$UEV/frames/"; cp "$OUT/stepup.json" "$OUT/readback.json" "$UEV/"
  cp "$OUT/stepup-flow.webm" "$VDIR/"
  rc=0; : >"$UEV/summary.tsv"
  vv() { # id frame expect forbid
    "$JSK_VERIFY_VISION_BIN" -image "$UEV/frames/$2.png" -prompt "This is a screenshot of a web page. List the page title, the labelled values and any error message. Quote the visible text exactly." -expect "$3" ${4:+-forbid "$4"} \
      -endpoint "$HELIX_VISION_ENDPOINT" -model "$HELIX_VISION_MODEL" -max-tokens 400 -timeout 5m -out "$UEV/verdicts/$1.json" -challenge-id "$1" >"$UEV/verdicts/$1.log" 2>&1; r=$?
    printf '%s\t%s\t%s\n' "$1" "$2.png" "$r" >>"$UEV/summary.tsv"; echo "$1 rc=$r"; [ "$r" -eq 0 ] || rc=1; }
  vv STEPUP-01 01-settings-initial "Settings,Kill switch" ""
  vv STEPUP-02 02-kill-on "Kill switch,On" ""
  vv STEPUP-03 03-off-refused "Verify with your passkey first" ""
  vv STEPUP-04 04-stepup "Verify with passkey" ""
  vv STEPUP-05 05-kill-off "Kill switch,Off" "Verify with your passkey first"
  python3 - "$UEV" <<'PY' || rc=1
import json,sys,os
d=json.load(open(os.path.join(sys.argv[1],"stepup.json"))); rb=json.load(open(os.path.join(sys.argv[1],"readback.json")))
S={s["id"]:s for s in d["steps"]}; bad=[]
def need(c,m):
    if not c: bad.append(m)
need(S["02-kill-on"].get("http") in (200,303) or S["02-kill-on"].get("kill")=="On","kill switch did not turn on")
need(S["03-off-refused"].get("http")==403 and S["03-off-refused"].get("error_code")=="step_up_required","turn-off without a fresh passkey check was NOT refused with 403 step_up_required: %r"%S["03-off-refused"])
need(S["03-off-refused"].get("kill")=="On","kill switch changed despite the refusal")
need(S["04-stepup"].get("passkey_status")=="ok","passkey step-up ceremony did not report ok")
need(S["05-kill-off"].get("kill")=="Off" and S["05-kill-off"].get("live")=="Off","after step-up kill switch is not Off / live not Off")
need(S["waited"]["ms_since_signin"]>=300000,"the passkey check was not allowed to age past five minutes")
need(rb.get("audit_chain_ok") is True,"audit chain walk failed")
need(rb.get("handoff_rows")==0 and rb.get("live_mode_setting") in ("","0",None),"live/handoff state changed")
need(rb.get("kill_switch_setting") in ("","0",None),"database kill switch is not off at the end: %r"%rb.get("kill_switch_setting"))
L=["DOM and database checks: "+("PASS" if not bad else "FAIL")]+["FAIL: "+b for b in bad]
print("\n".join(L)); open(os.path.join(sys.argv[1],"checks.txt"),"w").write("\n".join(L)+"\n")
sys.exit(1 if bad else 0)
PY
  python3 - "$VDIR" "$UEV/video-manifest.json" <<'PY' || { echo "FAIL: no decodable video" >&2; rc=1; }
import glob,hashlib,json,os,subprocess,sys
d,out=sys.argv[1:3]; rows=[]
for f in sorted(glob.glob(os.path.join(d,"*.webm"))):
    pr=subprocess.run(["ffprobe","-v","error","-count_frames","-select_streams","v:0","-show_entries","stream=nb_read_frames","-of","default=nw=1:nk=1",f],capture_output=True,text=True)
    rows.append({"file":os.path.basename(f),"bytes":os.path.getsize(f),"sha256":hashlib.sha256(open(f,"rb").read()).hexdigest(),"frames":pr.stdout.strip(),"stored_under":"storage-policy drive, not committed"})
json.dump({"videos":rows},open(out,"w"),indent=2); open(out,"a").write("\n")
sys.exit(0 if rows and all(int(r["frames"] or 0)>=10 for r in rows) else 1)
PY
  bash "$HERE/evnorm.sh" -w "$UEV" || exit 2
  ( cd "$UEV" && find . -type f ! -name SHA256SUMS | sort | xargs sha256sum >SHA256SUMS )
  exit "$rc"
fi
echo "usage: $0 [state|stepup|--prove-failure]" >&2; exit 2
