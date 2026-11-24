#!/usr/bin/env bash
# verify_nosend.sh - JSK-T068 no-send-bypass gate (FR-026, FR-027, SC-005). Runs, against the real SQLite apply path:
#   * the 8 named bypass attempts (one per FR-026 condition) at approve and the re-checks at handoff-complete,
#   * the static enumeration that HandoffComplete is the only code writing `sent`,
#   * the static proof that internal/draft imports no network or generative client.
# Exit 0 pass, 1 fail, 2 cannot determine.
# --prove-failure: re-runs the same tests through `go test -overlay` against mutated copies of internal/draft/service.go
# (live-mode check, kill-switch check, step-up check, company check, per-cap check removed; a second `sent` writer
# added); the unmutated control must pass and every mutant must FAIL.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
RUN='TestEightBypassAttemptsAtApprove|TestHashMismatchBypassAtApproveSupersedes|TestHandoffRechecks|TestOnlyHandoffWritesSent|TestNoGenerativeImports|TestPerHostAndDailyCaps|TestGateRestartAndKillSwitch'
run() { (cd "$here" && go test -count=1 -run "$RUN" "$@" ./internal/draft >/dev/null 2>&1); }
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  src="$here/internal/draft/service.go"
  ovl() { # name sed-expr : writes mutated copy and an overlay json
    sed -E "$2" "$src" > "$t/$1.go"
    cmp -s "$src" "$t/$1.go" && { echo "PROVE-FAILURE BAD: mutation $1 changed nothing"; exit 1; }
    printf '{"Replace":{"%s":"%s"}}' "$src" "$t/$1.go" > "$t/$1.json"
  }
  ovl live  's/if !s\.Gate\.LiveMode\(\) \{/if false {/'
  ovl kill  's/if killSwitchTx\(ctx, tx, s\.Gate\.EnvKill\) \{/if false {/'
  ovl step  's/if !s\.stepUpFresh\(in\.StepUpAt\) \{/if false {/'
  ovl co    's/strings\.TrimSpace\(in\.CompanyConfirm\) != p\.Company/false/'
  ovl cap   's/if n >= daily \|\| h >= perHost \{/if false {/'
  ovl hash  's/if a\.ApprovedHash != a\.DraftHash \{/if false {/'
  # static mutants: the static tests read DRAFT_SRC_DIR (a throwaway copy of the package with one extra file)
  for m in second llm; do mkdir "$t/$m"; cp "$here"/internal/draft/*.go "$t/$m/"; done
  printf 'package draft\n\nimport (\n\t"context"\n\t"database/sql"\n)\n\nfunc (s *Service) leak(ctx context.Context, tx *sql.Tx, id int64) error {\n\treturn s.transition(ctx, tx, id, "live_approved", "sent", "x", "handoff_recorded", nil)\n}\n' > "$t/second/leak.go"
  printf 'package draft\n\nimport _ "net/http"\n' > "$t/llm/net.go"
  run; c=$?
  bad=""
  for m in live kill step co cap hash; do run -overlay "$t/$m.json"; rc=$?; [ "$rc" -eq 0 ] && bad="$bad $m"; done
  for m in second llm; do (cd "$here" && DRAFT_SRC_DIR="$t/$m" go test -count=1 -run 'TestOnlyHandoffWritesSent|TestNoGenerativeImports' ./internal/draft >/dev/null 2>&1); rc=$?; [ "$rc" -eq 0 ] && bad="$bad $m"; done
  if [ "$c" -eq 0 ] && [ -z "$bad" ]; then echo "PROVE-FAILURE OK: control rc=0; 8 mutants (live, kill, step-up, company, cap, hash, second sent writer, network import) all rejected"; exit 0; fi
  echo "PROVE-FAILURE BAD: control=$c survivors:$bad"; exit 1
fi
run && { echo "OK: no-send-bypass suite green (8 FR-026 attempts, handoff re-checks, sent-writer enumeration, no generative import)"; exit 0; }
echo "FAIL: no-send-bypass suite failed"; exit 1
