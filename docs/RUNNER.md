# Forcing the container runner (`--runner=NAME`)

job_seeker's container tooling (`tools/containers`, driven by `jsstack` and `gates/live_h3.sh`) follows the
ecosystem-wide contract of the Containers module, `submodules/containers/docs/RUNNER_FLAG.md`.

| | |
|---|---|
| Flag | `--runner=NAME` or `--runner NAME` (any `jsstack` subcommand, before or after it) |
| Env | `CONTAINERS_RUNNER=NAME` |
| Precedence | flag > env > default priority order |
| `NAME` | a runner the module supports (`podman`, `docker`, `nerdctl`, ... ; the list comes from `runtime.SupportedRunners()`), or `auto` |
| Unknown `NAME` | refused, exit 2, the message lists the supported runners |
| Forced runner not installed or not reachable | refused, exit 2. **Never** a silent fallback to another runner |

```bash
bin/jsstack probe --runner=podman                 # runtime=podman ... compose=available
bin/jsstack up -file deploy/compose.yml -container jobseeker-gate --runner=docker
bash gates/live_h3.sh --runner=podman             # forwards the choice to every jsstack call
make live-h3 RUNNER=podman                        # same, through make
```

Implementation: `jsstack` calls `runtime.ParseRunnerFlag` and `runtime.ResolveRunner(val, found, os.Getenv)`
once, then `runtime.SelectRuntime(ctx, choice)` for the runtime and `compose.NewDefaultOrchestratorForRunner`
for compose, so both layers honour the same runner. `gates/live_h3.sh` exports `CONTAINERS_RUNNER`, which
every `jsstack` it starts reads. job_seeker has no `start.sh`/`restart.sh`: `jsstack` and the live gate are
its launchers. The native `jobseekerd` server uses no container runtime and takes no such flag.
`jobseekerd -h|--help` prints usage and exits 0 before any configuration is read.

Exit codes of `jsstack`: 0 ok, 1 real failure, **2 COULD NOT DETERMINE** (never a pass). `up` and `down` check
the compose file's required variables (`${VAR:?msg}`) first; when any is unset or empty they print
`jsstack: COULD NOT DETERMINE: <up|down> needs these variables set for <file>: NAMES` and exit 2, instead of
letting the compose front-end print a Python traceback with exit 1.

Proof (real host, no stubs): `go test ./cmd/jsstack/` in `tools/containers` and `bash gates/verify_runner_flag.sh`
(`--prove-failure` for the paired mutations). On a host with one runner installed the gate forces an installed
runner (must answer as that runner) and an absent one (must be refused with exit 2).

Containers pin: `submodules/containers` is at `2d18551c6c99` (the commit that adds this API); see
`docs/decisions/0007-containers-pin-forced-runner.md`.
