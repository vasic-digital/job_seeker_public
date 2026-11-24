# jobops GO / NO-GO decision record (JSK-T098, FR-061, FR-066, Roadmap-Item JSK-008)

Date 2026-11-24. Evidence: `SPIKE-RESULTS.md` (measured), `LICENCE-POSTURE.md`, `SPIKE-PLAN.md`.

## Verdict: CONDITIONAL. NOT a plain GO. Open conditions: a, b (disk), d (validator), e (egress), f (defaults)

R-08 rule: any NO on (a)-(d) is NO-GO. Nothing here is a NO; several criteria are not yet fully met, so the
decision stays conditional and the [GO only] tasks do not start. No criterion below is claimed beyond its evidence.

| Criterion | Status | Evidence (`SPIKE-RESULTS.md`, raw output in `evidence/spike-2026-11-24.txt`) |
|---|---|---|
| a Licence accepted in writing | **OPEN** | `LICENCE-POSTURE.md` acceptance table is empty; only the operator can fill it |
| b Image and RAM within section 12.6 | RAM MET (about 0.37 GB of 31 GB); **disk OPEN**: image 6.05 GB, host filesystem 100 percent used, 4.3 GB free after the pull | `podman stats`, `df -h` |
| c Rootless podman via submodules/containers | MET | `jsstack up/down` rc 0, rootless=true |
| d Read calls live and a schema validator accepts them | **PARTIAL**: calls answer 200 and shapes were recorded; the strict validator is T100, and the version check is UNCONFIRMED | probe output |
| e Telemetry blockable by egress policy | **PARTIAL**: disabled by configuration (no umami/openpanel traffic seen, `/stats` 404); an egress-policy block was NOT tested; other outbound traffic exists (sponsor lists) | logs, curl |
| f Risky scrapers OFF | **UNCONFIRMED**: no pipeline run was triggered and none ran on its own; per-extractor defaults were not enumerated | `pipeline/status` |

## Consequences

* [GO only] tasks T099-T102 are NOT started and stay blocked until the operator fills the acceptance table
  in `LICENCE-POSTURE.md` and confirms the disk cost of a 6 GB image. If the operator declines, T099-T102 and
  FR-062..FR-065 are N/A-by-decision and the core is unaffected.
* T103 (NoOp, always built) is done; the core never depends on jobops (FR-066).
* Open items carried into T100/T101: no version endpoint found, egress profile for sponsor downloads, first
  user seeding needs an empty data directory, auth is JWT login.
