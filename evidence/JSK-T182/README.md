# JSK-T182 / JSK-T186 evidence (wave 1 of docs/research/protocols/13-remediation-backlog.md)

- `red.txt`: `go test ./internal/draft` on the unguarded service (fields stubbed, no behaviour): every new case FAILS because the live
  approval succeeds (`err <nil> want ErrGateRegressed|ErrGateUndetermined|ErrPostingClosed|ErrApprovalHashMismatch`).
- `green.txt`: the same tests after the guards.
- Mutation pairs (tests/mutations.tsv): draft-gate-recheck-off, draft-posting-pin-off, draft-closed-posting-allowed,
  draft-gates-undetermined-passes, match-blocked-company-gate-off: all KILLED.
- Tests: `internal/draft/guards_test.go`; e2e step in `tests/e2e/journey.cjs` (blocked company added to the overlay after shortlisting
  -> HTTP 403 gate_regressed in a real browser).
- Scope notes: the timezone window is a score COMPONENT, not a hard gate, so it is not re-checked (documented in match.HardGates).
  No operator override exists for gate_regressed in this change (the doc allows one; it needs a decision on the override vocabulary).
- Behaviour change: DraftHash is now `draft.v2` and includes the posting raw_sha256; drafts frozen under v1 fail verification
  once and return to draft_prepared (re-Prepare), by design (fail closed).
