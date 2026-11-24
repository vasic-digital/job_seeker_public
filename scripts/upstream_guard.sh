#!/usr/bin/env bash
# upstream_guard.sh - safety guard for publishing job_seeker to additional hosts.
#
# Usage:
#   upstream_guard.sh check  <remote-url> [--root DIR] [--visibility V]
#   upstream_guard.sh enable <name>       [--root DIR] [--visibility V]   (name = file stem in upstreams/)
#   upstream_guard.sh --prove-failure
#
# Rules (exit 0 allow, 1 refuse, 2 COULD NOT DETERMINE = refuse; 2 is never a pass):
#   G1  every host other than the GitHub development remote needs the public-readiness
#       marker docs/public-readiness/AUDIT-GO (non-empty) - without it: refuse.
#   G2  git.m3w.org (m3w) is PRIVATE-only: the repository must be verified private
#       (API probe) - public, absent or undeterminable: refuse.
#   G3  `enable` renames upstreams/NAME.sh.disabled -> NAME.sh only when G1/G2 pass and
#       the target repository EXISTS (visibility != absent).
# --visibility V (private|public|absent|unknown) is the hermetic test seam: it replaces the
# network probe. Probes read credentials from env var NAMES only and never print them:
#   codeberg CODEBERG_GITEA_TOKEN, m3w M3W_GITEA_TOKEN, gitlab glab auth / GITLAB_TOKEN,
#   gitflic GITFLIC_TOKEN, gitverse GITVERSE_TOKEN. GitHub uses existing `gh` auth.
set -u
SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MARKER="docs/public-readiness/AUDIT-GO"

url_host() { local u="$1"; u="${u#*://}"; u="${u#*@}"; u="${u%%[:/]*}"; u="$(printf '%s' "$u" | tr 'A-Z' 'a-z')"; printf '%s' "${u%.}"; }
url_path() { local u="$1"; u="${u#*://}"; u="${u#*@}"; u="${u#*[:/]}"; u="${u%.git}"; printf '%s' "${u#/}"; }

probe() { # host path -> private|public|absent|unknown
  local host="$1" path="$2" code body
  case "$host" in
    codeberg.org) tok="${CODEBERG_GITEA_TOKEN:-}";;
    git.m3w.org)  tok="${M3W_GITEA_TOKEN:-}";;
    *) tok="";;
  esac
  case "$host" in
    codeberg.org|git.m3w.org)
      [ -n "$tok" ] || { echo unknown; return; }
      body="$(curl -s -m 20 -w '\n%{http_code}' -H "Authorization: token $tok" "https://$host/api/v1/repos/$path" 2>/dev/null)" || { echo unknown; return; }
      code="${body##*$'\n'}"; body="${body%$'\n'*}"
      [ "$code" = 404 ] && { echo absent; return; }
      [ "$code" = 200 ] || { echo unknown; return; }
      echo "$body" | python3 -c 'import json,sys
try: d=json.load(sys.stdin); print("private" if d.get("private") is True else "public" if d.get("private") is False else "unknown")
except Exception: print("unknown")' 2>/dev/null || echo unknown;;
    gitlab.com)
      command -v glab >/dev/null || { echo unknown; return; }
      body="$(glab api "projects/${path//\//%2F}" 2>/dev/null)" || { echo absent; return; }
      case "$body" in *'"visibility":"public"'*) echo public;; *'"visibility":"'*) echo private;; *) echo unknown;; esac;;
    *) echo unknown;;   # gitflic.ru / gitverse.ru: no read-only adapter registered -> undeterminable
  esac
}

