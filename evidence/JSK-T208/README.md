# JSK-T208 draft-review duplicate warnings and acknowledgement flow (FR-089)
- draft.Service.Duplicates lists OTHER open applications to the same Position (states of gate G9d; no personal data). Console draft page shows a warning block and the ack_duplicate checkbox only when some exist; the acknowledgement is enforced by gate G9d at approval (T205/T206 B4), the view never grants anything.
- Tests: internal/draft/duplicates_test.go, internal/console TestDraftPageRendersDuplicateWarningAndAck (run.txt). 4 mutation pairs t208-* KILLED.
- Conservative reading: warning and checkbox shown in every apply mode whenever duplicates exist (in single modes the lock gate refuses anyway); en messages only (no other catalogue exists yet).
- Not done here: Playwright screenshots of the new block per language (T217).
