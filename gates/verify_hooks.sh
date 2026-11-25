#!/usr/bin/env bash
# verify_hooks.sh - JSK-T005. Exercises scripts/hooks/{commit-msg,pre-push} in a THROWAWAY repository
# (never the real one) and requires each refusal to actually refuse and each valid case to pass.
# Control arm: with the hooks NOT installed the same bad commit is accepted, proving the hooks are
# what refuses. --prove-failure: a deliberately neutered copy of commit-msg must make this gate FAIL.
# Exit 0 pass, 1 fail, 2 cannot determine.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
hooks_src="${HOOKS_SRC:-$here/scripts/hooks}"
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  cp -r "$here/scripts/hooks" "$t/hooks"; printf '#!/usr/bin/env bash\nexit 0\n' > "$t/hooks/commit-msg"
  HOOKS_SRC="$t/hooks" bash "$0" >/dev/null 2>&1; rc=$?
  [ "$rc" -eq 1 ] || { echo "PROVE-FAILURE BAD: neutered hook gave rc=$rc"; exit 1; }
  # G06 mutation: a hook that hardcodes the id range again must be caught by the data-driven cases
  cp -r "$here/scripts/hooks" "$t/hooks2"
  python3 - "$t/hooks2/commit-msg" <<'PY'
import re,sys
p=sys.argv[1]; s=open(p).read()
s=re.sub(r'printf .%s\\n. "\$ids" \| grep -qxF "\$item"', 'printf "%s\\n" "$item" | grep -qE "^JSK-0(0[1-9]|1[0-9]|2[0-7])$"', s)
open(p,"w").write(s)
PY
  cmp -s "$here/scripts/hooks/commit-msg" "$t/hooks2/commit-msg" && { echo "PROVE-FAILURE BAD: G06 mutation did not apply"; exit 1; }
  HOOKS_SRC="$t/hooks2" bash "$0" >/dev/null 2>&1; rc2=$?
  [ "$rc2" -eq 1 ] || { echo "PROVE-FAILURE BAD: hardcoded-range hook gave rc=$rc2"; exit 1; }
  echo "PROVE-FAILURE OK: a neutered hook and a hardcoded-range hook are both caught (rc=1)"; exit 0
