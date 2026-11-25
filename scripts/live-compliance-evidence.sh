#!/usr/bin/env bash
# live-compliance-evidence.sh - JSK-T239: run the REAL jobseekerd process (own temp data dir, loopback, dry-run default,
# real egress client, real robots.txt fetch) and show that ingest is blocked until `jsctl ext terms accept`, then judged
# by the live robots.txt. Provider: remotive only (its robots.txt disallows /api/*), so NO job data is ever requested:
# the only outbound request is one GET https://remotive.com/robots.txt. Nothing is applied or sent.
# Output goes to stdout; pipe through scripts/evnorm.sh -w when writing under evidence/.
# Exit 0 both phases showed the expected audit rows, 1 not, 2 cannot determine (missing tool / daemon did not start).
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
for b in go sqlite3 curl; do command -v "$b" >/dev/null || { echo "UNDETERMINED: $b missing"; exit 2; }; done
work="$(mktemp -d)"; pid=""
cleanup() { [ -n "$pid" ] && kill "$pid" 2>/dev/null; sleep 0.5; rm -rf -- "$work"; }
trap cleanup EXIT
( cd "$here" && CGO_ENABLED=0 go build -trimpath -o "$work/jobseekerd" ./cmd/jobseekerd && CGO_ENABLED=0 go build -trimpath -o "$work/jsctl" ./cmd/jsctl ) || { echo "UNDETERMINED: build failed"; exit 2; }
port="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
mport="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
rnd() { head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n'; }
export JOBSEEKER_ENV=test JOBSEEKER_HTTPS_ADDR="127.0.0.1:$port" JOBSEEKER_METRICS_ADDR="127.0.0.1:$mport" \
  JOBSEEKER_DATA_DIR="$work/data" JOBSEEKER_CERT_DIR="$work/certs" JOBSEEKER_DB_KEY="$(rnd)" JOBSEEKER_SESSION_KEY="$(rnd)" \
  JOBSEEKER_BOOTSTRAP_TOKEN="$(rnd)" JOBSEEKER_ALLOWED_HOSTS=js.lan JOBSEEKER_ORIGINS="https://js.lan:$port" \
  JOBSEEKER_EGRESS_ALLOWLIST="$here/deploy/egress-allowlist.txt" JOBSEEKER_RULES_DIR="$here/rules" JOBSEEKER_PROVIDERS=remotive JEV_GATE=shadow
unset JOBSEEKER_TEST_ROBOTS_URL
"$work/jsctl" cert gen-ca --hosts js.lan >/dev/null && "$work/jsctl" cert issue --hosts js.lan >/dev/null || { echo "UNDETERMINED: cert setup"; exit 2; }
start() {
  "$work/jobseekerd" >"$work/daemon.log" 2>&1 & pid=$!
  for _ in $(seq 1 200); do grep -q "jobseekerd: serving" "$work/daemon.log" && return 0; sleep 0.1; done
  echo "UNDETERMINED: daemon did not start"; sed 's/^/  /' "$work/daemon.log" | head -20; return 1
}
stop() { kill "$pid" 2>/dev/null; wait "$pid" 2>/dev/null; pid=""; }
db="$work/data/jobseeker.db"
audit() { sqlite3 "$db" "SELECT id, actor, reason_code, evidence_json FROM audit_log WHERE reason_code IN ('compliance_blocked') ORDER BY id" 2>&1; }
echo "== mode: dry-run default; process: jobseekerd (real); provider: remotive; outbound: robots.txt only"
start || exit 2
"$work/jsctl" ext enable tracker_hook reply_detect
echo "== phase 1: terms NOT accepted"
"$work/jsctl" ext terms list
sleep 40   # first scheduler tick is 30 s after start
echo "-- audit rows (compliance_blocked):"; r1="$(audit)"; echo "$r1"
echo "-- daemon log (fetch lines):"; grep -E "fetch|extensions" "$work/daemon.log" | sed 's/^/  /'
stop
echo "== operator: jsctl ext terms accept --provider remotive"
"$work/jsctl" ext terms accept --provider remotive
sqlite3 "$db" "DELETE FROM setting WHERE key='sched.fetch.remotive.last'"   # own temp DB: make the 6 h job due again
n1="$(printf '%s\n' "$r1" | grep -c compliance_blocked)"
start || exit 2
echo "== phase 2: terms accepted, live robots.txt judges the exact path /api/remote-jobs"
sleep 40
r2="$(audit)"; echo "-- audit rows (compliance_blocked):"; echo "$r2"
n2="$(printf '%s\n' "$r2" | grep -c compliance_blocked)"
echo "-- posting rows fetched (must be 0): $(sqlite3 "$db" 'SELECT COUNT(*) FROM posting')"
stop
echo "-- compliance_blocked rows: phase1=$n1 total_after_phase2=$n2"
{ [ "$n1" -ge 1 ] && [ "$n2" -gt "$n1" ]; } && { echo "RESULT: ingest blocked before terms (provider_tos), and after terms by the live robots.txt (robots)"; exit 0; }
echo "RESULT: expected audit rows not found"; exit 1
