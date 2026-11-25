# JSK-T200d: single-job ATS read

- red.txt: first RED (implementation absent, tests do not build).
- tests.txt: the tests that prove each safety property (private IP, mixed answer, DNS rebinding single lookup + pinned dial, redirects, robots, ToS, dry-run).
- mutations.txt: 11 `atsjob-*` mutation pairs, all KILLED.
- live-cli.txt: the real `jsctl` binary: non-ATS links rc 2; an ATS link with the provider terms unaccepted rc 1 (blocked before the posting request).

NOT produced: a live fetch of a real ATS posting. The guard requires the operator to accept the provider terms, which an agent must not do on the operator's behalf.
