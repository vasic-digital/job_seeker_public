# i18n real-browser render evidence (FONT-001, JSK-T126, T157, T158, T217)

Produced by `go test -run TestI18nRenderLive ./tests/e2e` with `E2E_EVIDENCE_DIR=evidence/i18n-render` on the merged tip plus this change.
Real jobseekerd, real Chromium (Playwright), virtual passkey login, no mock. 15 languages x 2 colour schemes x 4 pages = 120 frames; the
repository keeps every light frame and the dark settings frame (75 PNG files). `manifest.json` has `<html lang/dir>`, the computed body
direction and, per frame, the fonts Chromium reports as having drawn each text node (CDP `CSS.getPlatformFontsForNode`).

- `summary.tsv`: per frame `system_glyphs` (must be 0) and the shipped Noto glyph count for zh ja ko hi ar fa.
- `control-thai.json`: the paired control; Thai has no shipped glyph and the detector flags a system font for it.
- `go-test.txt`: the passing run. `../i18n-render-red/red-before-css-fix.txt` is the RED run on the same test before the CSS fix: native
  `select` controls drew with Arial and mono-styled labels (buttons, table heads) fell to DejaVu Sans Mono for Arabic, Persian and
  Kazakh and to a system CJK font for ja ko zh hi.
- Fix: `web/static/console.css` (form controls inherit the page font; mono-styled labels list the shipped body stack behind JetBrains Mono).
- Not covered: reading by a vision model (none installed), the Position page, a human native speaker's judgement of the text.
