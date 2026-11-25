# Running HelixQA against job_seeker (JSK-T111)

The HelixQA dependency (manifest name `qa`, resolved by `scripts/resolve_deps.sh`) is HelixQA. Its `go.mod` replaces eight sibling modules with `../<name>`, so build it from a scratch
directory where they sit side by side (do not add them to this repository):

    mkdir -p "$S/hq" && cd "$S/hq"
    git clone --depth 1 git@github.com:vasic-digital/HelixQA.git qa        # or use the resolved checkout without .git
    git clone --depth 1 git@github.com:vasic-digital/Challenges.git challenges
    git clone --depth 1 git@github.com:vasic-digital/Containers.git containers
    git clone --depth 1 git@github.com:vasic-digital/security.git security
    git clone --depth 1 git@github.com:vasic-digital/LLMsVerifier.git llms_verifier
    git clone --depth 1 git@github.com:HelixDevelopment/DocProcessor.git doc_processor
    git clone --depth 1 git@github.com:HelixDevelopment/LLMOrchestrator.git llm_orchestrator
    git clone --depth 1 git@github.com:HelixDevelopment/LLMProvider.git llm_provider
    git clone --depth 1 git@github.com:HelixDevelopment/VisionEngine.git vision_engine
    (cd qa && go build -o "$S/bin/helixqa" ./cmd/helixqa)

Legs:

- No vision model needed: `JSK_HELIXQA_BIN=$S/bin/helixqa go test ./tests/live -run HelixQA -v` boots the real daemon
  (host `localhost`, private CA, trusted through `SSL_CERT_FILE`) and runs `qa/banks/job_seeker_console.yaml` with
  `helixqa http`. A paired test requires the same bank to FAIL against a server that answers 200 to everything.
- Vision / autonomous session: needs `HELIX_VISION_ENDPOINT` (a keyless OpenAI-compatible vision endpoint, optional
  `HELIX_VISION_MODEL`). A local keyless endpoint is provided by `scripts/vision-ollama.sh` (see docs/operator/vision-qa.md); the per-frame vision analyzer
  is proved there. `helixqa autonomous` was run and is BLOCKED (private-CA TLS makes every bridge screenshot blank; see docs/operator/vision-qa.md).

## Authenticated and state-changing sessions (JSK-T084 / JSK-T111)

Both sessions boot the REAL `jobseekerd` (host `localhost`, private CA, SYNTHETIC rows, dry-run, the real robots gate against a loopback
`robots.txt`, provider terms accepted through `jsctl`), sign in with a CDP virtual authenticator (`scripts/qa-signin.cjs`) and hand
`helixqa autonomous` a 0600 cookie file (`-web-cookie-file`) and the private CA (`-web-ca-file`). Nothing is ever sent: the compliance
guard and the dry-run default are product invariants, and the read-back below asserts zero handoff rows and live mode off.

| Leg | Driver | Bank | Verdict |
|---|---|---|---|
| Navigation (8 cases) | `scripts/qa-helixqa-authenticated.sh` | `qa/web/console_authenticated.yaml` | `scripts/qa-helixqa-verify.py` |
| State-changing (5 cases: prepare draft, reject with reason, dry-run approve, map queue posting, dismiss) | `JSK_QA_STAGE=1 go test ./tests/e2e -run TestHelixQAAuthenticated` with `JSK_QA_BANKS=qa/web-state` | `qa/web-state/console_state_changing.yaml` | `scripts/qa-state-verify.py` |
| Passkey step-up (kill switch off needs a fresh passkey check) | `JSK_QA_MODE=stepup go test ...` (runs `scripts/qa-stepup-flow.cjs`) | none (Playwright, see below) | `readback.json` + vision verdicts on its frames |

Two independent evidence sources are required for the state-changing leg. The vision model reads the exact after-action frame
(`helixqa-verify-vision` logic inside helixqa, strict token matching, frames saved with their sha256). The harness then stops the daemon
and reads its encrypted database (`tests/e2e/helixqa_stage_test.go`, `readback.json`): application states, approval and submission rows,
posting states, the audit transitions, and the audit hash-chain walk. `qa-state-verify.py` passes only when both agree; a model verdict
alone never passes, and neither does a database row alone.

Environment, by name: `JSK_HELIXQA_BIN` (built from HelixQA main at 3c6021a or later, which has `-web-video-dir`, PR #17), `E2E_NODE_MODULES`, `HELIX_VISION_ENDPOINT`
(loopback only), `HELIX_VISION_MODEL`, `JSK_QA_OUT`, `JSK_QA_BANKS`, `JSK_QA_STAGE=1`, `JSK_QA_VIDEO_DIR`, `JSK_QA_EXTRA`
(for example `-curiosity=false`). Build the HelixQA binary in place (`go build -o bin/helixqa ./cmd/helixqa` inside the qa checkout):
helixqa resolves its web bridge as `<binary dir>/../scripts/playwright-bridge.js` (or `scripts/` under the working directory) and an
out-of-tree binary reports every screenshot as blocked (`Cannot find module .../scripts/playwright-bridge.js`).

Video is window-scoped (the bridge's own page over CDP, `-web-video-dir`, not the host display) and stays out of git: it goes under the
storage-policy drives (`/mnt/DATA7TB/claude-work/qa-video/job_seeker/<run>/`) and only the manifest with sha256 is committed.

Why the step-up leg is separate: the HelixQA web bridge runs one short-lived node process per action, so no WebAuthn authenticator
survives between actions and a passkey ceremony cannot run inside a bank. A Playwright driver holds the CDP virtual authenticator for the
whole page life instead. A virtual authenticator is an emulated device (the same evidence class as T083); the real-authenticator ceremony
stays in T120.

Platform `api`: the api platform has no screen. `helixqa http` (bank `qa/banks/job_seeker_console.yaml`) is its meaningful leg, with wire
evidence; curiosity on it produced blank frames and is not run.

Honest limits (2026-11-25): the curiosity phase is still not run. `qwen2.5vl:7b` (pulled to `/mnt/DATA1TB`, loopback ollama) makes
HelixQA's plan phase time out on this host, and the 3B model's curiosity actions changed nothing in the console. The 3B model
sometimes returns a malformed blind reply on the dense inbox frame (the case is then BLOCKED, never passed) and misread a state
cell once on a correct frame, so the database read-back, not the model, is the strict evidence for state changes. The bridge keeps
its state in the fixed `/tmp/.helix-browser-state.json`, so run one HelixQA web session per host at a time.
