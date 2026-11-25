#!/usr/bin/env bash
# i18n_review.sh - JSK-T143..T156: independent per-language review (§11.4.256) via the canonical umbrella reviewer
# (_tools/review_translation.py). The reviewer provider MUST differ from every translator provider recorded in
# evidence/i18n/<lang>/pipeline.json. Writes evidence/i18n/<lang>/verdict.json (verdict PASS|FAIL|ERROR, translator,
# reviewer, scores). Exit 0 PASS, 1 FAIL, 2 ERROR/could not determine. Keys come from ~/.helixtranslate.env by NAME, never printed.
# Output under evidence/i18n/<lang>/ is always normalised via scripts/evnorm.sh before exit.
# Usage: UMBRELLA=<umbrella> i18n_review.sh <lang> [reviewer_provider (default: lang_reviewer plan, else cohere)] [model]
set -u
lang="${1:?lang}"; rp="${2:-}"; rm_="${3:-}"
root="$(cd "$(dirname "$0")/.." && pwd)"; U="${UMBRELLA:-$root/..}"
# default reviewer = the per-language plan in scripts/i18n_providers.json (lang_reviewer), else cohere (the legacy default)
if [ -z "$rp" ]; then
  rp="$(python3 -c "import json;print(json.load(open('$root/scripts/i18n_providers.json')).get('lang_reviewer',{}).get('$lang',{}).get('provider','cohere'))")"
  [ -z "$rm_" ] && rm_="$(python3 -c "import json;print(json.load(open('$root/scripts/i18n_providers.json')).get('lang_reviewer',{}).get('$lang',{}).get('model',''))")"
fi
[ "$rp" = mistral ] && export REVIEW_SLICE_PAUSE="${REVIEW_SLICE_PAUSE:-40}"   # mistral free tier: 2 requests/minute
R="$U/_tools/review_translation.py"; [ -f "$R" ] || { echo "UNDETERMINED: reviewer missing"; exit 2; }
ev="$root/evidence/i18n/$lang"; [ -f "$ev/pipeline.json" ] || { echo "UNDETERMINED: no pipeline evidence"; exit 2; }
set -a; . "${HELIX_ENVFILE:-$HOME/.helixtranslate.env}"; set +a
tr="$(python3 -c "import json,sys;print(','.join(json.load(open('$ev/pipeline.json'))['providers']))")"
case ",$tr," in *",$rp:"*|*",$rp,"*) echo "FAIL: reviewer provider $rp equals a translator provider ($tr)"; exit 2;; esac
# a DELTA review (REVIEW_KEYS set) writes review.delta.json / verdict.delta.json and NEVER touches the full-review verdict.json that the audit gates on
sfx=""; [ -n "${REVIEW_KEYS:-}" ] && sfx=".delta"
rm -f "$ev/review$sfx.json" "$ev/verdict$sfx.json"   # never read a stale result
# sliced review (free-tier reviewers cap one request far below the whole catalogue); REVIEW_SLICE / REVIEW_SLICE_PAUSE tune it
python3 "$root/scripts/i18n_review_chunked.py" --reviewer "$R" --src "$root/internal/i18n/messages/en.json" --translated "$root/internal/i18n/messages/$lang.json" \
  --lang "$lang" --provider "$rp" ${rm_:+--model "$rm_"} --out "$ev/review$sfx.json" >/dev/null 2>"$ev/review$sfx.stderr"; rc=$?
python3 - "$ev" "$tr" "$rc" "$sfx" <<'P'
import json,sys,os
ev,tr,rc,sfx=sys.argv[1],sys.argv[2],int(sys.argv[3]),sys.argv[4]
r=json.load(open(ev+"/review"+sfx+".json")) if os.path.exists(ev+"/review"+sfx+".json") else {"verdict":"ERROR"}
v={"verdict":("PASS" if rc==0 else "FAIL" if rc==1 else "ERROR") if r.get("verdict")==("PASS" if rc==0 else "FAIL" if rc==1 else "ERROR") else "ERROR","translator":tr,"reviewer":"%s:%s"%(r.get("_provider","?"),r.get("_model","?")),
   "scores":{k:r.get(k) for k in("accuracy","fluency","completeness","naturalness","register_appropriate")},
   "issues":r.get("issues",[]),"scope":r.get("scope","full"),"script_ok":r.get("script_ok"),"target_script_ok":r.get("target_script_ok"),"review_rc":rc}
json.dump(v,open(ev+"/verdict"+sfx+".json","w"),ensure_ascii=False,indent=1); print(json.dumps(v,ensure_ascii=False))
P
bash "$root/scripts/evnorm.sh" -w "$ev" >/dev/null || { echo "UNDETERMINED: evnorm failed on $ev"; exit 2; }   # evidence must land normalised (gates/verify_i18n_review_normalised.sh)
exit "$rc"
