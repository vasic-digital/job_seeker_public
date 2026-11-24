#!/usr/bin/env bash
# vision-ollama.sh - start/stop/status/pull for the USER-LEVEL local vision-model server (JSK-010, umbrella 11.4.278).
#
# A SEPARATE ollama instance, owned by the current user, on its own port and its own model directory on the DATA1TB
# drive. It never touches the system ollama (user `ollama`, port 11434, /usr/share/ollama). No sudo, no systemd.
# Nothing leaves the host: the server binds 127.0.0.1 only and frames are only ever sent to it.
#
# Usage: scripts/vision-ollama.sh {start|stop|status|pull [model]|env|models}
# Overrides (env, by name): VISION_OLLAMA_BIN, VISION_OLLAMA_PORT (11500), VISION_OLLAMA_HOME, VISION_OLLAMA_MODELS,
#   VISION_MODEL (qwen2.5vl:3b).
# Exit: 0 ok / running, 1 not running or a failed action, 2 cannot determine (missing binary, refused signal).
set -u
BIN="${VISION_OLLAMA_BIN:-$(command -v ollama || true)}"
PORT="${VISION_OLLAMA_PORT:-11500}"
HOME_DIR="${VISION_OLLAMA_HOME:-/mnt/DATA1TB/claude-work/ollama-user}"
MODELS="${VISION_OLLAMA_MODELS:-/mnt/DATA1TB/claude-work/ollama-models}"
MODEL="${VISION_MODEL:-qwen2.5vl:3b}"
PIDF="$HOME_DIR/ollama.pid"
export OLLAMA_HOST="127.0.0.1:$PORT" OLLAMA_MODELS="$MODELS"

[ -n "$BIN" ] && [ -x "$BIN" ] || { echo "cannot determine: no ollama binary (set VISION_OLLAMA_BIN)" >&2; exit 2; }
[ "$PORT" != 11434 ] || { echo "refusing: port 11434 is the system ollama" >&2; exit 2; }
mkdir -p "$HOME_DIR" "$MODELS"

# is_ours PID: the process exists, is owned by this user, is an `ollama serve`, and listens on OUR port (11.4.174).
is_ours() {
  local pid="$1"
  [ -n "$pid" ] && [ "$pid" -gt 1 ] 2>/dev/null || return 1
  [ "$(ps -o user= -p "$pid" 2>/dev/null | tr -d ' ')" = "$(id -un)" ] || return 1
  ps -o args= -p "$pid" 2>/dev/null | grep -q 'ollama serve' || return 1
  tr '\0' '\n' <"/proc/$pid/environ" 2>/dev/null | grep -qx "OLLAMA_HOST=127.0.0.1:$PORT"
}
up() { curl -fsS --max-time 3 "http://127.0.0.1:$PORT/api/version" >/dev/null 2>&1; }

case "${1:-status}" in
  start)
    if up; then echo "already up on 127.0.0.1:$PORT"; exit 0; fi
    nohup "$BIN" serve >"$HOME_DIR/serve.log" 2>&1 &
    echo $! >"$PIDF"
    for _ in $(seq 1 30); do up && break; sleep 1; done
    up && { echo "up pid=$(cat "$PIDF") 127.0.0.1:$PORT models=$MODELS"; exit 0; }
    echo "did not come up; see $HOME_DIR/serve.log" >&2; exit 1 ;;
  stop)
    pid="$(cat "$PIDF" 2>/dev/null || true)"
    if ! is_ours "$pid"; then echo "no verified user-owned server for port $PORT (pid file: '${pid:-none}'); not signalling anything" >&2; up && exit 2; exit 1; fi
    kill "$pid"; for _ in $(seq 1 15); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
    kill -0 "$pid" 2>/dev/null && { echo "pid $pid still alive after SIGTERM" >&2; exit 1; }
    rm -f "$PIDF"; echo "stopped pid $pid"; exit 0 ;;
  status)
    up && { echo "up 127.0.0.1:$PORT version=$(curl -fsS "http://127.0.0.1:$PORT/api/version")"; "$BIN" list; exit 0; }
    echo "down"; exit 1 ;;
  pull) up || { echo "start the server first" >&2; exit 1; }; exec "$BIN" pull "${2:-$MODEL}" ;;
  models) exec "$BIN" list ;;
  env) printf 'HELIX_VISION_ENDPOINT=http://127.0.0.1:%s/v1/chat/completions\nHELIX_VISION_MODEL=%s\n' "$PORT" "$MODEL" ;;
  *) echo "usage: $0 {start|stop|status|pull [model]|models|env}" >&2; exit 2 ;;
esac
