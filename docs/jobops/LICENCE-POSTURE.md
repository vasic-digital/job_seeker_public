# jobops licence posture memo (JSK-T096, FR-061, FR-062)

For the operator's WRITTEN acceptance. Status: **OPEN. Not accepted.** Legal review: **NOT PERFORMED**
(no counsel was consulted; nothing here is legal advice).

## Facts read from the v0.14.0 tree (commit 7fac74fc709a, 2026-11-24)

* `LICENSE` (sha256 `6973934bf3a41172ab39d7ec94aa4d41417dd36bc92ad6e321567a2bfd47ef82`, from `git clone --branch v0.14.0`, commit 7fac74fc709aa64ba7b590042757101e39c5ec60, 2026-11-24) is "Commons Clause" License Condition v1.0 on top of
  GNU Affero General Public License v3.0, licensor "JobOps contributors".
* Commons Clause removes the right to "Sell" the Software: providing to third parties, for a fee or other
  consideration (including hosting or support fees), a product or service whose value derives entirely or
  substantially from the Software.
* AGPLv3 section 13: users interacting with a MODIFIED version over a network must be offered the source.

## Posture job_seeker commits to

| Property | Commitment | How it is held |
|---|---|---|
| Private | Used by the single operator on their own host; never offered to third parties | sidecar bound to 127.0.0.1 / private network, no public route |
| Unmodified | No fork, patch, vendoring or linking; upstream image or submodule at the pinned tag only | image pinned by tag v0.14.0; the digest is recorded in the T097 spike evidence; `submodules/job-ops` never edited (FR-062); adapter talks HTTP only |
| Unsold | job_seeker is not offered for a fee or other consideration with jobops as a feature | the adapter is optional, NO-GO or absent leaves the core complete (FR-066) |

Under that posture no AGPL section 13 source-offer duty arises (no modified version, no third-party users)
and the Commons Clause "Sell" condition is not triggered. That is the author's reading, UNCONFIRMED by counsel.

## What the operator is asked to accept (written, in a commit or decision record)

1. Use of jobops v0.14.0 only under the three properties above.
2. Awareness that any future paid, hosted or multi-user offering of job_seeker that includes jobops would
   need counsel review first and is out of scope.
3. Awareness that legal review has not been performed.

## Acceptance record

| Field | Value |
|---|---|
| Accepted by | OPEN |
| Date | OPEN |
| Medium | OPEN |

Consequence while OPEN: criterion (a) of the GO criteria is unmet, so the GO decision is conditional (see
`DECISION.md`). Adapter tasks marked GO only stay blocked until this table is filled by the operator.
