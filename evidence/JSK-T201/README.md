# JSK-T201 Hacker News "Who is hiring" adapter (Algolia)
- red.txt RED first (undefined hnOne); green.txt GREEN under -race; FuzzHNOne 20 s no findings.
- Labelled gold set: internal/providers/testdata/hn_gold.tsv (8 hire rows, 4 skip rows) hand-labelled from 30 real captured comments, then checked against the parser (not the reverse). The hand labels exposed two parser faults before merge (angle-bracket salary bound; mode rule).
- Injection corpus: 6 hostile comment bodies in TestHNInjectionCorpus; userinfo/javascript links never become ApplyURL.
- Mutation pairs jsk016-hn-* (6) KILLED; three first SURVIVED and were closed with TestHNHeaderShapeGuards / TestHNCursorBoundMakesNoRequest.
- Live: TWO reads (thread discovery, comments page 0), both 200 via the production egress client; provenance files in testdata. Stated exception to one-read-per-provider (two distinct endpoints). Thread: "Ask HN: Who is hiring? (November 2026)" id 50338728, 378 comments.
- Parser limits (honest): only the "Company | Role | Location | ..." header convention (about 45 of the first 100 comments were top-level; the free-form ones are skipped, counted by class). Replies never ingested. Salary only from a currency-bearing header segment without angle brackets.
- Apply: handoff only (first https link or the comment page). Rate: no official limit documented (UNCONFIRMED); adapter uses 6h / 4 per day / <=6 requests per cycle.