fi
command -v git >/dev/null || { echo "UNDETERMINED: git missing"; exit 2; }
w="$(mktemp -d)"; trap 'rm -rf -- "$w"' EXIT
fails=0
check() { # name expected(0|1) actual
  if [ "$2" = "$3" ]; then echo "PASS  $1"; else echo "FAIL  $1 (want rc=$2 got rc=$3)"; fails=$((fails+1)); fi
}
mk() { # dir
  git init -q "$1" && git -C "$1" config user.email t@example.invalid && git -C "$1" config user.name t \
    && git -C "$1" config commit.gpgsign false || return 2
  mkdir -p "$1/.git/hooks" "$1/scripts"
}
install() { cp -r "$hooks_src" "$1/scripts/hooks"; mkdir -p "$1/specs"; cp -r "$here/specs/." "$1/specs/"; cp "$here/scripts/upstream_guard.sh" "$1/scripts/upstream_guard.sh"; ln -sfn "$1/scripts/hooks/commit-msg" "$1/.git/hooks/commit-msg"; ln -sfn "$1/scripts/hooks/pre-push" "$1/.git/hooks/pre-push"; }
try() { # dir msg -> rc
  ( cd "$1" && git commit -q -m "$2" >/dev/null 2>&1 ); echo $?
}
r="$w/r"; mk "$r" || { echo "UNDETERMINED: cannot init throwaway repo"; exit 2; }
# control: no hooks installed -> a trailer-less commit succeeds
mkdir -p "$r/internal"; echo a > "$r/internal/a.go"; git -C "$r" add -- internal/a.go
check "control: no hook => trailerless commit accepted" 0 "$(try "$r" 'no trailer')"
install "$r"
echo b > "$r/internal/b.go"; git -C "$r" add -- internal/b.go
check "no Roadmap-Item refused" 1 "$(try "$r" 'JSK-T013 something')"
check "bad item id refused" 1 "$(try "$r" $'x\n\nRoadmap-Item: JSK-099')"
check "two Roadmap-Item refused" 1 "$(try "$r" $'x\n\nRoadmap-Item: JSK-001\nRoadmap-Item: JSK-002')"
check "two tasks without reason refused" 1 "$(try "$r" $'JSK-T001 and JSK-T002\n\nRoadmap-Item: JSK-001')"
check "two tasks with reason accepted" 0 "$(try "$r" $'JSK-T001 and JSK-T002\n\nRoadmap-Item: JSK-001\nMulti-Task-Reason: shared skeleton in one tree')"
echo c > "$r/internal/c.go"; git -C "$r" add -- internal/c.go
check "allocated epic JSK-017 accepted (defined in tasks.md)" 0 "$(try "$r" $'JSK-T225 kernel\n\nRoadmap-Item: JSK-017')"
echo c2 > "$r/internal/c2.go"; git -C "$r" add -- internal/c2.go
# G06: the allowed epic set is DATA (specs/*/tasks.md), not a hardcoded range.
check "epic JSK-028 refused while no tasks.md defines it" 1 "$(try "$r" $'x\n\nRoadmap-Item: JSK-028')"
tf="$(ls "$r"/specs/*/tasks.md | head -1)"
printf '\n## Phase 99: fixture (epic JSK-028)\n\nepics JSK-031 JSK-032 JSK-033, JSK-034/JSK-035 (JSK-036)\n' >> "$tf"
check "epic JSK-028 accepted once the throwaway tasks.md defines it" 0 "$(try "$r" $'x\n\nRoadmap-Item: JSK-028')"
for e in 032 035; do echo "d$e" > "$r/internal/d$e.go"; git -C "$r" add -- "internal/d$e.go"; check "adjacent-id list: JSK-$e accepted" 0 "$(try "$r" $'x\n\nRoadmap-Item: JSK-'$e)"; done
echo c3 > "$r/internal/c3.go"; git -C "$r" add -- internal/c3.go
check "bogus JSK-999 refused even with JSK-028 defined" 1 "$(try "$r" $'x\n\nRoadmap-Item: JSK-999')"
check "bogus JSK-ABC refused" 1 "$(try "$r" $'x\n\nRoadmap-Item: JSK-ABC')"
check "four-digit JSK-0280 refused (no prefix match on JSK-028)" 1 "$(try "$r" $'x\n\nRoadmap-Item: JSK-0280')"
rm -f "$r"/specs/*/tasks.md
printf 'x\n\nRoadmap-Item: JSK-001\n' > "$w/msg"
( cd "$r" && bash scripts/hooks/commit-msg "$w/msg" >/dev/null 2>&1 ); check "no readable tasks.md => rc 2 fail closed" 2 "$?"
cp "$here"/specs/*/tasks.md "$tf" 2>/dev/null || { mkdir -p "$(dirname "$tf")"; cp "$here"/specs/*/tasks.md "$tf"; }
check "single task accepted" 0 "$(try "$r" $'JSK-T013 config\n\nRoadmap-Item: JSK-002')"
mkdir -p "$r/random"; echo x > "$r/random/z.txt"; git -C "$r" add -- random/z.txt
check "out-of-scope path without reason refused" 1 "$(try "$r" $'y\n\nRoadmap-Item: JSK-001')"
check "out-of-scope with bogus reason refused" 1 "$(try "$r" $'y\n\nRoadmap-Item: JSK-001\nOut-of-Scope-Reason: because')"
check "out-of-scope with closed-list reason accepted" 0 "$(try "$r" $'y\n\nRoadmap-Item: JSK-001\nOut-of-Scope-Reason: tooling-fix')"
mkdir -p "$r/.github/workflows"; echo x > "$r/.github/workflows/ci.yml"; git -C "$r" add -f -- .github/workflows/ci.yml
check "CI/CD file refused even with trailers" 1 "$(try "$r" $'ci\n\nRoadmap-Item: JSK-001\nOut-of-Scope-Reason: tooling-fix')"
git -C "$r" rm -q --cached -- .github/workflows/ci.yml
# pre-push: non-fast-forward refused, fast-forward accepted (gates skipped here: they belong to the real repo)
bare="$w/bare.git"; git init -q --bare "$bare"; git -C "$r" remote add o "$bare"
( cd "$r" && JSK_SKIP_GATES=1 git push -q o HEAD:refs/heads/main >/dev/null 2>&1 ); check "fast-forward/new ref push accepted" 0 "$?"
git -C "$r" commit -q --amend --no-edit -m $'amended\n\nRoadmap-Item: JSK-001' 2>/dev/null
( cd "$r" && JSK_SKIP_GATES=1 git push -q -f o HEAD:refs/heads/main >/dev/null 2>&1 ); check "forced non-fast-forward push refused" 1 "$?"
[ "$fails" -eq 0 ] && { echo "OK: all hook cases behaved"; exit 0; }
echo "FAILED: $fails case(s)"; exit 1
