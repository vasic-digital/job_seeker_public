# ADR 0007: bump submodules/containers to the forced-runner API

* Status: accepted (2026-11-24, operator directive: every launcher accepts `--runner=NAME`)
* Supersedes the pin in ADR 0003 ("umbrella pin 22b49707f9bb, for parity") for this one submodule.

## Decision

`submodules/containers` moves `22b49707f9bb` -> `2d18551c6c99e21f4287be5b1d3c927ac3901d07`
(`vasic-digital/containers` PR #3, "forced runner selector"), which adds `runtime.ParseRunnerFlag`,
`runtime.ResolveRunner`, `runtime.SelectRuntime`, `compose.NewDefaultOrchestratorForRunner` and
`docs/RUNNER_FLAG.md`. The gitlink and `helix-deps.yaml` `deps[].ref` move in the same change
(`bash gates/verify_pins.sh`: 0).

## Consequence

The umbrella still pins `22b49707f9bb` until its own gitlink bump. The ADR 0003 "parity with the umbrella"
reasoning no longer holds for this submodule while the two differ; the pin is a strict fast-forward of the
old one (the old pin is an ancestor of the new one).
