#!/usr/bin/env bash
# publish_snapshot.sh - ONE-WAY clean-snapshot publisher: dev repo (private history) -> public repo (no dev history).
#
# Usage:
#   publish_snapshot.sh build [--src REF] [--out DIR] [--parent GITURL] [--message MSG]
#   publish_snapshot.sh push  --out DIR URL [URL...]        # fast-forward / initial push of branch main only
#   publish_snapshot.sh --prove-failure                      # hermetic self-test + mutants
# `build` exports REF (default github/main) of the CURRENT repo with `git archive`, keeps submodule gitlinks
# (their URLs rewritten to https in .gitmodules), then REFUSES (rc 1) when any of these fail on the snapshot:
#   S1 a tracked path matches scripts/publish_exclude.txt
#   S2 gates/verify_public_readiness.sh (run INSIDE the snapshot repo)
#   S3 a contributor e-mail of the dev history (other than the publisher identity) appears in any snapshot
#      file, or .gitmodules still has an ssh-only submodule URL (boolean report)
#   S4 the VALUE of any env var named in PUBLISH_SECRET_ENV (space separated NAMES, default: the provider token
#      names) appears in any snapshot file - boolean only, the value is never printed
# then creates ONE commit: initial (no --parent / parent has no main) or a new commit on top of the public
# repo's current main (tree = clean snapshot). Author/committer = this machine's git identity.
# Pushes are never forced: `git push URL main:refs/heads/main` only; a non-fast-forward is rejected by git.
# Exit: 0 ok, 1 refused, 2 could not determine (never a pass). Tokens: env var NAMES only, never printed.
set -u
SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
ROOT="$(cd "$(dirname "$SELF")/.." && pwd)"
SECRET_ENV_DEFAULT="CODEBERG_GITEA_TOKEN M3W_GITEA_TOKEN GITFLIC_TOKEN GITVERSE_TOKEN GITLAB_TOKEN GITHUB_TOKEN"

die() { echo "publish_snapshot: $*" >&2; exit "${2:-2}"; }

# value_in_tree DIR NAME -> 0 if env var NAME is set (>=8 chars) and its value occurs in any tracked file of DIR
value_in_tree() {
  local d="$1" n="$2" v; v="${!n:-}"
  [ "${#v}" -ge 8 ] || return 1
  PS_V="$v" python3 - "$d" <<'PY'
import os,subprocess,sys
d=sys.argv[1]; v=os.environ["PS_V"].encode()
fs=subprocess.run(["git","-C",d,"ls-files","-z"],capture_output=True).stdout.split(b"\0")
for f in fs:
    if not f: continue
    p=os.path.join(d,f.decode("utf-8","replace"))
    if os.path.isfile(p) and not os.path.islink(p) and v in open(p,"rb").read(): sys.exit(0)
sys.exit(1)
PY
}

verify_snapshot() { # DIR EXCLUDEFILE EMAILS_FILE ; prints refusals; rc 0 clean, 1 refused
  local d="$1" ex="$2" em="$3" bad=0 p g n rc hit
  while IFS= read -r -d '' p; do
    local allowed=0
    while IFS= read -r g; do   # '!glob' lines are explicit, reasoned allowances
      case "$g" in '!'*) g="${g#!}"; [[ "$p" == $g ]] && allowed=1;; esac
    done < "$ex"
    [ "$allowed" -eq 0 ] || continue
    while IFS= read -r g; do
      case "$g" in ''|'#'*|'!'*) continue;; esac
      # shellcheck disable=SC2053
      if [[ "$p" == $g ]]; then echo "REFUSED S1: tracked path '$p' matches exclusion '$g'"; bad=1; fi
    done < "$ex"
  done < <(git -C "$d" ls-files -z)
  if [ -f "$d/gates/verify_public_readiness.sh" ]; then
    bash "$d/gates/verify_public_readiness.sh" >"$d/.pr.out" 2>&1; rc=$?
    tail -n 3 "$d/.pr.out" | sed 's/^/  gate: /'; rm -f "$d/.pr.out"
    [ "$rc" -eq 0 ] || { echo "REFUSED S2: gates/verify_public_readiness.sh rc=$rc"; bad=1; }
  else echo "REFUSED S2: gates/verify_public_readiness.sh missing in snapshot"; bad=1; fi
  if [ -s "$em" ]; then
    hit="$(python3 - "$d" "$em" <<'PY'
