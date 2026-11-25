# ADR 0010: Hebrew is dropped from the RTL list; the shipped RTL locales are ar and fa (JSK-T180, Q-ATS-7)

* Status: accepted (2026-11-24, operator decision: drop Hebrew from the RTL list)

## Decision

* The directive's RTL list is **ar and fa**. No `he` locale is added: the locale set stays at 15.
* The wiring that the add-option would have changed is left as is, and already excludes Hebrew
  (umbrella `_tools/career/catalog.py` `RTL_LOCALES = {ar, fa}`, `gates.py` `_RTL_LANGS = {ar, fa}`; measured in
  `evidence/JSK-T180/green.txt`).
* `evidence/JSK-T180/rtl-probe.txt` is kept as the record that WeasyPrint 67.0 can render Hebrew RTL if the
  decision is ever reversed (synthetic text only).
* Gap G-23 of `docs/research/protocols/11-ats-gap-analysis.md` and question Q-ATS-7 are closed by this decision.

## Consequences

Reversing this needs a new ADR, a Hebrew translation reviewed by a Hebrew reader (section 11.4.256) and the five
code points listed in the probe note.