cmd_check() {
  local url="$1" root="$ROOT" vis="" host path
  shift
  while [ $# -gt 0 ]; do case "$1" in --root) root="$2"; shift 2;; --visibility) vis="$2"; shift 2;; *) echo "unknown arg $1" >&2; return 2;; esac; done
  case "$url" in /*|./*|../*|file://*) return 0;; esac   # local filesystem remote: not a publication host
  host="$(url_host "$url")"; path="$(url_path "$url")"
  [ -n "$host" ] || { echo "upstream_guard: COULD NOT DETERMINE host of '$url'" >&2; return 2; }
  if [ "$host" = github.com ]; then
    return 0   # existing private development remote (PR delivery); public flip is a separate operator step
  fi
  if [ ! -s "$root/$MARKER" ]; then
    echo "upstream_guard: REFUSED push to $host: public-readiness marker $MARKER missing or empty" >&2; return 1
  fi
  if [ "$host" = git.m3w.org ]; then
    [ -n "$vis" ] || vis="$(probe "$host" "$path")"
    case "$vis" in
      private) ;;
      public) echo "upstream_guard: REFUSED: m3w repository $path is PUBLIC; m3w is private-only" >&2; return 1;;
      *) echo "upstream_guard: REFUSED (rc 2): m3w repository $path visibility is '$vis', not verified private" >&2; return 2;;
    esac
  fi
  return 0
}

cmd_enable() {
  local name="$1" root="$ROOT" vis="" url host path
  shift
  while [ $# -gt 0 ]; do case "$1" in --root) root="$2"; shift 2;; --visibility) vis="$2"; shift 2;; *) echo "unknown arg $1" >&2; return 2;; esac; done
  local f="$root/upstreams/$name.sh.disabled"
  [ -f "$f" ] || { echo "upstream_guard: no disabled recipe $f" >&2; return 2; }
  url="$(sed -n 's/^export UPSTREAMABLE_REPOSITORY="\(.*\)"/\1/p' "$f" | head -1)"
  [ -n "$url" ] || { echo "upstream_guard: recipe has no UPSTREAMABLE_REPOSITORY" >&2; return 2; }
  host="$(url_host "$url")"; path="$(url_path "$url")"
  [ -n "$vis" ] || vis="$(probe "$host" "$path")"
  cmd_check "$url" --root "$root" --visibility "$vis" || return $?
  case "$vis" in
    absent) echo "upstream_guard: REFUSED: target repository $host/$path does not exist yet" >&2; return 1;;
    unknown) echo "upstream_guard: COULD NOT DETERMINE existence of $host/$path" >&2; return 2;;
  esac
  mv "$f" "$root/upstreams/$name.sh" && echo "enabled upstreams/$name.sh -> $url"
}

suite() { # $1 = script under test; prints PASS/FAIL lines; rc 0 iff all pass
  local S="$1" t pass=0 fail=0 rc
  t="$(mktemp -d)"; mkdir -p "$t/ok/docs/public-readiness" "$t/ok/upstreams" "$t/bad/upstreams"
  echo GO > "$t/ok/docs/public-readiness/AUDIT-GO"
  for r in ok bad; do
    printf 'export UPSTREAMABLE_REPOSITORY="git@git.m3w.org:vasic-digital/job_seeker.git"\n' > "$t/$r/upstreams/m3w.sh.disabled"
    printf 'export UPSTREAMABLE_REPOSITORY="git@codeberg.org:vasic-digital/job_seeker.git"\n' > "$t/$r/upstreams/codeberg.sh.disabled"
  done
  exp() { # label expected-rc actual-rc
    if [ "$2" = "$3" ]; then pass=$((pass+1)); echo "PASS $1 (rc $3)"; else fail=$((fail+1)); echo "FAIL $1 expected rc $2 got $3"; fi; }
  local M=git@git.m3w.org:vasic-digital/job_seeker.git C=git@codeberg.org:vasic-digital/job_seeker.git
  "$S" check "$C" --root "$t/ok" --visibility public;  exp "control: codeberg public with marker allowed" 0 $?
  "$S" check "$M" --root "$t/ok" --visibility private; exp "control: m3w private with marker allowed" 0 $?
  "$S" check git@github.com:vasic-digital/job_seeker.git --root "$t/bad"; exp "control: github dev remote allowed" 0 $?
  "$S" check /srv/local/repo.git --root "$t/bad"; exp "control: local-path remote allowed" 0 $?
  "$S" check "$C" --root "$t/bad" --visibility public; exp "G1 mutation: marker missing refuses codeberg" 1 $?
  "$S" check "$M" --root "$t/bad" --visibility private; exp "G1 mutation: marker missing refuses m3w" 1 $?
  : > "$t/bad/x"; mkdir -p "$t/bad/docs/public-readiness"; : > "$t/bad/docs/public-readiness/AUDIT-GO"
  "$S" check "$C" --root "$t/bad" --visibility public; exp "G1 mutation: EMPTY marker refuses" 1 $?
  "$S" check "$M" --root "$t/ok" --visibility public;  exp "G2 mutation: m3w public refused" 1 $?
  "$S" check git@GIT.M3W.ORG:vasic-digital/job_seeker.git --root "$t/ok" --visibility public; exp "G2 mutation: upper-case m3w host still refused" 1 $?
  "$S" check git@git.m3w.org.:vasic-digital/job_seeker.git --root "$t/ok" --visibility public; exp "G2 mutation: trailing-dot m3w host still refused" 1 $?
  "$S" check "$M" --root "$t/ok" --visibility absent;  exp "G2 mutation: m3w absent refused (rc2)" 2 $?
  "$S" check "$M" --root "$t/ok" --visibility unknown; exp "G2 mutation: m3w undeterminable refused (rc2)" 2 $?
  "$S" enable codeberg --root "$t/ok" --visibility absent; exp "G3 mutation: enable with absent repo refused" 1 $?
  [ -f "$t/ok/upstreams/codeberg.sh.disabled" ]; exp "G3 mutation: refused enable left recipe disabled" 0 $?
  "$S" enable m3w --root "$t/ok" --visibility public; exp "G3 mutation: enable m3w public refused" 1 $?
  "$S" enable codeberg --root "$t/bad" --visibility public; exp "G3 mutation: enable without marker refused" 1 $?
  "$S" enable codeberg --root "$t/ok" --visibility public; exp "control: enable codeberg existing public + marker" 0 $?
  [ -f "$t/ok/upstreams/codeberg.sh" ] && [ ! -f "$t/ok/upstreams/codeberg.sh.disabled" ]; exp "control: recipe renamed to .sh" 0 $?
  rm -rf "$t"
  echo "suite: $pass passed, $fail failed"
  [ "$fail" -eq 0 ]
}

prove_failure() {
  local d m rc=0 n=0 caught=0
  suite "$SELF" || { echo "FAIL: unmutated guard does not pass its own suite"; return 1; }
  d="$(mktemp -d)"
  # each mutation neuters ONE guard in a throwaway copy; the suite MUST then fail
  mut() { # label sed-expression
    n=$((n+1)); m="$d/m$n.sh"; sed "$2" "$SELF" > "$m"; chmod +x "$m"
    if cmp -s "$m" "$SELF"; then echo "FAIL mutation '$1' did not change the script (inoperative proof)"; rc=1; return; fi
    if suite "$m" >/dev/null 2>&1; then echo "FAIL mutation '$1' NOT caught"; rc=1; else caught=$((caught+1)); echo "PASS mutation '$1' caught"; fi
  }
  mut "G1 marker check removed" 's/if \[ ! -s "\$root\/\$MARKER" \]; then/if false; then/'
  mut "G1 marker accepts empty file" 's/\[ ! -s "\$root\/\$MARKER" \]/[ ! -e "$root\/$MARKER" ]/'
  mut "G2 m3w public allowed" 's/public) echo "upstream_guard: REFUSED: m3w repository \$path is PUBLIC; m3w is private-only" >&2; return 1;;/public) ;;/'
  mut "G2 m3w unknown allowed" 's/\*) echo "upstream_guard: REFUSED (rc 2): m3w repository/*) true; echo "upstream_guard: (mut) m3w repository/; s/visibility is .\$vis., not verified private" >&2; return 2;;/visibility is x" >\&2;;/'
  mut "host normalisation removed" 's/u="\$(printf .%s. "\$u" | tr .A-Z. .a-z.)"; printf .%s. "\${u%.}"; }/printf "%s" "$u"; }/'
  mut "G3 absent repository enabled" 's/absent) echo "upstream_guard: REFUSED: target repository \$host\/\$path does not exist yet" >&2; return 1;;/absent) ;;/'
  mut "G3 enable skips guard check" 's/cmd_check "\$url" --root "\$root" --visibility "\$vis" || return \$?/true/'
  rm -rf "$d"
  echo "upstream_guard --prove-failure: $caught/$n mutations caught"
  return $rc
}

case "${1:-}" in
  check) shift; cmd_check "$@";;
  enable) shift; cmd_enable "$@";;
  --prove-failure) prove_failure;;
  *) sed -n 2,22p "$SELF" >&2; exit 2;;
esac
