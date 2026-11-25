# JSK-T179 roadmap-item correction

Task JSK-T179 (OCR consistency check, US3, FR-108) belongs to epic **JSK-005** (Applications and tracker): its line in
`specs/001-job-seeker-mvp/tasks.md` cites JSK-005 and `docs/workable-items/Issues.md` assigns US3 to JSK-005.

Commit `7ffb316` ("fix(evidence): regenerate JSK-T179 evidence with redacted contact reasons", merged in PR #213) carries the
trailer `Roadmap-Item: JSK-010` (Test hardening). That trailer is wrong for this task. Other T179 commits (for example `6112edd`)
carry `Roadmap-Item: JSK-005`. Git history is not rewritten (section 11.4.113); this file is the fix-forward record.
