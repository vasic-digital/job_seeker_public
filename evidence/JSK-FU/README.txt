JSK follow-ups from PR #45 review (items 1-4,6 here; item 5 re-record after merge).
red-scheduler.txt/green-scheduler.txt, red-pipeline.txt/green-pipeline.txt: RED then GREEN.
Limiter skew test (TestMinIntervalHoldsUnderBackwardClockStep) passed on first run: it documents existing correct behaviour (min interval measured from max(at)), no code change.
Chaos disk-full child: header comment corrected to state it uses direct SQL, not ingest.Runner.
