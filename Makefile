# job_seeker Makefile (JSK-T002). CGO is off everywhere (R-02, plan constraints).
export CGO_ENABLED := 0
MODULE   := github.com/vasic-digital/job_seeker
VERSION  ?= $(shell git describe --tags --always 2>/dev/null || echo "")
COMMIT   ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo "")
DIRTY    ?= $(shell test -z "$$(git status --porcelain 2>/dev/null)" && echo false || echo true)
LDFLAGS  := -s -w -X $(MODULE)/internal/buildinfo.version=$(VERSION) \
            -X $(MODULE)/internal/buildinfo.commit=$(COMMIT) \
            -X $(MODULE)/internal/buildinfo.dirty=$(DIRTY)

.PHONY: idempotency evidence-normalise evidence-check deps hardening mutation challenges vuln sbom soak atomicity txcheck build build-gate fmt vet test race gates test-all full live-h3

build:
	mkdir -p bin
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/jobseekerd ./cmd/jobseekerd
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/jsctl ./cmd/jsctl

# static binaries copied into the image by deploy/Containerfile (JSK-T031)
build-gate: build
	CGO_ENABLED=0 go build -trimpath -o bin/jsgate-serve ./cmd/jsgate-serve
	cd tools/containers && CGO_ENABLED=0 go build -trimpath -o ../../bin/jsstack ./cmd/jsstack && CGO_ENABLED=0 go build -trimpath -o ../../bin/jsgate-probe ./cmd/jsgate-probe

# live-h3: JSK-T032 live HTTP/3 + Brotli gate through a real container (needs podman or docker via the Containers module)
# RUNNER=podman|docker|nerdctl|auto forces the container runner (docs/RUNNER.md), e.g. make live-h3 RUNNER=podman
live-h3: build-gate
	bash gates/live_h3.sh $(if $(RUNNER),--runner=$(RUNNER))

fmt:
	bash gates/fmt_check.sh

vet:
	go vet ./...

# GO_TEST_TIMEOUT: tests/e2e drives a real browser against the real server and contains a real five-minute step-up clock wait,
# so the 10 minute go test default is too short on a loaded host. A hang is still bounded (30m), and a failure is never hidden.
GO_TEST_TIMEOUT ?= 30m

test:
	go test -timeout $(GO_TEST_TIMEOUT) ./...

# the race detector needs cgo; only the TEST run enables it, shipped binaries stay CGO_ENABLED=0
race:
	CGO_ENABLED=1 go test -race -timeout $(GO_TEST_TIMEOUT) ./...

# gates: fast local enforcement (no CI/CD, section 11.4.156). Runs formatting, vet and every gates/verify_*.sh.
# evidence-normalise: scrub home/checkout/user/host/temp paths from every evidence file in place (shared normaliser).
evidence-normalise:
	bash scripts/evnorm.sh -w evidence

# evidence-check: reject un-normalised evidence early (also part of `make gates`).
evidence-check:
	bash gates/verify_evidence_normalised.sh

gates: fmt vet
	go test ./gates/...
	bash gates/run_all.sh

# test-all: unit + race + gates. Live and container tests are added by later tasks and are
# reported as SKIP-with-reason by the ledger gate when their topology is absent.
test-all: gates test race

# full: the one-command automation entry (type 4 of section 11.4.169) = JSK-T109. Build, unit/race/gates, the
# integration suite (T108), then tests/full: the downstream leg on a labelled input injected at the ingest stage (N=3
# identical digests) and the live leg. The live leg is opt-in (JSK_FULL_LIVE=1) because it spends a real provider
# request: budget is DATA in tests/full/live_budget.tsv and exhaustion is SKIP provider_quota. Evidence: evidence/JSK-T109.
full: build test-all
	go test -count=1 ./tests/integration/
	JSK_EVIDENCE_DIR=$(CURDIR)/evidence/JSK-T109 go test -count=1 -v ./tests/full/

# live: builds and runs the REAL jobseekerd/jsctl, probes them over TLS, writes evidence/JSK-T033 (CGO off).
live:
	JSK_EVIDENCE_DIR=$(CURDIR)/evidence/JSK-T033 go test -count=1 -v ./tests/live/

# hardening (JSK-010): coverage floors, -race, benchmark regression gate, then the live flood/RSS and kill -9 legs
# against the real jobseekerd. Each gate has a --prove-failure arm.
hardening:
	bash gates/hardening_coverage.sh
	bash gates/hardening_fuzz.sh
	bash gates/hardening_race.sh
	bash gates/hardening_bench.sh
	bash gates/hardening_mutation.sh
	bash gates/hardening_vuln.sh
	bash gates/hardening_sbom.sh
	$(MAKE) atomicity txcheck
	bash gates/hardening_challenges.sh
	JSK_EVIDENCE_DIR=$(CURDIR)/evidence/JSK-T112 go test -count=1 -v -run Flood ./tests/live/
	JSK_EVIDENCE_DIR=$(CURDIR)/evidence/JSK-T114 go test -count=1 -v -run Chaos ./tests/live/
	JSK_EVIDENCE_DIR=$(CURDIR)/evidence/JSK-T114 go test -count=1 -v ./tests/chaos/

# JSK-T107 per-package mutation pairs (each row must be killed; --prove-failure proves the gate itself)
mutation:
	bash gates/hardening_mutation.sh
	bash gates/hardening_vuln.sh
	bash gates/hardening_sbom.sh

# Resolve the own-org dependencies (env JSK_DEP_<NAME> / parent umbrella submodules/ / sibling dir) into .deps/
deps:
	bash scripts/resolve_deps.sh link

# JSK-T110 challenge bank through the Challenges Submodule (needs challenges + containers resolvable: make deps)
challenges:
	JSK_EVIDENCE_DIR=$(CURDIR)/evidence/JSK-T110 bash gates/hardening_challenges.sh

# JSK-T113 security: govulncheck (RED until the Go toolchain is on a patched release), SBOM, needle scan
vuln:
	bash gates/hardening_vuln.sh
sbom:
	bash gates/hardening_sbom.sh

# JSK-T115 concurrency and atomicity (race detector on) and JSK-T116 static write-tx / lock-order review
atomicity:
	CGO_ENABLED=1 go test -race -count=1 ./tests/atomicity
# JSK-T253 idempotency under retry (11.4.253, G23): every mutating operation replayed twice against the real stack, store digests compared
idempotency:
	CGO_ENABLED=0 go test -count=1 -v ./tests/idempotency
txcheck:
	go test -count=1 -v ./gates/txcheck

# JSK-T117 long soak against the real jobseekerd: make soak SOAK_SECS=1920 (opt-in, runs SOAK_SECS seconds)
SOAK_SECS ?= 1920
soak:
	JSK_SOAK_SECS=$(SOAK_SECS) JSK_EVIDENCE_DIR=$(CURDIR)/evidence/JSK-T117 go test -count=1 -timeout 3h -v -run SoakRSS ./tests/live/
