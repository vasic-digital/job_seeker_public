# 0006 Rate limiters are local (JSK-T028 catalogue follow-up)

Verdict row `ratelimiter` of ADR 0003 said "reuse candidate (UNCONFIRMED)". Decision at T028: **local, small, in `internal/ratelimit`**.
Reasons: FR-047 needs per-key exponential BACKOFF that never hard-locks the operator out (success resets), and
a `Retry-After`/`next_allowed_at` answer in the closed error model; the catalogue module's source was not read in this task
and adopting it needs the operator-ratified submodule add (ADR 0003). Re-evaluate when that add is ratified; the package is ~150 lines
and has its own tests, so replacing it later is cheap.
