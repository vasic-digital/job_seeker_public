# Compiled sketch of the extension interfaces

Scratch Go module (`example.com/jsk-sketch`, no dependencies) proving that the interfaces in
[`../21-extension-interfaces.md`](../21-extension-interfaces.md) compile and that the conformance-suite
and mutation-pairing idea works. It is documentation, not production code: nothing under `internal/` imports it.

```bash
cd docs/research/protocols/appendix-compiled-sketch
go vet ./... && go test -count=1 ./...
```

Captured output of the last run: [`../appendix-sketch-output.txt`](../appendix-sketch-output.txt).
Reference plugins: `refs/jsonld` (StandardReader), `refs/remotemode` (EligibilityRule + mutant),
`refs/keyword` (ScoreFeature), `refs/handoff` (ApplicationChannel). Framework: `ext/` (verdicts, registry,
ports, conformance reporter).
