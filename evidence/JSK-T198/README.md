# JSK-T198 Lever adapter
- red.txt RED first; green.txt GREEN under -race; FuzzLeverOne 15 s no findings; mutation pairs jsk016-lever-uuid / -https-only / -salary-sane KILLED.
- Live: ONE read through the production egress client of api.lever.co/v0/postings/leverdemo (docs' example site): 200, 10 records; golden = first 4 raw records (testdata/lever_leverdemo_golden.json + provenance).
- Not run: a second live read through the adapter itself (one-read budget). The adapter translates the captured payload; contract live_test is not added for Lever.
- Apply: handoff only (applyUrl). EU host unsupported by design.
