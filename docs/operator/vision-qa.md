# Vision QA with a LOCAL vision model (JSK-T084 / JSK-T111, umbrella 11.4.278)

HelixQA's vision analyzer (`helixqa-verify-vision`, built from `qa ./cmd/helixqa-verify-vision`) sends a screenshot to an
OpenAI-compatible `/v1/chat/completions` endpoint and does STRICT fact matching on the model's reply (every `-expect` token must
appear, any `-forbid` token fails it). It sends no credential, so the endpoint must be keyless. This setup supplies one that runs on
the development host: no frame leaves the machine.

## Components

| What | Where |
|---|---|
| User-level ollama (separate from the system one on 11434; never modified) | `scripts/vision-ollama.sh`, port 11500 (`VISION_OLLAMA_PORT`), bound to 127.0.0.1 |
| Model store | `/mnt/DATA1TB/claude-work/ollama-models` (`VISION_OLLAMA_MODELS`), the FAST drive per `docs/storage-policy.md` of the umbrella |
| Server home (pid, log) | `/mnt/DATA1TB/claude-work/ollama-user` (`VISION_OLLAMA_HOME`) |
| Model | `qwen2.5vl:3b`, 3.2 GB on disk, about 4.8 GB VRAM on the RTX 3060 (measured 972 MiB -> 5746 MiB) |
| Session driver | `scripts/vision-qa.sh` (+ `vision-capture.cjs`, `vision-mutant-server.py`) |
| Endpoint wiring (env by name) | `HELIX_VISION_ENDPOINT=http://127.0.0.1:11500/v1/chat/completions`, `HELIX_VISION_MODEL=qwen2.5vl:3b` (`scripts/vision-ollama.sh env` prints them) |

No sudo and no systemd are involved. `scripts/vision-ollama.sh stop` only signals the pid it recorded, and only after checking that
the process is owned by the current user, is an `ollama serve`, and carries `OLLAMA_HOST=127.0.0.1:<our port>` in its environment
(11.4.174); otherwise it signals nothing and exits non-zero.

## Run it

    scripts/vision-ollama.sh start
    scripts/vision-ollama.sh pull            # once; resumable; about 3.2 GB
    export JSK_VERIFY_VISION_BIN=/path/to/helixqa-verify-vision      # see docs/operator/helixqa.md for the build
    export JSK_PLAYWRIGHT_NODE_MODULES=/path/to/node_modules         # a directory containing `playwright` (chromium installed)
    scripts/vision-qa.sh                     # writes evidence/JSK-T084 (override with OUT=...)
    scripts/vision-ollama.sh stop

Exit: 0 every verdict as expected, 1 a verdict was not as expected, 2 cannot determine (missing tool, local server down, endpoint
not local). The script refuses an endpoint that is not on 127.0.0.1/localhost.

## What the session does

1. Builds and boots the REAL `jobseekerd` (private CA, host `localhost`), screenshots `/login` with Chromium: `VQA-LIVE-LOGIN`.
2. Verifies three AUTHENTICATED console frames (inbox, draft review, settings) that the Playwright journey captured against the
   real daemon (`evidence/JSK-T083/zoom200-*.png`): `VQA-CONSOLE-*`. Logging in needs a passkey ceremony, which the e2e journey
   performs; this leg reuses its real frames rather than faking a login.
3. Paired mutation (section 1.1): a server answering HTTP 200 to every path, once with a bare "OK" page and once with a Go panic
   page served as 200, is screenshotted and checked against the SAME expectations as the login frame, with `-expect-fail`: the
   case succeeds only if the raw verdict is a fail. A status-code-only check would pass both servers.

## Evidence (2026-11-24, `evidence/JSK-T084/`, copy of the verdicts in `evidence/JSK-T111/vision-*`)

`summary.tsv` has six rows, all as expected (exit 0): four must-pass frames (raw `pass: true`, `hallucinated: false`) and two
mutants (raw `pass: false`, `matched_facts: 0`, so `case_result: true`). `frames/` holds the screenshots, `verdicts/` the verdict
JSON plus the model's full reply, `SHA256SUMS` covers everything, `MODEL.txt` the model and resource figures. The whole run took
4m23s including a cold model load.

## Honest limits

- A 3B VLM is a coarse reader. In the live-login reply it correctly quoted the sign-in and bootstrap-token text but also wrote
  "Navigation Links: not explicitly visible" and padded the answer; on the settings frame it ran button labels together
  (`TURNTHEKILLSWITCHON`). Expectation tokens are therefore short, text-level facts that appear on the real page (nav labels,
  headings), matched case-insensitively as substrings. Passing means those words were read from the pixels, NOT that layout,
  spacing, contrast or colour are right: those stay with axe and the Playwright journey (JSK-T083).
- False positives are possible both ways (a hallucinated token can satisfy `-expect`; a misread can fail a good page). Use
  `-forbid` for known-bad strings and keep expectations to text that is unambiguous at the captured resolution.
- Frames and verdicts for `console-*` were captured by the e2e journey, not by this script; `live-login` and `mutant-*` were
  captured here. TLS trust for the screenshot step is `--ignore-certificate-errors` (Chromium ignores `SSL_CERT_FILE`); the
  private-CA chain is proved separately by `helixqa http` (docs/operator/helixqa.md).
- `helixqa autonomous` (curiosity phase, video) was NOT run: this change wires and proves the per-frame vision analyzer, which is
  the leg `HELIX_VISION_ENDPOINT` unblocks. The autonomous driver and video evidence remain separate work.
- Larger model: `qwen2.5vl:7b` (6 GB) was started and stopped at 15% because the link ran at about 1.5 MB/s and 3B already passed
  every check. `VISION_MODEL=qwen2.5vl:7b scripts/vision-ollama.sh pull` switches when more accuracy is needed (about 8 GB VRAM).
