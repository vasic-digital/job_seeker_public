# Product research, architecture and roadmap

| | |
|---|---|
| Date | 2026-11-24 |
| Status | PROPOSAL. Docs only: no production code is changed by this directory. |
| Origin | Operator directive of 2026-11-24: "The Job Seeker MUST become a full product we can sell and advertise in the future, fully configurable, generic and universal, usable by ANY job-seeking user profiles, or even companies!" |
| Evidence rule | Every external claim carries a URL and the retrieval date 2026-11-24; `UNCONFIRMED` marks what could not be verified. Legal and regulatory material is context for counsel, never a conclusion. |
| Follow-up | A second docs-only PR amends `specs/001-job-seeker-mvp/{spec,tasks}.md` and the workable-items DB with the epics and ids proposed in 09. |

| File | Content |
|---|---|
| [01-vision-personas-and-market.md](01-vision-personas-and-market.md) | Vision, seven personas, eight competitors with prices, unique differentiators |
| [02-generic-profile-and-domain-model.md](02-generic-profile-and-domain-model.md) | Occupation Packs replacing the 10 areas, Profile and Fact ledger, document engine, ESCO/O*NET/ISCO mapping, migrations sketch |
| [03-multi-tenant-architecture.md](03-multi-tenant-architecture.md) | Stages S0-S4, tenancy isolation options, per-tenant keys, RBAC, SSO, audit, quotas, white-label, API, marketplace |
| [04-deployment-and-operations.md](04-deployment-and-operations.md) | Self-host, SaaS, on-prem, observability, backup/DR, upgrades, SLO hypotheses |
| [05-commercial-licensing-and-third-party-terms.md](05-commercial-licensing-and-third-party-terms.md) | Provider terms vs a sold product, jobops consequence, dependency licence audit, own licence options, trademark, app stores |
| [06-compliance-privacy-and-ai-regulation.md](06-compliance-privacy-and-ai-regulation.md) | GDPR roles, EU AI Act candidate-side vs company-side, feature gating, US laws, accessibility, consumer law, certifications |
| [07-business-model-pricing-and-go-to-market.md](07-business-model-pricing-and-go-to-market.md) | Editions, price hypotheses, billing, site, channels, metrics |
| [08-configurability-matrix.md](08-configurability-matrix.md) | 30 configurable behaviours, precedence layers, storage, admin UX |
| [09-product-roadmap-and-spec-impact.md](09-product-roadmap-and-spec-impact.md) | Spec items to change, stages, epics JSK-018..027, FR-200..234, SC-100..115, T400..T523, risks, operator decisions |

Reading order for a decision-maker: 09 sections 8-9, then 05 and 06, then 07, then 02-04 and 08.
