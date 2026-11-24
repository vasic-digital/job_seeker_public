# JSK-T197 Greenhouse adapter + watchlist (PARTIAL)
- red.txt: RED (tests written first; package did not compile: undefined NewGreenhouse).
- green.txt: GREEN under -race. Fuzz 15 s each (FuzzGreenhouseOne 708k execs, FuzzLoadWatchlist 86k execs), no findings.
- Mutation pairs jsk016-slug-regex, jsk016-gh-unescape, jsk016-gh-https-only: KILLED (tests/mutations.tsv).
- Live: ONE read, GET boards-api.greenhouse.io/v1/boards/vaulttec/jobs?content=true (the docs' sample absolute_url board) -> HTTP 404, TLS *.greenhouse.io, ip 13.32.12.41 (internal/providers/testdata/greenhouse_vaulttec.provenance.json). No second read made. Golden is DOC-DERIVED; live shape UNCONFIRMED.
- Egress: boards-api.greenhouse.io added to deploy/egress-allowlist.txt with reason.
- No automated applying: apply = handoff to absolute_url.
