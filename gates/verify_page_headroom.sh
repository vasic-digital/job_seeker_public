#!/usr/bin/env bash
# verify_page_headroom.sh - JSK-T173 gate (decision 0009, option a): the ATS profile keeps the page maximum and
# warns (advisory, never a block) when a document REACHES it. Runs TestPageHeadroomWarning. Exit 0 pass, 1 fail,
# 2 cannot determine.
# --prove-failure: runs the test on throwaway copies carrying one mutation each of profile.go; the control must
# pass and every mutation must be rejected: warning removed, warning moved to "block", warning fires under the
# limit, over-limit block dropped.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
run() { (cd "$1" && go test -count=1 ./internal/ext/ats/profile -run 'PageHeadroom' >/dev/null 2>&1); }
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  cp_tree() { mkdir "$t/$1"; (cd "$here" && tar --exclude=.git --exclude=evidence --exclude=submodules -cf - .) | tar -xf - -C "$t/$1"; }
  f=internal/ext/ats/profile/profile.go
  declare -A MUT=(
    [nowarn]='s/d.Pages == p.f.Upload.MaxPages:/false:/'
    [asblock]='s/fnd("warn", "page_budget_at_limit")/fnd("block", "page_budget_at_limit")/'
    [under]='s/d.Pages == p.f.Upload.MaxPages:/d.Pages >= p.f.Upload.MaxPages-1:/'
    [noblock]='s/d.Pages > p.f.Upload.MaxPages:/false:/'
  )
  cp_tree ctl; run "$t/ctl" || { echo "PROVE-FAILURE BAD: control copy fails"; exit 1; }
  bad=0
  for k in "${!MUT[@]}"; do
    cp_tree "m_$k"; sed -i "${MUT[$k]}" "$t/m_$k/$f"
    if cmp -s "$here/$f" "$t/m_$k/$f"; then echo "PROVE-FAILURE BAD: mutation $k did not change the source"; bad=1; continue; fi
    if run "$t/m_$k"; then echo "PROVE-FAILURE BAD: mutation $k survived"; bad=1; fi
  done
  [ "$bad" -eq 0 ] && { echo "PROVE-FAILURE OK: control passes; ${#MUT[@]} mutations all rejected"; exit 0; }
  exit 1
fi
run "$here" && { echo "OK: page-headroom warning suite green"; exit 0; }
echo "FAIL: page-headroom suite failed"; exit 1
