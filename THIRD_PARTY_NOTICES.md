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

## Fonts (`web/static/design/`)

Every font file the console serves is third-party software under the SIL Open Font License 1.1 (OFL-1.1), which permits
bundling, embedding and redistributing the unmodified or subset files provided the licence text and copyright notice travel
with them. The files are synced byte-for-byte from the umbrella `design-system/` (pinned by `web/static/design/MANIFEST.sha256`),
never edited here. The table is derived by `python3 scripts/gen_font_notices.py` from the files on disk (paths are relative to
`web/static/design/`; `--check` fails on drift), and `bash gates/verify_font_licences.sh` fails when a shipped font file has
no row, a row's licence file is missing or carries no OFL text, or a row's sha256 no longer matches the file
(`--prove-failure` runs eleven mutations and a control).

The Noto rows are the script faces for the languages Inter does not cover (Arabic and Persian, Hindi, Chinese, Japanese,
Korean); Cyrillic (ru sr be kk) is covered by Inter itself. The glyph decision, the regeneration command and the open items
(FONT-001 to FONT-003) are recorded in the umbrella's `design-system/THIRD_PARTY_NOTICES.md`.

<!-- BEGIN generated:fonts -->

| File | Family / weight | Version | Licence | Licence file | sha256 | Source |
|---|---|---|---|---|---|---|
| `brand/display/bricolage-700-latin-ext.woff2` | Bricolage Grotesque | UNKNOWN (not recorded in the tree) | OFL-1.1 | `brand/display/OFL-Bricolage.txt` | 0b7d83e8c3fcf011b50af79aa7dd50d9a094991aa4243258dddfe8420e21c5d3 | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `brand/display/bricolage-700-latin.woff2` | Bricolage Grotesque | UNKNOWN (not recorded in the tree) | OFL-1.1 | `brand/display/OFL-Bricolage.txt` | aaa4c454785d0061834014abbab28ac995879166133a89effaf5f4cf02925d7e | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `brand/display/bricolage-800-latin-ext.woff2` | Bricolage Grotesque | UNKNOWN (not recorded in the tree) | OFL-1.1 | `brand/display/OFL-Bricolage.txt` | 734f941cd72c2ba077c299332a3687a3b12e21b61049d92c5117812efa09f2de | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `brand/display/bricolage-800-latin.woff2` | Bricolage Grotesque | UNKNOWN (not recorded in the tree) | OFL-1.1 | `brand/display/OFL-Bricolage.txt` | d96719122a2cf22b113b16d79b9605c6afda6a1879d48d844fcb2da836077765 | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `fonts/inter/inter-cyrillic-ext.woff2` | Inter | UNKNOWN (not recorded in the tree) | OFL-1.1 | `fonts/inter/OFL.txt` | ca157063339ac4ad418f214f3abfed119b0798ab4d377386ce5c9e5a7a435ebd | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `fonts/inter/inter-cyrillic.woff2` | Inter | UNKNOWN (not recorded in the tree) | OFL-1.1 | `fonts/inter/OFL.txt` | 71d5ee93cc1e9f1d520a3a8b66456de18c7879d8df09d57fcd2eaff75fef0075 | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `fonts/inter/inter-latin-ext.woff2` | Inter | UNKNOWN (not recorded in the tree) | OFL-1.1 | `fonts/inter/OFL.txt` | 34b9c504cab7a73e37b746343a449132e56cf7b5481af2cb81dc74dcff25c956 | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `fonts/inter/inter-latin.woff2` | Inter | UNKNOWN (not recorded in the tree) | OFL-1.1 | `fonts/inter/OFL.txt` | 3100e775e8616cd2611beecfa23a4263d7037586789b43f035236a2e6fbd4c62 | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `fonts/jetbrains-mono/jetbrains-mono-cyrillic-ext.woff2` | JetBrains Mono | UNKNOWN (not recorded in the tree) | OFL-1.1 | `fonts/jetbrains-mono/OFL.txt` | 62213be8a78b42f1e29d1452d91e2f8b3e745572a9dd98d3941e39fa00b37d76 | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `fonts/jetbrains-mono/jetbrains-mono-cyrillic.woff2` | JetBrains Mono | UNKNOWN (not recorded in the tree) | OFL-1.1 | `fonts/jetbrains-mono/OFL.txt` | e17cfd15fb96909d64095015f958207063a0c07191da3512df7d560a781aebdf | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `fonts/jetbrains-mono/jetbrains-mono-latin-ext.woff2` | JetBrains Mono | UNKNOWN (not recorded in the tree) | OFL-1.1 | `fonts/jetbrains-mono/OFL.txt` | db5ff4db83e580426280e9337a58dc57d3a83784a1b03ad80914651594441d52 | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `fonts/jetbrains-mono/jetbrains-mono-latin.woff2` | JetBrains Mono | UNKNOWN (not recorded in the tree) | OFL-1.1 | `fonts/jetbrains-mono/OFL.txt` | 83c005d49d8a6a50474c73a5a36ac0468076e9c4a29da7bdb14995d80560a5be | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `fonts/noto/noto-sans-arabic-400.woff2` | Noto Sans Arabic 400 all | Version 2.005; ttfautohint (v1.8.3) -l 8 -r 50 -G 200 -x 14 -D arab -f none -a qsq -X "" (source `NotoSansArabic-Regular.ttf`) | OFL-1.1 | `fonts/noto/OFL.txt` | 3c12b79528e978e599b6acd5ccd26a1d7d32d9e3276447e31ee079d579ddacf6 | Debian `fonts-noto-core` 20201225-2build1, subset by the umbrella `design-system/fonts/tools/build_noto_subsets.py` (1136 codepoints) |
| `fonts/noto/noto-sans-arabic-700.woff2` | Noto Sans Arabic 700 all | Version 2.005; ttfautohint (v1.8.3) -l 8 -r 50 -G 200 -x 14 -D arab -f none -a qsq -X "" (source `NotoSansArabic-Bold.ttf`) | OFL-1.1 | `fonts/noto/OFL.txt` | d4b546788f430e6d17ae983645a86c17a84dd3ecd25848a9a3e69b0438d2b27e | Debian `fonts-noto-core` 20201225-2build1, subset by the umbrella `design-system/fonts/tools/build_noto_subsets.py` (1136 codepoints) |
| `fonts/noto/noto-sans-devanagari-400.woff2` | Noto Sans Devanagari 400 all | Version 2.001; ttfautohint (v1.8.3) -l 8 -r 50 -G 200 -x 14 -D deva -f none -a qsq -X "" (source `NotoSansDevanagari-Regular.ttf`) | OFL-1.1 | `fonts/noto/OFL.txt` | 49dad4dfe047ca2b073959df35536a6555200ebc1662f38c8f6a6a222159b6bd | Debian `fonts-noto-core` 20201225-2build1, subset by the umbrella `design-system/fonts/tools/build_noto_subsets.py` (215 codepoints) |
| `fonts/noto/noto-sans-devanagari-700.woff2` | Noto Sans Devanagari 700 all | Version 2.001; ttfautohint (v1.8.3) -l 8 -r 50 -G 200 -x 14 -D deva -f none -a qsq -X "" (source `NotoSansDevanagari-Bold.ttf`) | OFL-1.1 | `fonts/noto/OFL.txt` | 5dbadec5085cc21e27e59ec748a850af4c757495ee2e08237832c503c5c1fda6 | Debian `fonts-noto-core` 20201225-2build1, subset by the umbrella `design-system/fonts/tools/build_noto_subsets.py` (215 codepoints) |
| `fonts/noto/noto-sans-jp-400-core.woff2` | Noto Sans JP 400 core | Version 2.004;hotconv 1.0.118;makeotfexe 2.5.65603 (source `NotoSansCJK-Regular.ttc#0`) | OFL-1.1 | `fonts/noto/OFL-CJK.txt` | 394c95db353c26d5f6f2727c3f1b7d580ec71a64c26c8e8907ab4d7fbaecb739 | Debian `fonts-noto-cjk` 1:20240730+repack1-1build1, subset by the umbrella `design-system/fonts/tools/build_noto_subsets.py` (1100 codepoints) |
| `fonts/noto/noto-sans-jp-400-rest.woff2` | Noto Sans JP 400 rest | Version 2.004;hotconv 1.0.118;makeotfexe 2.5.65603 (source `NotoSansCJK-Regular.ttc#0`) | OFL-1.1 | `fonts/noto/OFL-CJK.txt` | 5854f6907867a68e559d65c297b0e5c5af383d5f509ca0ba90b5b6d4d0d3e16d | Debian `fonts-noto-cjk` 1:20240730+repack1-1build1, subset by the umbrella `design-system/fonts/tools/build_noto_subsets.py` (2854 codepoints) |
| `fonts/noto/noto-sans-kr-400-core.woff2` | Noto Sans KR 400 core | Version 2.004;hotconv 1.0.118;makeotfexe 2.5.65603 (source `NotoSansCJK-Regular.ttc#1`) | OFL-1.1 | `fonts/noto/OFL-CJK.txt` | d0f833f6b1e30e4c09868eb1e1b845711e30a26d712b96069647c156c694ddb3 | Debian `fonts-noto-cjk` 1:20240730+repack1-1build1, subset by the umbrella `design-system/fonts/tools/build_noto_subsets.py` (771 codepoints) |
| `fonts/noto/noto-sans-kr-400-rest.woff2` | Noto Sans KR 400 rest | Version 2.004;hotconv 1.0.118;makeotfexe 2.5.65603 (source `NotoSansCJK-Regular.ttc#1`) | OFL-1.1 | `fonts/noto/OFL-CJK.txt` | 2284108a95d6291192c88d8cdebfb7a8e80bc286ea4fbf99249aba1b5a046664 | Debian `fonts-noto-cjk` 1:20240730+repack1-1build1, subset by the umbrella `design-system/fonts/tools/build_noto_subsets.py` (2580 codepoints) |
| `fonts/noto/noto-sans-sc-400-core.woff2` | Noto Sans SC 400 core | Version 2.004;hotconv 1.0.118;makeotfexe 2.5.65603 (source `NotoSansCJK-Regular.ttc#2`) | OFL-1.1 | `fonts/noto/OFL-CJK.txt` | 6a63050a8d2f7075c3c1cb0eb2a9223d9c3b323503a840d07a61f1954b180c31 | Debian `fonts-noto-cjk` 1:20240730+repack1-1build1, subset by the umbrella `design-system/fonts/tools/build_noto_subsets.py` (1335 codepoints) |
| `fonts/noto/noto-sans-sc-400-rest.woff2` | Noto Sans SC 400 rest | Version 2.004;hotconv 1.0.118;makeotfexe 2.5.65603 (source `NotoSansCJK-Regular.ttc#2`) | OFL-1.1 | `fonts/noto/OFL-CJK.txt` | 4b4278a5f6fe87b20dd2bb3f8add1fe075c6485a6b76084750b4f07d1bbb847c | Debian `fonts-noto-cjk` 1:20240730+repack1-1build1, subset by the umbrella `design-system/fonts/tools/build_noto_subsets.py` (3539 codepoints) |
| `fonts/space-grotesk/space-grotesk-latin-ext.woff2` | Space Grotesk | UNKNOWN (not recorded in the tree) | OFL-1.1 | `fonts/space-grotesk/OFL.txt` | 952dddb45d2f96f71cbf3b7f510b24379afc3c89ea02fcf89d377b45d62c0166 | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |
| `fonts/space-grotesk/space-grotesk-latin.woff2` | Space Grotesk | UNKNOWN (not recorded in the tree) | OFL-1.1 | `fonts/space-grotesk/OFL.txt` | 0640890476fc1198ab4de571fb658de443c4d85b66466ec09534a8737ab1ce9d | fetched from the Google Fonts CSS2 woff2 endpoint and committed as shipped (design-system/fonts/fonts.css header) |

<!-- END generated:fonts -->

## Other third-party software

`jobops` (`DaKheera47/job-ops`) is run as a separate third-party service behind an adapter, not vendored or
linked; it keeps its own upstream licence.
