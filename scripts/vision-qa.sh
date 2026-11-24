#!/usr/bin/env bash
# vision-qa.sh - HelixQA vision session against the REAL job_seeker console with a LOCAL vision model (JSK-T084/T111,
# umbrella 11.4.278). Docs: docs/operator/vision-qa.md.
#
# Legs, each a real frame -> real model -> machine-checked verdict (helixqa-verify-vision, strict fact matching):
#   live   : boots the real jobseekerd (private CA, host localhost), screenshots /login with Chromium, verifies it.
#   console: verifies the authenticated console frames already captured by the Playwright journey (evidence/JSK-T083).
#   mutant : the paired mutation. A server answering HTTP 200 to everything (blank "OK" page, and a stack-trace page)
#            is screenshotted and run against the SAME expectations; the vision check must FLAG it (-expect-fail
#            records case_result=true only when the raw verdict is a fail).
# Needs: ollama server up (scripts/vision-ollama.sh start), a helixqa-verify-vision binary (JSK_VERIFY_VISION_BIN, built
# from the resolved qa dependency ./cmd/helixqa-verify-vision, see docs/operator/helixqa.md), node + playwright
# (JSK_PLAYWRIGHT_NODE_MODULES), go, python3.
# Output: $OUT (default evidence/JSK-T084): frames/, verdicts/, summary.tsv, SHA256SUMS.
# Exit: 0 every leg as expected, 1 a verdict was not as expected, 2 cannot determine (missing tool/endpoint).
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"; ROOT="$(cd "$HERE/.." && pwd)"
OUT="${OUT:-$ROOT/evidence/JSK-T084}"
WORK="${VISION_QA_WORK:-/mnt/DATA1TB/claude-work/tmp/vision-qa}"
PORT="${VISION_OLLAMA_PORT:-11500}"
export HELIX_VISION_ENDPOINT="${HELIX_VISION_ENDPOINT:-http://127.0.0.1:$PORT/v1/chat/completions}"
export HELIX_VISION_MODEL="${HELIX_VISION_MODEL:-${VISION_MODEL:-qwen2.5vl:3b}}"
VV="${JSK_VERIFY_VISION_BIN:-}"
need() { command -v "$1" >/dev/null 2>&1 || { echo "cannot determine: $1 missing" >&2; exit 2; }; }
need go; need node; need python3; need curl
[ -n "$VV" ] && [ -x "$VV" ] || { echo "cannot determine: set JSK_VERIFY_VISION_BIN (helixqa-verify-vision)" >&2; exit 2; }
[ -n "${JSK_PLAYWRIGHT_NODE_MODULES:-}" ] || { echo "cannot determine: set JSK_PLAYWRIGHT_NODE_MODULES" >&2; exit 2; }
case "$HELIX_VISION_ENDPOINT" in http://127.0.0.1:*|http://localhost:*) ;; *) echo "refusing: endpoint must be local (no frame leaves the host): $HELIX_VISION_ENDPOINT" >&2; exit 2;; esac
curl -fsS --max-time 3 "http://127.0.0.1:$PORT/api/version" >/dev/null || { echo "cannot determine: local vision server down (scripts/vision-ollama.sh start)" >&2; exit 2; }
mkdir -p "$OUT/frames" "$OUT/verdicts" "$WORK/bin"; : >"$OUT/summary.tsv"
PIDS=()
cleanup() { for p in "${PIDS[@]:-}"; do [ -n "$p" ] && [ "$p" -gt 1 ] 2>/dev/null && kill "$p" 2>/dev/null; done; }
trap cleanup EXIT
rc=0
# run_case ID IMAGE EXPECT FORBID PROMPT [expect-fail]
run_case() {
  local id="$1" img="$2" exp="$3" forb="$4" prompt="$5" ef="${6:-}" args=()
  [ -n "$ef" ] && args+=(-expect-fail)
  [ -n "$forb" ] && args+=(-forbid "$forb")
  "$VV" -image "$img" -prompt "$prompt" -expect "$exp" "${args[@]}" -endpoint "$HELIX_VISION_ENDPOINT" \
     -model "$HELIX_VISION_MODEL" -max-tokens 400 -timeout 5m -out "$OUT/verdicts/$id.json" -challenge-id "$id" >"$OUT/verdicts/$id.log" 2>&1
  local r=$?
  printf '%s\t%s\t%s\t%s\n' "$id" "$([ -n "$ef" ] && echo mutation-must-be-flagged || echo must-pass)" "$r" "$(basename "$img")" >>"$OUT/summary.tsv"
  echo "$id rc=$r (${ef:+expect-fail})"; [ "$r" -eq 0 ] || rc=1
}
P_DESC="This is a screenshot of a web page. List the page title or main heading, the navigation links, and any buttons or form fields you can read. Quote the visible text exactly."

