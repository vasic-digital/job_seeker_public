# JSK-T177b approval-screen knockout checklist: evidence

- `red.txt`: RED before code (the console had no knockout rows and no Knockout page field).
- `e2e-journey.txt`: real jobseekerd + real Chromium + virtual passkey (tests/e2e/knockout_live_test.go, knockout_live.cjs), DRY-RUN only, nothing sent. Synthetic postings and a synthetic overlay (work_auth_countries: DE); no operator data.
- `knockout-*.png`: unfavourable (light, dark, mobile), acknowledged, missing, confirmed, Arabic (RTL) and Serbian renderings.
- `mutations.txt`: three new mutation rows, all KILLED.
- i18n: translator for the 10 new keys is the Claude translator (the HelixTranslate pipeline cannot run on this host: translate-pipeline.sh exits rc 2, HT_FLEET undeclared, no engine image); each language has an independent delta review (`evidence/i18n/<lang>/verdict.delta.json`, reviewer differs from every translator provider).
- Dry-run records the advisory row `warned_knockout` regardless of ticks (design of T177); the acknowledgement is recorded only on a live approval (`knockout_checked`), which is covered by internal/draft unit tests.
