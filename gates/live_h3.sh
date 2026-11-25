#!/usr/bin/env bash
# live_h3.sh — JSK-T032 / FR-040: live HTTP/3 + Brotli gate against the REAL server stack running in a REAL
# container, driven only through tools/containers (the Containers Submodule). Not part of `make gates`
# (needs a container runtime); run with `make live-h3`. Evidence is written to evidence/JSK-T032/.
#   1. full stack (TCP+UDP published): a real HTTP/3 client must see proto HTTP/3.0 and Content-Encoding: br
#      with a body that Brotli-decodes to the real /healthz JSON.
#   2. TCP-only stack (UDP really not published): the HTTP/3 attempt must FAIL and the same URL over TCP
#      must negotiate HTTP/2.0 with Content-Encoding: br. The same stack must also make `check -expect h3`
#      fail (a h3 assertion that passes without QUIC would be a bluff).
# Exit: 0 all hold, 1 an assertion failed, 2 could not determine (no runtime, binaries missing): never a pass.
# --prove-failure: host-only paired mutation: the probe must FAIL against a server with Brotli removed
#   (-mutate no-brotli) and PASS against the unmodified server (control). No container needed.
# --runner=NAME (also --runner NAME; env CONTAINERS_RUNNER) FORCES the container runner for every jsstack call
#   (podman|docker|nerdctl|...|auto). Unknown or not-installed => rc 2, never a fallback (docs/RUNNER.md).
set -u
root="$(cd "$(dirname "$0")/.." && pwd)"; cd "$root"
bin="${JS_BIN_DIR:-$root/bin}"
args=(); while [ $# -gt 0 ]; do
  case "$1" in
    --runner=*) [ -n "${1#--runner=}" ] || { echo "UNDETERMINED: --runner= has an empty value"; exit 2; }; export CONTAINERS_RUNNER="${1#--runner=}"; shift ;;
    --runner) [ $# -ge 2 ] && [ "${2#-}" = "$2" ] || { echo "UNDETERMINED: --runner needs a value"; exit 2; }; export CONTAINERS_RUNNER="$2"; shift 2 ;;
    --help|-h) sed -n '2,/^set -u/p' "$0" | sed '$d'; exit 0 ;;
    *) args+=("$1"); shift ;;
  esac
done
set -- "${args[@]+"${args[@]}"}"
for b in jsstack jsgate-probe jsgate-serve; do
  [ -x "$bin/$b" ] || { echo "UNDETERMINED: $bin/$b missing (run: make build-gate)"; exit 2; }
done
tmp="$(mktemp -d)"; chmod 755 "$tmp"; pids=""
stacks_up=0
cleanup() {
  for p in $pids; do kill "$p" 2>/dev/null; done
  if [ "$stacks_up" -eq 1 ]; then # never leave a stack running, whatever aborted the gate
    for f in deploy/compose.yml deploy/compose.tcp-only.yml; do "$bin/jsstack" down -file "$f" -project js-live-h3 >/dev/null 2>&1; done
  fi
  if [ -n "${out:-}" ] && [ -f "$out" ]; then bash "$root/scripts/evnorm.sh" -w "$out" || echo "WARNING: evidence normaliser failed; run: bash scripts/evnorm.sh -w $out" >&2; fi   # shared evidence normaliser
  rm -rf "$tmp"
}
trap cleanup EXIT
trap 'exit 2' INT TERM
"$bin/jsgate-probe" gencert -dir "$tmp/certs" >/dev/null || { echo "UNDETERMINED: gencert failed"; exit 2; }