import os,subprocess,sys
d,em=sys.argv[1],sys.argv[2]
emails=[l.strip().lower().encode() for l in open(em) if l.strip()]
n=0
for f in subprocess.run(["git","-C",d,"ls-files","-z"],capture_output=True).stdout.split(b"\0"):
    if not f: continue
    p=os.path.join(d,f.decode("utf-8","replace"))
    if os.path.isfile(p) and not os.path.islink(p):
        b=open(p,"rb").read().lower()
        if any(e in b for e in emails): n+=1
print(n)
PY
)"
    [ "${hit:-1}" = 0 ] || { echo "REFUSED S3: $hit file(s) contain a contributor e-mail of the dev history"; bad=1; }
  fi
  if [ -f "$d/.gitmodules" ] && grep -Eq '^\s*url\s*=\s*(git@|ssh://)' "$d/.gitmodules"; then echo "REFUSED S3: .gitmodules has an ssh-only submodule URL"; bad=1; fi
  for n in ${PUBLISH_SECRET_ENV:-$SECRET_ENV_DEFAULT}; do
    if value_in_tree "$d" "$n"; then echo "REFUSED S4: value of env var $n occurs in the snapshot (value not shown)"; bad=1; fi
  done
  return $bad
}

cmd_build() {
  local src="github/main" out="" parent="" msg="" repo="${PUBLISH_SRC_REPO:-$ROOT}" ex="" id_n id_e
  while [ $# -gt 0 ]; do case "$1" in --src) src="$2"; shift 2;; --out) out="$2"; shift 2;; --parent) parent="$2"; shift 2;; --message) msg="$2"; shift 2;; --repo) repo="$2"; shift 2;; --exclude-file) ex="$2"; shift 2;; *) die "unknown arg $1";; esac; done
  [ -n "$out" ] || die "build needs --out DIR"
  if [ -e "$out" ] && [ -n "$(ls -A "$out" 2>/dev/null)" ]; then die "--out $out exists and is not empty"; fi
  ex="${ex:-$ROOT/scripts/publish_exclude.txt}"; [ -f "$ex" ] || die "exclusion list $ex missing"
  command -v python3 >/dev/null || die "python3 missing"
  local sha; sha="$(git -C "$repo" rev-parse --verify "$src^{commit}" 2>/dev/null)" || die "cannot resolve $src in $repo"
  id_n="$(git config user.name)"; id_e="$(git config user.email)"; [ -n "$id_n" ] && [ -n "$id_e" ] || die "git identity not configured"
  mkdir -p "$out" || die "cannot create $out"
  git -C "$repo" archive "$sha" | tar -x -C "$out" || die "git archive failed"
  git -C "$out" init -q -b main || die "init failed"
  git -C "$out" config user.name "$id_n"; git -C "$out" config user.email "$id_e"
  # .gitmodules: ssh -> https (public clones are anonymous)
  if [ -f "$out/.gitmodules" ]; then
    sed -i -E 's#^(\s*url\s*=\s*)git@([A-Za-z0-9.-]+):#\1https://\2/#' "$out/.gitmodules"
  fi
  git -C "$out" add -A -f . || die "add failed"
  # gitlinks (git archive omits them): restore as 160000 index entries
  local gsha gpath
  while read -r gsha gpath; do
    git -C "$out" update-index --add --cacheinfo "160000,$gsha,$gpath" || die "gitlink restore failed for $gpath"
  done < <(git -C "$repo" ls-tree -r "$sha" | awk '$1=="160000"{print $3" "$4}')
  local em; em="$(mktemp)"
  git -C "$repo" log "$sha" --format='%ae%n%ce' | sort -u | grep -viE 'noreply|^$' | grep -vxiF "$id_e" > "$em" || true
  echo "snapshot of $src @ ${sha:0:12}: $(git -C "$out" ls-files | wc -l) tracked entries; checking..."
  verify_snapshot "$out" "$ex" "$em"; local vrc=$?; rm -f "$em"
  [ "$vrc" -eq 0 ] || { echo "publish_snapshot: REFUSED - snapshot left in $out for inspection, NOT committed" >&2; return 1; }
  local pargs=() initial="Initial public snapshot of vasic-digital/job_seeker @ ${sha:0:8}"
  if [ -n "$parent" ]; then
    if git -C "$out" fetch -q "$parent" main 2>/dev/null; then pargs=(-p FETCH_HEAD); msg="${msg:-Public snapshot update of vasic-digital/job_seeker @ ${sha:0:8}}"; else echo "parent has no main (or unreachable): initial commit"; fi
  fi
  msg="${msg:-$initial}"
  local tree c; tree="$(git -C "$out" write-tree)" || die "write-tree failed"
  c="$(git -C "$out" commit-tree "${pargs[@]}" -m "$msg" "$tree")" || die "commit-tree failed"
  git -C "$out" update-ref refs/heads/main "$c" || die "update-ref failed"
  echo "OK: commit $(git -C "$out" rev-parse --short=12 main) in $out ($(git -C "$out" rev-list --count main) commit(s) in history)"
}

cmd_push() {
  local out="" urls=() u rc=0
  while [ $# -gt 0 ]; do case "$1" in --out) out="$2"; shift 2;; -*) die "unknown arg $1";; *) urls+=("$1"); shift;; esac; done
  [ -n "$out" ] && [ "${#urls[@]}" -gt 0 ] || die "push needs --out DIR and URL..."
  git -C "$out" rev-parse --verify main >/dev/null 2>&1 || die "no main in $out"
  for u in "${urls[@]}"; do
    if git -C "$out" push "$u" main:refs/heads/main; then echo "pushed: $u"; else echo "FAILED push: $u" >&2; rc=1; fi
  done
  return $rc
}

selftest() { # $1 = script under test ; rc 0 iff all pass
  local S="$1" t pass=0 fail=0 rc k D R EX
  t="$(mktemp -d)"
  exp() { if [ "$2" = "$3" ]; then pass=$((pass+1)); echo "PASS $1 (rc $3)"; else fail=$((fail+1)); echo "FAIL $1 expected rc $2 got $3"; fi; }
  R="$t/dev"; mkdir -p "$R/gates"; git init -q -b main "$R"
  printf '#!/usr/bin/env bash\ncd "$(dirname "$0")/.." || exit 2\ngrep -rqE "BADMARK" . --include="*.txt" --exclude-dir=.git && { echo found; exit 1; }; exit 0\n' > "$R/gates/verify_public_readiness.sh"
  echo hello > "$R/README.txt"
  printf '[submodule "a"]\n\tpath = sub/a\n\turl = git@github.com:o/a.git\n' > "$R/.gitmodules"
  git -C "$R" add -A; git -C "$R" -c user.name=Dev -c user.email=dev@private.test commit -qm c1
  git -C "$R" update-index --add --cacheinfo 160000,2d18551c6c99e21f4287be5b1d3c927ac3901d07,sub/a; git -C "$R" commit -qm gl
  EX="$t/ex.txt"; printf '.env\n*.pem\nsecrets/*\n' > "$EX"
  PUBLISH_SRC_REPO="$R" "$S" build --src main --out "$t/o1" --exclude-file "$EX" >/dev/null; exp "control: clean snapshot builds" 0 $?
  [ "$(git -C "$t/o1" rev-list --count main)" = 1 ]; exp "control: exactly one commit (no dev history)" 0 $?
  git -C "$t/o1" log -1 --format=%B | grep -q 'Initial public snapshot of vasic-digital/job_seeker @'; exp "control: initial message" 0 $?
  git -C "$t/o1" ls-tree -r main | grep -q '^160000 commit 2d18551c.*sub/a'; exp "control: gitlink preserved" 0 $?
  git -C "$t/o1" show main:.gitmodules | grep -q 'https://github.com/o/a.git'; exp "control: submodule URL rewritten to https" 0 $?
  [ "$(git -C "$t/o1" log -1 --format=%ae)" = "$(git config user.email)" ]; exp "control: author = configured identity, not dev" 0 $?
  git -C "$t/o1" log --all -p | grep -q dev@private.test; [ $? -eq 1 ]; exp "control: dev author e-mail absent from public history" 0 $?
  git init -q --bare "$t/pub.git"; "$S" push --out "$t/o1" "$t/pub.git" >/dev/null 2>&1; exp "control: initial push" 0 $?
  echo more > "$R/more.txt"; git -C "$R" add -A; git -C "$R" commit -qm c3
  PUBLISH_SRC_REPO="$R" "$S" build --src main --out "$t/o2" --parent "$t/pub.git" --exclude-file "$EX" >/dev/null; exp "control: update build" 0 $?
  [ "$(git -C "$t/o2" rev-list --count main)" = 2 ]; exp "control: update = new commit on top (2 commits)" 0 $?
  "$S" push --out "$t/o2" "$t/pub.git" >/dev/null 2>&1; exp "control: update push is fast-forward" 0 $?
  PUBLISH_SRC_REPO="$R" "$S" build --src main --out "$t/o3" --exclude-file "$EX" >/dev/null
  "$S" push --out "$t/o3" "$t/pub.git" >/dev/null 2>&1; [ $? -ne 0 ]; exp "mutation: divergent (non-ff) push rejected, never forced" 0 $?
  printf '!keep.pem\n' >> "$EX"; mkdir -p "$t/da"; cp -r "$R/." "$t/da/"; echo p > "$t/da/keep.pem"; git -C "$t/da" add -A -f; git -C "$t/da" -c user.name=Dev -c user.email=dev@private.test commit -qm a
  PUBLISH_SRC_REPO="$t/da" "$S" build --src main --out "$t/oa" --exclude-file "$EX" >/dev/null; exp "control: '!' allowance lets a named path through" 0 $?
  local sv="ZZsecret""value99ZZ"
  for k in env pem gate mail secret; do
    D="$t/dev$k"; cp -r "$R" "$D"
    case "$k" in
      env) echo X=1 > "$D/.env";;
      pem) mkdir -p "$D/x"; echo x > "$D/x/a.pem";;
      gate) echo BADMARK > "$D/n.txt";;
      mail) echo "contact dev@private.test" > "$D/n.txt";;
      secret) echo "tok $sv" > "$D/n.txt";;
    esac
    git -C "$D" add -A -f; git -C "$D" -c user.name=Dev -c user.email=dev@private.test commit -qm bad
    PUBLISH_SECRET_ENV=PS_TEST_TOKEN PS_TEST_TOKEN="$sv" PUBLISH_SRC_REPO="$D" "$S" build --src main --out "$t/r$k" --exclude-file "$EX" >"$t/r$k.log" 2>&1; rc=$?
    exp "mutation: $k refused" 1 $rc
    git -C "$t/r$k" rev-parse --verify main >/dev/null 2>&1; [ $? -ne 0 ]; exp "mutation: $k leaves NO commit" 0 $?
    grep -q "$sv" "$t/r$k.log"; [ $? -ne 0 ]; exp "$k: output never prints the secret value" 0 $?
  done
  PUBLISH_SRC_REPO="$R" "$S" build --src nope --out "$t/o9" --exclude-file "$EX" >/dev/null 2>&1; exp "unresolvable ref -> rc 2" 2 $?
  PUBLISH_SRC_REPO="$R" "$S" build --src main --out "$t/o1" --exclude-file "$EX" >/dev/null 2>&1; exp "non-empty --out -> rc 2" 2 $?
  echo "selftest: $pass passed, $fail failed"; [ "$fail" -eq 0 ]
}

