#!/usr/bin/env bash
# check_no_personal_data.sh - JSK-T088 (FR-054, FR-077). Loads the overlay values at run time (never printed),
# scans the tracked tree, plus any paths in PDGATE_EXTRA (colon-separated: image-layer exports, a public DB
# copy, logs, evidence), and fails on a tracked posting body, PDF or image outside gates/pdgate.allow and on
# any tracked path under the private data directories (JOBSEEKER_DATA_DIR, CAREER_PRIVATE_DIR).
# Exit 0 clean, 1 findings, 2 cannot determine (no overlay.yaml reachable: this is NEVER a pass).
# --prove-failure: runs the real scanner against a synthetic overlay and a throwaway git repo and requires
# every needle control to behave (hit where a value is planted, clean where it is not, rc 2 without overlay).
set -u
cd "$(dirname "$0")/.."
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
BIN="$(mktemp -d)/jspdscan"
go build -o "$BIN" ./cmd/jspdscan || { echo "UNDETERMINED: build failed"; exit 2; }

if [ "${1:-}" = "--prove-failure" ]; then
  T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
  mkdir -p "$T/ov" "$T/repo"
  cat > "$T/ov/overlay.yaml" <<'Y'
salary_min: 187431
salary_currency: EUR
blocked_companies:
  - Zorbanex Holdings
Y
  git -C "$T/repo" init -q; git -C "$T/repo" config user.email t@t; git -C "$T/repo" config user.name t
  echo "ordinary text, price in EUR, nothing else" > "$T/repo/a.txt"
  git -C "$T/repo" add a.txt; git -C "$T/repo" commit -qm base
  fail=0
  expect() { # name want_rc args...
    local n="$1" want="$2"; shift 2
    CAREER_PRIVATE_DIR="$T/ov" "$BIN" "$@" >"$T/out" 2>&1; local rc=$?
    if [ "$rc" -eq "$want" ]; then echo "  ok   $n (rc $rc)"; else echo "  BAD  $n: rc $rc want $want"; fail=1; fi
    if grep -qE '187431|Zorbanex' "$T/out"; then echo "  BAD  $n: output printed a needle value"; fail=1; fi
  }
  expect "clean repo; low-entropy EUR alone is not a hit" 0 -repo "$T/repo"
  echo "contract at 187431 per year" > "$T/repo/b.txt"; git -C "$T/repo" add b.txt
  expect "high-entropy salary figure in a tracked file" 1 -repo "$T/repo"
  git -C "$T/repo" rm -qf --cached b.txt; rm "$T/repo/b.txt"
  echo "salary_currency: EUR" > "$T/repo/c.txt"; git -C "$T/repo" add c.txt
  expect "low-entropy value WITH its key name" 1 -repo "$T/repo"
  git -C "$T/repo" rm -qf --cached c.txt; rm "$T/repo/c.txt"
  echo "never apply to zorbanex holdings" > "$T/repo/d.txt"; git -C "$T/repo" add d.txt
  expect "blocked company name (case-insensitive)" 1 -repo "$T/repo"
  git -C "$T/repo" rm -qf --cached d.txt; rm "$T/repo/d.txt"
  printf '%%PDF-1.4 x' > "$T/repo/cv.pdf"; git -C "$T/repo" add cv.pdf
  expect "tracked PDF outside the allow-list" 1 -repo "$T/repo"
  printf 'docs/\n' > "$T/allow"
  mkdir -p "$T/repo/docs"; git -C "$T/repo" mv cv.pdf docs/cv.pdf
  expect "tracked PDF inside an allowed prefix" 0 -repo "$T/repo" -allow "$T/allow"
  mkdir -p "$T/repo/postings"; echo '{"body":"x"}' > "$T/repo/postings/1.json"; git -C "$T/repo" add postings
  expect "tracked posting body" 1 -repo "$T/repo" -allow "$T/allow"
  git -C "$T/repo" rm -qf --cached postings/1.json; rm -r "$T/repo/postings"
  mkdir -p "$T/priv"; echo hi > "$T/repo/p.txt"; git -C "$T/repo" add p.txt
  expect "tracked path under a private dir" 1 -repo "$T/repo" -private-dir "$T/repo"
  git -C "$T/repo" add -A >/dev/null
  mkdir -p "$T/layer"; echo "x 187431 y" > "$T/layer/blob"
  git -C "$T/repo" rm -qf --cached docs/cv.pdf p.txt >/dev/null 2>&1; rm -rf "$T/repo/docs" "$T/repo/p.txt"
  expect "needle only in an extra path (image layer / DB copy / log)" 1 -repo "$T/repo" -extra "$T/layer"
  CAREER_PRIVATE_DIR="$T/nowhere" "$BIN" -repo "$T/repo" >/dev/null 2>&1; rc=$?
  if [ "$rc" -eq 2 ]; then echo "  ok   no overlay -> UNDETERMINED (rc 2)"; else echo "  BAD  no overlay rc $rc want 2"; fail=1; fi
  [ "$fail" -eq 0 ] && { echo "PROVE-FAILURE OK: every needle control behaves"; exit 0; }
  echo "PROVE-FAILURE BAD"; exit 1
fi

ov="${CAREER_PRIVATE_DIR:-}"
args=(-repo . -allow gates/pdgate.allow)
[ -n "${JOBSEEKER_DATA_DIR:-}" ] && args+=(-private-dir "$JOBSEEKER_DATA_DIR")
[ -n "$ov" ] && args+=(-private-dir "$ov")
IFS=: read -ra ex <<< "${PDGATE_EXTRA:-}"
for e in "${ex[@]:-}"; do [ -n "$e" ] && args+=(-extra "$e"); done
"$BIN" "${args[@]}"; exit $?
