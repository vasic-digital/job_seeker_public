# Adding a job-source provider (FR-011)

A new provider needs exactly three things and touches no core code:

1. An adapter in `internal/providers` implementing `providers.Provider` (`Name`, `Limits`, `Fetch`,
   `LegalNotice`). It is a pure translator: it receives the egress client, makes one request, returns a
   `Batch` with a complete `CallProof`; it owns no database, no sleep and no retry. A malformed record is a
   `SkipReason`, a malformed envelope a `*providers.Error` of class `schema`. It must keep the provider's own
   URL in `SourceURL` and never carry a logo field.
2. One line in the egress allow-list file (exact DNS name; `deploy/egress-allowlist.txt`). A host not listed
   is refused before any DNS query.
3. A provider row. `ingest.Runner.EnsureProvider` inserts it on first run from the adapter's `Limits`.
   (`jsctl provider add` is the operator form.)

Then add the adapter to `Runner.Providers`. Limits are enforced by the persisted limiter, backoff and circuit
state by the runner, attribution by `providers.AttributionFor` (add the display name and home link).

Proof: `internal/ingest` `TestNewProviderNeedsOnlyAdapterAndRow` runs a brand-new provider type end to end
(limiter, run row, provider_call row, upsert, encrypted body, health) with no change outside the adapter.
That test uses a provider double and is a tool-logic unit test, not evidence that any real provider works.

## ToS review gate

Each provider carries its own terms. Before enabling one, record: attribution rule, caching/storage rule,
rate limit (documented vs policy), re-publication ban, and whether a key or account acceptance is needed.
Himalayas is implemented but its live use waits for the operator's ToS acceptance and key decision.
