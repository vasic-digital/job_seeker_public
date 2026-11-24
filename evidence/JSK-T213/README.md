# JSK-T213 (PARTIAL) schema.org JobPosting JSON-LD reader
- Scope done: the read-only parser ParseJobPostingJSONLD (internal/providers/jsonld.go). NOT done from T213: the generic RSS/Atom adapter, per-host robots/ToS check, any fetch path/wiring. Task NOT ticked.
- red.txt RED first; green.txt GREEN under -race; FuzzJSONLD 20 s no findings; mutation pairs jsk016-jsonld-* (5) KILLED.
- Fixture is DOC-DERIVED (Google job-posting structured-data docs, schema.org), placeholders only; NO live read was made (no allow-listed career host exists and none is invented).
- Safety: no network access in the reader; page and posting URLs must be https without userinfo; block count (50), block size (1 MiB) and nesting depth (8) bounded; salary bounded; text sanitised downstream by Finalize; apply = handoff to the posting URL.
