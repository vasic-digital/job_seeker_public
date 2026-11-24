// Consumer module for the canonical Containers Submodule (section 11.4.76), JSK-T010.
// The dependency is resolved by scripts/resolve_deps.sh (env override, parent umbrella submodules/, sibling dir; pin in
// ../../helix-deps.yaml, verified against the resolved checkout by gates/verify_pins.sh); Go code consumes it through
// the replace below. It is a SEPARATE module so the server's dependency graph carries none of it.
module github.com/vasic-digital/job_seeker/tools/containers

go 1.26.11

require (
	digital.vasic.containers v0.0.0
	github.com/quic-go/quic-go v0.66.0
)

replace digital.vasic.containers => ../../.deps/containers

require github.com/andybalholm/brotli v1.2.0

require (
	github.com/quic-go/qpack v0.6.0 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)
