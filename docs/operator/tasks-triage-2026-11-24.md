# specs/001-job-seeker-mvp/tasks.md triage, 2026-11-24

Measured against github/main `53d1d57` (the baseline; later merges, e.g. #165, only add PARTIAL text to T217 and do not tick anything): **138 unticked tasks** (`grep -c '^- \[ \]'`). Status vocabulary: **BLOCKED-OP** (needs an operator or counsel decision/action), **BLOCKED-TASK** (waits for another task), **NOW** (achievable now with what is on this host), **AWAITING-TRANSLATIONS** (waits for catalogues from the translation stream), **OTHER-AGENT** (owned by another stream right now, excluded from this triage's work), **PARTIAL** (part shipped; the named remainder is what is left). Re-derive, never quote: this is a dated reading.

## A. Excluded because another stream owns them now

T168, T169, T171, T173, T178, T179, T180 (gate agent); T120 (attended runbook); T177, T244 (overlay agent); T187 leftovers (no unticked T187 line exists at this commit); T129..T157 (translation stream: 12 translate + 15-language review tasks and the screenshot task T157); red-test fixes (no task ids).

## B. MVP phases 1-12

| Task | Status | What is left / evidence needed |
|---|---|---|
| T005 | BLOCKED-OP | Install the hooks into the shared git dir (`scripts/install-hooks.sh`); evidence = hook refusing a commit without a `Roadmap-Item:` trailer in the real git dir. |
| T040 | BLOCKED-OP | Operator accepts the Himalayas ToS and decides on a key; then `JS_HIMALAYAS_OK=1` live fetch evidence. |
| T045 | BLOCKED-TASK | Provider-health console display (Phase 5 console work); evidence = rendered frame + test. |
| T048 | BLOCKED-OP / time | 24 h observer ends 2026-11-25T10:28:07Z (run the query in `evidence/JSK-T048/observer-start.txt`); the Himalayas leg waits on T040. |
| T053 | BLOCKED-OP | RU/SR/DE real POSTING corpus is operator-supplied; measurement on it. |
| T057 | BLOCKED-OP | Operator confirmation of the agent-made labels; >= 30 gold per area and RU/SR corpus operator-supplied. |
| T058 | BLOCKED-OP | Per-area SC-003 thresholds need >= 30 gold per area (same corpus gap as T057). |
| T059 | BLOCKED-TASK | Store-level decrypted-row determinism comparison; T055 is now ticked, so this is NOW if the three-run comparison is extended to the store (small). |
| T069 | NOW | 20 random audit tamper trials (delete-and-recompute-forward, reorder, tail truncation) + 20 `kill -9` mid-transition runs; no evidence directory exists yet. |
| T083 | PARTIAL / BLOCKED-OP | RU/SR branches need the RU/SR posting corpus; rest of the journey is shipped. |
| T084 | BLOCKED (upstream) | Needs an autonomous HelixQA session over real frames; `helixqa autonomous` is blocked upstream by TLS/blank-frame (evidence/JSK-T111/autonomous). Not fixable in this repo. |
| T087 | NOW | Approval-engine-level concurrent-approval race; T064 (the engine) is ticked, so the stated blocker is gone. |
| T088 | BLOCKED-OP | A real `overlay.yaml` on the host and the T031 image export via `PDGATE_EXTRA`. |
| T092 | NOW | Restore drill into a scratch CONTAINER through `submodules/containers` (podman present). |
| T099-T102 | BLOCKED-OP | `[GO only]`: jobops adoption (pin v0.14.0, sidecar) needs the operator GO from the T095-T098 spike; T101/T102 also need T099/T100. |
| T108 | PARTIAL | See the task text; remaining items depend on live discovery (T040/T048). |
| T109 | PARTIAL | `make full` legs exist; remaining legs listed in the task text need live providers. |
| T111 | PARTIAL / BLOCKED (upstream) | Same upstream HelixQA autonomous blocker as T084. |
| T112 | BLOCKED-TASK | Outbound politeness meter needs a live discovery path (T040/T048). |
| T114 | PARTIAL | Remaining chaos classes named in the task text. |
| T120 | OTHER-AGENT | Attended runbook. |
| T121 | PARTIAL-NOW | Operator guide and quickstart re-verified against the BUILT artefact; the artefact build is gated by T123. Doc re-verification part is NOW. |
| T122 | BLOCKED-TASK | Whole-MVP independent review needs the other MVP tasks closed first. |
| T123 | BLOCKED-OP | Merge/tag; first build is BLOCKED-NO-BASELINE until the operator records how a first baseline is accepted (Q12). |
| T126 | AWAITING-TRANSLATIONS | ar/fa RTL layer is built; real RTL frames need ar/fa catalogues. |
| T127 | NOW | Source licensed open fonts (Noto families) for Cyrillic, zh, ja, ko, hi, ar, fa; record licence + hashes. Needs network download. |
| T158 | AWAITING-TRANSLATIONS | e2e in every language. |
| T159 | BLOCKED-TASK | Phase 11 acceptance needs T129..T158. |
| T160 | PARTIAL | job_seeker half done; the umbrella half is outside this repository. |
| T200 | PARTIAL-NOW | Console paste surface exists (merged PR #151); still open: ATS-resolves-by-id single-job public read and `manual:<host>` provider naming. |
| T202 | BLOCKED-OP | Bundesagentur key/terms: two live reads returned HTTP 403. |
| T207 | AWAITING-TRANSLATIONS | Only non-en catalogues remain. |
| T211 | BLOCKED-OP | Adzuna terms and key. |
| T215 | BLOCKED-OP | hh.ru app registration and developer agreement. |
| T217 | PARTIAL (PR #165 merged, evidence/JSK-T217 is on main) | Done for en and ru, light and dark, 4 pages, vision-verified; 13 languages and RTL (ar, fa) AWAIT-TRANSLATIONS. Not ticked. |
| T219 | BLOCKED-TASK | Acceptance review is being written as a separate PR (docs/operator/provider-expansion-acceptance-2026-11-24.md, not on main yet); expected verdict NOT READY while T200/T202/T207/T211/T215/T217 are open. |
| T242 | BLOCKED-OP | Operator-approved overlay schema change (work-auth and language keys). |
| T244 | OTHER-AGENT | Extension acceptance. |

## C. Product programme, Phase 14 (T400..T523)

All of these are BLOCKED-OP as a block: the tasks file says nothing in Phase 14 starts before the operator accepts `docs/product/09` section 9 (decision D-SCOPE-1, amendment PR #2). After acceptance, the first NOW candidates would be: T401, T403, T410, T412, T413, T425, T427, T435 (software, no outside input). Individually blocked on outside input even after acceptance: T400, T402, T417, T481, T493, T506 (`[OP]`), T404, T405, T428 (`[RESEARCH]`, need cited primary sources). T461 needs the library/licence choice first; T466, T505 are spikes. T411 waits for T410; T414 for T413; T437 for T436; T441 for T435; T451..T453 for T450; T464 for T463; T477 for T438; T491/T492 for T490; T501..T504 for T500; T520..T523 for the earlier stages. Individual rows are not repeated here: each follows from its epic and the dependencies in `docs/product/09`.

## D. Summary

NOW: T069, T087, T092 (container drill), T127, T059, T121 (doc part), T200 (remainder). BLOCKED-OP is the largest class (provider keys/terms, corpus, jobops GO, Phase 14 acceptance, Q12). AWAITING-TRANSLATIONS: T126, T158, T207, T217 remainder.
