# testdata provenance

`testdata/*_sample.json` are record-level samples captured from the three live provider APIs on
2026-11-23 (umbrella `docs/plans/job-seeker-research/samples/`, descriptions truncated by the capturer,
no personal data). They are used ONLY as INPUT to the translators in determinism and parsing unit tests
(label: `fixture-input`, section 11.4.289(C)(3)). They are never evidence that a provider works: that is
the live contract test (`live_test.go`, `JS_LIVE=1`) and the `provider_call` rows of the ingest evidence.
The `httptest` TLS server in `helpers_test.go` is test-tool logic (HTTP framing, limits, headers); its
verdicts are labelled `tool-logic unit test`, not provider evidence.
