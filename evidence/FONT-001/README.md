# FONT-001 evidence

`run.txt` is the captured output of `scripts/build_noto_subsets.py --check`, `gates/verify_font_licences.sh`, `gates/verify_font_coverage.sh`
and the official-cmap table (`docs/design/noto-official-sources.tsv`, produced by `scripts/noto_official_check.py` against the official
upstream Noto files). Faces are defined by data in `web/static/design/fonts/noto/codepoints/`. Source fonts are the Debian packages recorded in
`noto/SOURCES.tsv`; any other installed version is rc 2. The CJK rebuild is deterministic (two builds, identical sha256). Arabic and Devanagari
code point sets were unchanged and those four files are byte-identical to before. The encoder options of the earlier subsets were not recorded, so the
rebuilt CJK files are not byte-identical to the old ones even for the same code points (sizes grew by about 6 percent).
