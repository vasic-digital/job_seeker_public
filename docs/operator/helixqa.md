# Running HelixQA against job_seeker (JSK-T111)

`submodules/qa` is HelixQA. Its `go.mod` replaces eight sibling modules with `../<name>`, so build it from a scratch
directory where they sit side by side (do not add them to this repository):

    mkdir -p "$S/hq" && cd "$S/hq"
    git clone --depth 1 git@github.com:vasic-digital/HelixQA.git qa        # or copy submodules/qa without .git
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
  is proved there. The autonomous driver (`helixqa autonomous`) is still not run.
