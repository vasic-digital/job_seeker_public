# Contract: job-source provider adapter

Covers FR-001..FR-012, FR-011 (extension point).

```go
package providers

type Provider interface {
    Name() string                         // "remotive" | "remoteok" | "himalayas"
    Limits() Limits                       // documented or policy limits, enforced by the limiter, not by the adapter
    Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error)
    LegalNotice(b Batch) string           // text re-read each run, hashed by the caller
}

type Limits struct {
    MinInterval time.Duration            // remoteok 60m (project policy, none published), himalayas 2h, remotive 6h
    MaxPages int                         // Himalayas poll cycle: page 1..MaxPages once per MinInterval (anonymous limit unpublished)
    MaxPerDay   int                      // remotive 4
    PageSize    int
}

type Batch struct {
    Postings []Posting
    Skipped  []SkipReason                // malformed records, counted by error class
    Call     CallProof                   // host, resolved ip, tls subject, status, request id, latency, response sha256
}

type Posting struct {                    // canonical (see data-model posting)
    ProviderName, ProviderJobID, URL, SourceURL, ApplyURL, Title, Company, LocationRaw string
    RawSHA256 string                     // per record
    SalaryPeriod string
    RemoteMode string; Countries []string; TZHint string
    SalaryMin, SalaryMax int; SalaryCur string
    Tags []string; Body string           // body is raw here, sanitized by the pipeline before storage
    PostedAt, ExpiresAt time.Time
}
```

## Rules

1. Adapters never construct their own `http.Client`; they receive the egress client. `depguard` bans `http.DefaultClient`, `http.Get`, `net.Dial` outside `internal/egress`.
2. Adapters are pure translators: no DB access, no sleeping, no retry (the runner owns backoff, the limiter, and circuit state).
3. A malformed single record becomes a `SkipReason`; a malformed envelope returns an error with class `schema`.
4. Provider URL and provider name are mandatory fields of every `Posting`; empty means the record is skipped.
5. Himalayas: `applicationLink` is stored verbatim in `ApplyURL` (never rewritten); expired records (`expiryDate` past) are marked with `ExpiresAt`.
6. Deferred providers (jobspipe, Sprout AI, SmartApplier) implement the same interface later; each needs its own ToS review item.
7. Adding a provider requires an entry in the egress allow-list file and a row in `provider` via migration or `jsctl provider add`; nothing else changes.

## Contract tests

- Live: one real call per provider yields a non-empty `Batch` and a complete `CallProof` (SKIP with reason `topology_unsupported` if the network is absent).
- Offline replay of a captured real payload as INPUT to the translator (labelled `fixture-input`, provenance recorded) for determinism tests only; never labelled as a live PASS.
