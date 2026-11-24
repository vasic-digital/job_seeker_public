# Deferred providers (FR-011): deferred, NOT removed

Each item implements `providers.Provider` later, behind its own ToS review, with no core change
(see `extending.md`). Status for all three: DEFERRED, scope kept, no adapter written, no network contact.

| Provider | Needs before work starts | Open question |
|---|---|---|
| jobspipe | ToS review: attribution, storage, rate limit, re-publication | API shape UNKNOWN (not researched) |
| Sprout AI | ToS review as above; operator decision on a vendor account | API shape UNKNOWN |
| SmartApplier | ToS review as above; it is an apply-automation vendor, so the FR-010/human-approval boundary must be re-checked first | API shape UNKNOWN |

Registered in `docs/workable_items.db` (type Feature, status Queued because the closed status set has no Deferred value; title prefix "Deferred provider", epic JSK-003): JSK-013 jobspipe, JSK-014 Sprout AI, JSK-015 SmartApplier.
