# JSK-T239 wiring evidence (2026-11-24)

* red-before-wiring.txt, red-ingest.txt, red-draft.txt, red-jsctl.txt: the tests written first, failing (compile or behaviour) before the code.
* green.txt: the same packages green after the wiring.
* mutations.txt: 14 `wire-*` rows of tests/mutations.tsv, all KILLED (the first run had one SURVIVOR, wire-guard-panic-unrecovered; the test only asserted the verdict, it now asserts the plugin_panic reason, and the re-run is KILLED).
* live-robots.txt: `JS_LIVE=1` run of TestLiveRobotsThroughRealEgress, one real robots.txt fetch through the egress client. It shows the real provider_tos and robots gates composing, and one real finding: remotive.com robots.txt disallows /api/*.
