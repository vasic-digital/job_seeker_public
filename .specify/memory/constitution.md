---
version: 1.0.0
ratified: '2026-11-24'
last_updated: '2026-11-24'
---

# job_seeker Constitution

This project constitution EXTENDS the universal Helix Constitution (resolved at run time with
`find_constitution.sh`; canonical name `constitution/CLAUDE.md`, see this repository's `CLAUDE.md`).
It never weakens or overrides an inherited clause. Where the two disagree, the Helix Constitution wins.

## Principles

1. **No personal data in git.** Personal values live only in the private overlay
   (`vasic-digital/career_private`, read through `CAREER_PRIVATE_DIR`). A repo gate fails on any
   overlay value in a tracked file, the DB schema defaults, fixtures or logs (CONST-042 / §11.4.10).
2. **Human in the loop; dry-run default.** No unattended submit path exists (spec FR-024 to FR-036).
3. **Anti-bluff.** Every PASS carries captured runtime evidence; outside unit tests no stubs, mocks or
   fixture servers are evidence (§11.4, §11.4.289). An absent real dependency is an honest SKIP.
4. **All 13 test types of §11.4.169** are planned per feature, with an honest SKIP-with-reason as the
   only permitted absence.
5. **Reproduce first** (§11.4.292); **scope discipline**: every commit names the workable item
   (`JSK-NNN`) it serves (§11.4.293).
6. **Containers only through `submodules/containers`**, rootless (§11.4.76, §11.4.161).
7. **Hosted-Jev-only decision layer** per the umbrella override
   `docs/governance/OVERRIDE-JEV-HOSTED-ONLY.md`; the key is read by env-var NAME, never printed.
8. **Third-party code stays unmodified** (jobops: AGPLv3 + Commons Clause, separate process only).
9. **UI strings** pass the HelixTranslate pipeline and an independent per-language review (§11.4.255,
   §11.4.256); UI work follows §11.4.162 and §11.4.216 to §11.4.221.
10. **Never force-push; all work lands on `main` (§11.4.113, §11.4.288); independent review of every
    change (§11.4.142).**