if [ "${1:-}" = "--prove-failure" ]; then
  port="$("$bin/jsstack" port -udp js-live-h3-proof 18500)" || { echo "UNDETERMINED: no port"; exit 2; }
  run_host() { # $1 mutate -> prints probe rc
    "$bin/jsgate-serve" -addr "127.0.0.1:$port" -cert "$tmp/certs/tls.crt" -key "$tmp/certs/tls.key" -mutate "$1" >"$tmp/serve.$1" 2>&1 &
    local sp=$!; pids="$pids $sp"; sleep 1
    "$bin/jsgate-probe" check -url "https://127.0.0.1:$port/healthz" -ca "$tmp/certs/ca.pem" -expect h3 >"$tmp/probe.$1" 2>&1; local rc=$?
    kill "$sp" 2>/dev/null; wait "$sp" 2>/dev/null; sleep 1; echo "$rc"
  }
  c="$(run_host none)"; m="$(run_host no-brotli)"
  if [ "$c" -eq 0 ] && [ "$m" -eq 1 ]; then echo "PROVE-FAILURE OK: control rc=0, no-brotli mutation rc=1"; exit 0; fi
  echo "PROVE-FAILURE BAD: control=$c no-brotli=$m (want 0,1)"; cat "$tmp"/probe.*; exit 1
fi

"$bin/jsstack" probe || { echo "UNDETERMINED: no runtime/compose (or the forced runner is refused)"; exit 2; }
ev="$root/evidence/JSK-T032"; mkdir -p "$ev"; out="$ev/live-h3.txt"
{ echo "# live HTTP/3 + Brotli gate, $(date -u +%Y-%m-%dT%H:%M:%SZ), commit $(git rev-parse --short=12 HEAD)"; "$bin/jsstack" probe; } >"$out"
port="$("$bin/jsstack" port -udp job-seeker-live-h3 18443)" || { echo "UNDETERMINED: no port"; exit 2; }
export JS_PORT="$port" JS_CERT_DIR="$tmp/certs"
url="https://127.0.0.1:$port/healthz"; ca="$tmp/certs/ca.pem"
rc=0
stacks_up=1
stage() { echo "== $*" | tee -a "$out"; }
run() { "$@" 2>&1 | tee -a "$out"; return "${PIPESTATUS[0]}"; }

stage "1. full stack, TCP+UDP published, port $port"
run "$bin/jsstack" up -file deploy/compose.yml -project js-live-h3 -container jobseeker-gate -build -wait-port "$port"; r=$?
[ "$r" -eq 0 ] || { echo "stack failed to start rc=$r" | tee -a "$out"; "$bin/jsstack" down -file deploy/compose.yml -project js-live-h3 >/dev/null 2>&1; exit "$r"; }
sleep 2
run "$bin/jsstack" status -container jobseeker-gate || rc=1
run "$bin/jsgate-probe" check -url "$url" -ca "$ca" -expect h3 || rc=1
run "$bin/jsstack" logs -container jobseeker-gate >/dev/null
run "$bin/jsstack" down -file deploy/compose.yml -project js-live-h3 || rc=1

stage "2. TCP-only stack (UDP not published)"
run "$bin/jsstack" up -file deploy/compose.tcp-only.yml -project js-live-h3 -container jobseeker-gate -build -wait-port "$port"; r=$?
[ "$r" -eq 0 ] || { echo "tcp-only stack failed to start rc=$r" | tee -a "$out"; "$bin/jsstack" down -file deploy/compose.tcp-only.yml -project js-live-h3 >/dev/null 2>&1; exit "$r"; }
sleep 2
run "$bin/jsgate-probe" check -url "$url" -ca "$ca" -expect h2-fallback || rc=1
stage "2b. mutation control: the h3 assertion MUST fail on the TCP-only stack"
"$bin/jsgate-probe" check -url "$url" -ca "$ca" -expect h3 >>"$out" 2>&1; mrc=$?
if [ "$mrc" -eq 1 ]; then echo "PASS: h3 assertion fails without QUIC (rc=1)" | tee -a "$out"; else echo "FAIL: h3 assertion rc=$mrc on a TCP-only stack" | tee -a "$out"; rc=1; fi
run "$bin/jsstack" down -file deploy/compose.tcp-only.yml -project js-live-h3 || rc=1

[ "$rc" -eq 0 ] && echo "LIVE-H3 OK" | tee -a "$out" || echo "LIVE-H3 FAIL" | tee -a "$out"
exit "$rc"
