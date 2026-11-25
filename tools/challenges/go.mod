// Consumer module for the Challenges dependency (resolved via scripts/resolve_deps.sh -> .deps/challenges) (section 11.4.169, JSK-T110). A SEPARATE module so the server's
// dependency graph carries none of it. Its import path sits under github.com/vasic-digital/job_seeker/, which is
// what lets it import the application's internal/ packages and drive the REAL code (no re-implementation).
module github.com/vasic-digital/job_seeker/tools/challenges

go 1.26.11

require (
	digital.vasic.challenges v0.0.0
	github.com/vasic-digital/job_seeker v0.0.0
)

require (
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rogpeppe/go-internal v1.16.0 // indirect
	golang.org/x/crypto v0.59.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.63.0 // indirect
)

replace digital.vasic.challenges => ../../.deps/challenges

replace github.com/vasic-digital/job_seeker => ../..

replace digital.vasic.containers => ../../.deps/containers