# ---- live leg: the real daemon
BIN="$WORK/bin"
# GOTOOLCHAIN is left at the host default (auto): go.mod may require a newer toolchain than the system go.
(cd "$ROOT" && CGO_ENABLED=0 go build -trimpath -o "$BIN/jobseekerd" ./cmd/jobseekerd && CGO_ENABLED=0 go build -trimpath -o "$BIN/jsctl" ./cmd/jsctl) || { echo "build failed" >&2; exit 2; }
free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'; }
D="$(mktemp -d "$WORK/run.XXXXXX")"; HP="$(free_port)"; MP="$(free_port)"; rh() { head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'; }
printf 'remotive.com\n' >"$D/allow.txt"
export JOBSEEKER_ENV=test JOBSEEKER_HTTPS_ADDR="127.0.0.1:$HP" JOBSEEKER_METRICS_ADDR="127.0.0.1:$MP" JOBSEEKER_DATA_DIR="$D/data" \
  JOBSEEKER_CERT_DIR="$D/certs" JOBSEEKER_DB_KEY="$(rh 24)" JOBSEEKER_SESSION_KEY="$(rh 24)" JOBSEEKER_BOOTSTRAP_TOKEN="$(rh 24)" \
  JOBSEEKER_ALLOWED_HOSTS=localhost JOBSEEKER_ORIGINS="https://localhost:$HP" JOBSEEKER_EGRESS_ALLOWLIST="$D/allow.txt" JEV_GATE=shadow
"$BIN/jsctl" cert gen-ca --hosts localhost >/dev/null && "$BIN/jsctl" cert issue --hosts localhost >/dev/null || { echo "cert provisioning failed" >&2; exit 2; }
"$BIN/jobseekerd" >"$D/jobseekerd.log" 2>&1 & PIDS+=($!)
for _ in $(seq 1 60); do grep -q 'jobseekerd: serving' "$D/jobseekerd.log" && break; sleep 0.5; done
grep -q 'jobseekerd: serving' "$D/jobseekerd.log" || { echo "daemon did not start" >&2; exit 2; }
node "$HERE/vision-capture.cjs" "https://localhost:$HP/login" "$OUT/frames/live-login.png" >"$OUT/frames/live-login.capture.json" || exit 2
LOGIN_EXP="Sign in,passkey"
run_case VQA-LIVE-LOGIN "$OUT/frames/live-login.png" "$LOGIN_EXP" "" "$P_DESC"

# ---- console leg: frames from the real authenticated journey
for n in inbox draft settings; do cp "$ROOT/evidence/JSK-T083/zoom200-$n.png" "$OUT/frames/console-$n.png"; done
run_case VQA-CONSOLE-INBOX "$OUT/frames/console-inbox.png" "Inbox,Review queue,Applications,Settings" "" "$P_DESC"
run_case VQA-CONSOLE-DRAFT "$OUT/frames/console-draft.png" "Draft review,Template files" "" "$P_DESC"
run_case VQA-CONSOLE-SETTINGS "$OUT/frames/console-settings.png" "Settings" "" "$P_DESC"

# ---- paired mutation: a server answering 200 to everything, same expectations as the login frame
for m in ok error; do
  MPORT="$(free_port)"; python3 "$HERE/vision-mutant-server.py" "$MPORT" "$m" & PIDS+=($!); sleep 1
  node "$HERE/vision-capture.cjs" "http://127.0.0.1:$MPORT/login" "$OUT/frames/mutant-$m.png" >"$OUT/frames/mutant-$m.capture.json" || exit 2
  run_case "VQA-MUTANT-$m" "$OUT/frames/mutant-$m.png" "$LOGIN_EXP" "" "$P_DESC" expect-fail
done
# keep the evidence free of host-specific absolute paths
find "$OUT" -type f \( -name '*.json' -o -name '*.log' \) -exec sed -i "s#$OUT#evidence/JSK-T084#g" {} +
(cd "$OUT" && find . -type f ! -name SHA256SUMS | sort | xargs sha256sum >SHA256SUMS)
echo "summary:"; cat "$OUT/summary.tsv"; exit $rc
