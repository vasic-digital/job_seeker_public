# 0002 Constitution resolution and carrier lockstep

| | |
|---|---|
| Status | ACCEPTED |
| Task | JSK-T004 |

Inside the umbrella (`vasic/job_seeker`) `find_constitution.sh` resolves `submodules/constitution` by walking
up from the working directory; measured: exit 0 from the umbrella checkout, exit 1 from `/tmp`.
No constitution submodule is vendored here (plan Complexity Tracking row; decision: not added).
Standalone fallback: export `CONSTITUTION_DIR=<absolute path to a Helix Constitution checkout>` (honoured by
`find_constitution.sh` Phase 0 and by `gates/verify_carriers.sh`). With neither, the gate exits 2
(UNDETERMINED), never a pass. `gates/verify_carriers.sh` asserts the four carriers stay in lockstep (section 11.4.157).
