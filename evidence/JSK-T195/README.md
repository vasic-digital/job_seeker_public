# JSK-T195 evidence: labelled synthetic cross-provider pairs and edge cases (FR-083)

Label: **synthetic-pair**. Generator: `internal/position/synth_test.go`. Tool-logic proof only; never counted as recall evidence and never closes an SC.

- 7 safe perturbations (case, `(m/w/d)`, `- Remote` suffix, company legal form, whitespace/`Group`, `Sr.`, identity) x 8 fixtures = 56 pairs, plus 805 pairs
  re-emitting each of the 115 real corpus postings (full run, `synthetic-pairs.txt`): **all resolve to one Position**. The default test run uses a
  stride-10 subset (~20 s); `JS_SYNTH_FULL=1` runs all.
- 4 hard perturbations are MEASURED, not asserted (full run): drop last word auto 13 / queued 50 / new 52; swap first and last word auto 107 / new 8;
  one-character typo auto 107 / queued 8; location rewrite auto 115. The "new" cells are potential false splits (title shortened by a word), a recall
  limit that the review queue and the T193 body signature are meant to narrow; thresholds are uncalibrated.
- Section 4 edge cases: Cyrillic vs Latin company (transliteration), agency employer forced to the queue, human name alias (Facebook/Meta),
  expired listing with a live duplicate (Position stays open, representative moves; all expired -> expired), plus the T191 suite (location, two ATS ids,
  seniority, technology, relisting, missing company, cannot_link/must_link).
