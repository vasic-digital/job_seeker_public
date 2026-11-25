# JSK-T177 knockout pre-flight: evidence

- `red.txt`: RED before code. The knockout package had no non-test files (build failed) and `draft.Service` had no `Knockout` field (vet failed).
- `mutations.txt`: 11 rows `t177-*` of `tests/mutations.tsv`, each run through `gates/hardening_mutation.sh --only <id>` under `scripts/agent-admit.sh`: all KILLED.
- `gates.txt`: `gates/verify_plugins.sh --static` rc 0 (14 plugin packages, 21 registered) and `gates/verify_gold.sh`: three identical runs, digest `d0cc8b49357046a1ada29b27a0a02193c585d3dc9a63ff19563dbff79c1c5a04` (unchanged; this task never touches the gold grid or scoring).
- `green-fail-lines.txt`: output of `go test ./internal/... ./cmd/...` with every `ok` line filtered out; empty means no failure.

Tests use the real SQLite store, the real keyring, the real `match.LoadOverlay` over a temp `overlay.yaml` with synthetic values, and the real first-party handlers. Nothing is a double of the unit under test.

Behaviour: handlers only recognise; the operator's answer of record is the overlay key `work_auth_countries`. Dry-run (the default) never blocks and records `warned_knockout`. A live approval is refused (`knockout_unconfirmed`, audit `refused_knockout_unconfirmed`) while any recognised question is unfavourable or unanswerable and not acknowledged by id; a pass records `knockout_checked`. Audit rows carry `id:state:cause[:ack]` strings only.
