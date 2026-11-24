#!/usr/bin/env bash
# verify_tiebreak.sh - JSK-009 gate (FR-020, FR-049, FR-009): runs the internal/llm suite (accept rule, closed
# answer set, gate==shadow, real-text refusal, 5xx fallback, 35-body injection corpus). The in-process provider in
# those tests is a double of the CLIENT's logic only; the vendor itself is exercised by the live test (SKIP
# credentials_absent without TYPESAFE_API_KEY). Exit 0 pass, 1 fail, 2 cannot determine.
# --prove-failure: runs the suite on throwaway copies of the tree carrying one mutation each of tiebreak.go; the
# control must pass and every mutation must be rejected: accept rule weakened (0.75 -> 0.0), top-3 membership
# ignored, shadow gate dropped, real-text approval ignored, ambiguous-only guard removed, key sent to any origin.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
run() { (cd "$1" && go test -count=1 ./internal/llm >/dev/null 2>&1); }
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  cp_tree() { mkdir "$t/$1"; (cd "$here" && tar --exclude=.git --exclude=evidence -cf - .) | tar -xf - -C "$t/$1"; }
  f=internal/llm/tiebreak.go
  declare -A MUT=(
    [conf]='s/MinConfidence = 0.75/MinConfidence = 0.0/'
    [top3]='s/case !inSet:/case false:/'
    [gate]='s/if o.JevGate != "shadow" {/if false {/'
    [realtext]='s/if !q.Synthetic && !t.o.AllowRealText {/if false {/'
    [ambig]='s/if m.Status != match.StatusAmbiguous {/if false {/'
    [origin]='s/!strings.EqualFold(strings.TrimRight(t.o.BaseURL, "\/"), PinnedBaseURL) && !t.o.testOrigin()/false/'
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
run "$here" && { echo "OK: internal/llm tiebreak suite green"; exit 0; }
echo "FAIL: internal/llm suite failed"; exit 1