prove_failure() {
  selftest "$SELF" || { echo "FAIL: unmutated tool fails its own suite"; return 1; }
  local d n=0 caught=0 rc=0 m; d="$(mktemp -d)"
  mut() { n=$((n+1)); m="$d/m$n.sh"; sed "$2" "$SELF" > "$m"; chmod +x "$m"
    if cmp -s "$m" "$SELF"; then echo "FAIL mutation '$1' changed nothing"; rc=1; return; fi
    if selftest "$m" >/dev/null 2>&1; then echo "FAIL mutation '$1' NOT caught"; rc=1; else caught=$((caught+1)); echo "PASS mutation '$1' caught"; fi; }
  mut "S1 exclusion match disabled" 's/if \[\[ "\$p" == \$g \]\]; then/if false; then/'
  mut "S1 allowance ignored" 's/\[\[ "\$p" == \$g \]\] \&\& allowed=1/true/'
  mut "S2 gate failure ignored" 's/\[ "\$rc" -eq 0 \] || { echo "REFUSED S2/true || { echo "REFUSED S2/'
  mut "S3 contributor e-mail scan disabled" 's/\[ "\${hit:-1}" = 0 \] ||/true ||/'
  mut "S4 secret value scan disabled" 's/if value_in_tree "\$d" "\$n"; then/if false; then/'
  mut "ssh->https rewrite removed" 's/sed -i -E .s#\^(.*$/true/'
  mut "gitlinks dropped" 's/update-index --add --cacheinfo "160000,\$gsha,\$gpath"/true/'
  mut "parent ignored (always initial)" 's/if git -C "\$out" fetch -q "\$parent" main 2>\/dev\/null; then/if false; then/'
  mut "push forced" 's/push "\$u" main:refs\/heads\/main/push -f "$u" main:refs\/heads\/main/'
  mut "dev identity leaks via author" 's/git -C "\$out" config user.email "\$id_e"/git -C "$out" config user.email dev@private.test/'
  echo "publish_snapshot --prove-failure: $caught/$n mutations caught"; return $rc
}

case "${1:-}" in
  build) shift; cmd_build "$@";;
  push) shift; cmd_push "$@";;
  --prove-failure) prove_failure;;
  *) sed -n 2,24p "$SELF" >&2; exit 2;;
esac
