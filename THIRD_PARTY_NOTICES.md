# Third-party notices

job_seeker's own code is under the source-available, all-rights-reserved [LICENSE](LICENSE).
Third-party components keep their own licences, which this file lists. It does not grant or
restrict anything beyond what those licences say; consult each project for its full text.

## Go modules (compiled into the main module)

Derived, not hand-written: `python3 scripts/gen_third_party_notices.py` runs `go list -deps -test ./...`
and `go list -m -json all`, keeps only modules that supply at least one package, and classifies each by
text markers in the LICENSE/COPYING file of the module cache copy. A module whose licence text matches
no marker is printed as `see <file>` or `UNDETERMINED`; none is guessed. Reading of 2026-11-24 over
`go.mod`/`go.sum` at this revision (the sub-modules under `tools/` are listed separately below):

| Module | Version | Licence (detected) |
|---|---|---|
| `github.com/andybalholm/brotli` | v1.3.0 | MIT |
| `github.com/dustin/go-humanize` | v1.1.0 | MIT |
| `github.com/fxamacker/cbor/v2` | v2.9.4 | MIT |
| `github.com/gabriel-vasile/mimetype` | v1.4.12 | MIT |
| `github.com/gin-contrib/sse` | v1.1.0 | MIT |
| `github.com/gin-gonic/gin` | v1.12.0 | MIT |
| `github.com/go-playground/locales` | v0.14.1 | MIT |
| `github.com/go-playground/universal-translator` | v0.18.1 | MIT |
| `github.com/go-playground/validator/v10` | v10.30.1 | MIT |
| `github.com/go-viper/mapstructure/v2` | v2.5.0 | MIT |
| `github.com/go-webauthn/webauthn` | v0.18.2 | BSD-3-Clause |
| `github.com/go-webauthn/x` | v0.3.1 | BSD-3-Clause |
| `github.com/goccy/go-yaml` | v1.19.2 | MIT |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | MIT |
| `github.com/google/go-tpm` | v0.9.8 | Apache-2.0 |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause |
| `github.com/leodido/go-urn` | v1.4.0 | MIT |
| `github.com/mattn/go-isatty` | v0.0.24 | MIT |
| `github.com/pelletier/go-toml/v2` | v2.2.4 | MIT |
| `github.com/philhofer/fwd` | v1.2.0 | MIT |
| `github.com/quic-go/qpack` | v0.6.0 | MIT |
| `github.com/quic-go/quic-go` | v0.66.0 | MIT |
| `github.com/remyoudompheng/bigfft` | v0.0.0-20230129092748-24d4a6f8daec | BSD-3-Clause |
| `github.com/tinylib/msgp` | v1.6.4 | MIT |
| `github.com/ugorji/go/codec` | v1.3.1 | MIT |
| `github.com/x448/float16` | v0.8.4 | MIT |
| `go.mongodb.org/mongo-driver/v2` | v2.5.0 | Apache-2.0 |
| `golang.org/x/crypto` | v0.59.0 | BSD-3-Clause |
| `golang.org/x/net` | v0.62.0 | BSD-3-Clause |
| `golang.org/x/sys` | v0.50.0 | BSD-3-Clause |
| `golang.org/x/text` | v0.44.0 | BSD-3-Clause |
| `google.golang.org/protobuf` | v1.36.10 | BSD-3-Clause |
| `modernc.org/libc` | v1.80.1 | BSD-3-Clause |
| `modernc.org/mathutil` | v1.7.1 | BSD-3-Clause |
| `modernc.org/memory` | v1.12.1 | BSD-3-Clause |
| `modernc.org/sqlite` | v1.63.0 | BSD-3-Clause |

No copyleft-strong (GPL/AGPL) licence was detected among them. The one MPL-2.0 file-level-copyleft
module, if listed above, is used unmodified.

## Own-org dependencies (`helix-deps.yaml`, resolved by `scripts/resolve_deps.sh`; not git submodules)

| Path | Upstream | Licence |
|---|---|---|
| `containers` | `vasic-digital/containers` | first-party (same Owner); no LICENSE file was found in the checkout inspected - UNDETERMINED, to be settled by the Owner |
| `challenges` | `vasic-digital/challenges` | first-party (same Owner); no LICENSE file found - UNDETERMINED |
| `security` | `vasic-digital/security` | first-party (same Owner); no LICENSE file found - UNDETERMINED |
| `qa` | `vasic-digital/HelixQA` | Apache-2.0 (LICENSE file present, text read) |

The dependencies were not checked out in the worktree this file was produced from; their licence files
were read from the sibling checkouts on the author's machine, which may sit at a different pin.
`tools/containers` and `tools/challenges` are separate Go modules that depend on the first two; their
transitive Go dependencies were NOT enumerated here (the dependencies they `replace` were absent) and are
UNDETERMINED until `scripts/gen_third_party_notices.py` is run inside each with the dependencies resolved.

## Other third-party software

`jobops` (`DaKheera47/job-ops`) is run as a separate third-party service behind an adapter, not vendored or
linked; it keeps its own upstream licence.
