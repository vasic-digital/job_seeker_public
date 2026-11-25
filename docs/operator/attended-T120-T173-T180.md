# Attended runs: T120, T173, T180 (operator runbook)

Mode chosen by the operator: **agents prepare, you approve.** Everything an agent can do with local tools was done on
2026-11-24 and is recorded under `evidence/`; this page lists only what is left for a human. No task is ticked
by this page: a task is ticked only after the evidence named below exists in the repository.

Evidence rules (`evidence/README.md`): hashes, counts and command output only. Never place a CV, contact detail,
credential, recovery token or real application content in `evidence/`.

---

## JSK-T120: human-attended live handoff acceptance run

Why it cannot be automated: FR-026 condition (4) needs a passkey step-up from a REAL authenticator. Every automated
journey uses a CDP virtual authenticator, an emulated device (spec: "the real-authenticator ceremony happens only in
the attended acceptance run"). The ledger allows exactly one `operator_attended` row (`gates/verify_ledger.sh`).

### Prepared by the agent (unattended, real)
- The full live journey with the virtual authenticator against a real `jobseekerd` over TLS: `evidence/JSK-T120/unattended/`
  (`go-test.txt` is the `go test ./tests/e2e/ -run TestConsoleJourney` output, plus the journey log and artefacts). It covers
  step-up, live mode, typed company confirmation, kill-switch refusal at handoff, `handoff-complete` and state `sent`.
  See the section "Result of the unattended run" at the bottom for the pass/fail line of this session.
- Precondition checks you can re-run: `make gates` and `jsctl verify-audit`.

### What you need
- A real authenticator: a hardware security key (FIDO2) or your device's platform authenticator, and ideally a SECOND one.
- A real posting you actually intend to apply to, from a provider that has no apply API (handoff mode), with prepared CV
  and cover slot. A real application will be handed to the provider; if you do not want that, pick a posting you would
  genuinely apply to or stop before step 9 and record the run as PARTIAL (do not tick).
- Secrets exported by NAME from your own shell profile: `JOBSEEKER_DB_KEY`, `JOBSEEKER_SESSION_KEY`, `JOBSEEKER_BOOTSTRAP_TOKEN`
  (and `JOBSEEKER_DB_KEY_FILE` if you use a file). Never type them into a file in this repository.

### Steps (you perform; expected result after each)
1. `export JOBSEEKER_ENV=production JOBSEEKER_DATA_DIR=<outside any git tree> JOBSEEKER_CERT_DIR=<dir> JOBSEEKER_ALLOWED_HOSTS=<host> JOBSEEKER_ORIGINS=https://<host>:<port> JOBSEEKER_HTTPS_ADDR=<ip>:<port> CAREER_PRIVATE_DIR=<overlay checkout> JOBSEEKER_TEMPLATE_DIR=<unsealed templates>`.
   Expected: no output. Pick a free port yourself (`ss -ltn`); 8080 and 8443 are in use on this host.
2. `jsctl cert gen-ca --hosts <host>` then `jsctl cert issue --hosts <host>` then `jsctl cert check`.
   Expected: CA created, server certificate issued, check reports valid. Trust the CA in the browser you will use.
3. `jsctl migrate`. Expected: a verified hardlinked backup is taken first, then migrations applied; exit 0.
4. `jsctl ext terms list`; for the provider you will use, after reading its current legal notice: `jsctl ext terms accept --provider <name>`.
   Expected: provider shows REVIEWED. (Prepare and Approve are blocked otherwise.)
5. `jobseekerd`. Expected: capability matrix printed (names only) and the line `jobseekerd: serving`; non-zero exit means a violated invariant, stop and record it.
6. In the browser open `https://<host>:<port>/login`, register the first passkey with the bootstrap token using your REAL authenticator,
   register a SECOND authenticator, sign out and sign in again. Expected: `/login` shows passkey status `ok`.
   Record: authenticator make/model and transport (not any key material).
7. Settings: turn live mode ON. Expected: a passkey prompt on your real authenticator (step-up), then live state shows On.
8. Applications: open your chosen posting, Shortlist, Prepare, check the shown SHA-256 of CV and cover, then Approve (live):
   type the company name exactly, complete the step-up on the real authenticator within 5 minutes.
   Expected: state `live_approved`. Negative check worth doing once: wait over 5 minutes before step 8's step-up and confirm approval is refused (no step-up).
9. Use the opened provider apply URL, submit the application at the provider, copy the confirmation reference.
10. In the console press "Record as sent" and enter the reference. Expected: state `sent`, audit rows `live_approved` and `handoff_recorded`.
11. `jsctl verify-audit --anchor`. Expected: exit 0, chain intact.
12. Turn live mode OFF in Settings (step-up again) and stop `jobseekerd`.

### Evidence you produce and where
Create `evidence/JSK-T120/attended/` containing:
- `run.txt`: date, host class, authenticator type (hardware key or platform), number of authenticators registered, the exact outputs of steps 3, 5, 11 (no secrets), the final application state, and the provider name.
- `audit-excerpt.txt`: from `/audit.jsonl` only the rows for this application (flag ids and hashes only, no content).
- `reference.sha256`: SHA-256 of the confirmation reference (never the reference itself if the provider treats it as private).
- Optional screenshots with personal data blurred or omitted.
Then tell the agent: it adds the single ledger row `001-job-seeker-mvp	e2e	SKIP		operator_attended	operator_attended live handoff acceptance run` to `tests/ledger.tsv`, runs `bash gates/verify_ledger.sh` and ticks T120 in a PR (the attended row is reported separately, never counted as an automated PASS).

---

## JSK-T173: CV page budget headroom (operator decision first)

Why it cannot be automated: whether to compress to 2 pages is a content decision (Q-ATS-4), and the 2-page norm for this seniority is UNCONFIRMED.

### Prepared by the agent (unattended, real): `evidence/JSK-T173/measurement.txt`
Measured over the 150 CV and 150 cover PDFs of the umbrella `.cache-career`: every CV is exactly 3 pages (gate limit 3, zero headroom), every cover 1 page.
Page 3 holds 3 to 32 percent of the extracted text (median 15 percent); for the `go` area, 9 percent (hi) to 24 percent (ru, kk, ja, sr).
So a 2-page target needs a cut of roughly that share, or a tighter layout, per locale.

### Steps (you perform)
1. Decide: (a) keep 3 pages, adding a gate warning when a CV reaches the limit with no headroom; or (b) target 2 pages for the classes whose page-3 share is at most 15 percent, and keep 3 for the rest; or (c) 2 pages for all (content cut about 25 to 30 percent in the heaviest locales).
2. Write the decision into `docs/decisions/` (next free number) as a file stating the option, the page budget per page-class, and the date; or reply to the agent with the option letter and it writes the decision file.
3. Expected outcome of the decision: the agent implements the gate in the umbrella (`_tools/career/gates.py` `_PAGE_LIMITS`, plus a page table) with a paired mutation, and re-measures. Evidence then: `evidence/JSK-T173/` gets `red.txt`, `green.txt`, `pages-after.txt`.
Do not tick T173 on the decision alone.

---

## JSK-T180: Hebrew locale decision and RTL build

Why it cannot be automated: adding Hebrew is a product decision (Q-ATS-7: add `he` or drop it from the directive's RTL list), and the Hebrew CV and cover content must be translated and reviewed by someone who reads Hebrew.

### Prepared by the agent (unattended, real): `evidence/JSK-T180/rtl-probe.txt`
- WeasyPrint 67.0 renders Hebrew RTL with the installed `Noto Sans Hebrew` (fonts embedded) and the text layer extracts in logical order (synthetic text only).
- Not changed (waiting on your decision): `career/catalog.py` (`LOCALES`, `RTL_LOCALES`), `render/__init__.py` (`HTML_LOCALES`), `render/fonts.py` (`EXTRA["he"]`), `gates.py` (script range 0590-05FF, `_RTL_LANGS`), the `he` locale content and tests.

### Steps (you perform)
1. Decide: add `he` (16th locale) or drop Hebrew from the RTL list. Reply with the decision or write it to `docs/decisions/`.
2. If `he`: name the reviewer who reads Hebrew and who will check the translation (HelixTranslate output needs an independent per-language review, §11.4.256).
3. Expected outcome after your decision: the agent wires the five code points above, translates via the canonical pipeline, builds `cv-<area>-he.pdf` and `cover-<area>-he.pdf` through WeasyPrint, runs `gates.validate_pdf` (tagging, script range, logical order), and records `red.txt`, `green.txt` and a `he` page table in `evidence/JSK-T180/`.
4. Your attended part after the build: open one Hebrew CV in a PDF viewer and confirm text direction, punctuation placement and digits; record the confirmation (date, reader, viewer, yes or no) in `evidence/JSK-T180/hebrew-read-check.txt`.
Do not tick T180 until the build, gate output and your read check exist. If you drop Hebrew, the evidence is the decision file plus the updated directive text, and the task is closed as WON'T-DO by that decision.

---

## Result of the unattended run (2026-11-24, `go test ./tests/e2e/ -run TestConsoleJourney`, base 53d1d57)

`evidence/JSK-T120/unattended/go-test.txt`: 124 journey steps PASS and 0 steps FAIL, covering the live journey with the virtual authenticator
(step-up, live approval, kill-switch refusal at handoff, `sent`), the FR-026 bypass codes, accessibility sweeps and the audit export.
The test as a whole is **FAIL (exit 2)**: after the live journey, the no-JavaScript approval path timed out in Playwright on
`click('text=Prepare template draft')` ("element is not stable", `tests/e2e/journey.cjs:332`). The cause was not investigated in this task
(UNCONFIRMED: flake, animation or regression) and nothing was changed for it. It is outside what T120 needs, but it means the
automated journey is not green at this base: re-run it before the attended run (`E2E_NODE_MODULES=<_tests/node_modules> E2E_EVIDENCE_DIR=<dir> go test -count=1 -v ./tests/e2e/ -run TestConsoleJourney`, about 6.5 minutes) and tell the agent if it fails again.

## Decisions taken 2026-11-24 (T173, T180)

* T173: option (a), ADR `docs/decisions/0009-cv-page-budget-keep-three-with-headroom-warning.md`; implemented and evidenced in `evidence/JSK-T173/`.
* T180: Hebrew dropped from the RTL list, ADR `docs/decisions/0010-hebrew-dropped-from-rtl-list.md`; closed as WON'T-DO, evidence `evidence/JSK-T180/green.txt`. The attended steps above for these two tasks are void.
