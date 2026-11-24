# design-toolkit assessment and design-contract consumption (JSK-T070, FR-041)

Revision 2026-11-24. Honesty markers: VERIFIED = re-read at source this session; PROPOSED = a decision awaiting the operator; ASSUMED = inference.

## 1. What `design-toolkit` is (VERIFIED, `<umbrella>/design-toolkit`, HEAD 1b9abac)

A hub engine of generators and knowledge notes: `generators/` (`gen-tokens.mjs`, `dtcg-to-od.mjs`, `gen-marks.mjs`), `knowledge/` (colour, motion, a11y-wcag22, aria-apg, typography, layout, DTCG tokens), `docs/` (MANUAL, USER-GUIDE, hybrid-pipeline) and `evidence/`. It produces tokens and brand marks; it ships no console components and no component catalogue of its own.

## 2. Verdict

| Question | Answer |
|---|---|
| Is it consumed as a submodule of job_seeker? | NO. Verdict: **reference only, not vendored.** job_seeker needs no token generation: the umbrella `design-system/` already carries the finished `brand-vasic-digital` token file, `components-extended.css`, fonts and motion CSS, and `jsctl sync-design` copies exactly those, pinned by `web/static/design/MANIFEST.sha256` and checked by `gates/verify_design.sh` (with `--prove-failure`). Adding a second source of the same tokens would create a second canonical source, which §11.4.216 forbids. |
| What would change the verdict? | A job_seeker-specific brand (own accent) would need `gen-tokens.mjs`; that is a new work item, not claimed here. |
| Is the console a UI-shipping product (spec C1)? | Treated as YES; no internal-tool exemption is claimed. The operator's Q3 answer is still open (UNDECIDED), so the full contract is consumed. |

## 3. §11.4.216 canonical token source (VERIFIED)

One file: `web/static/design/brand/vasic-digital.css` (copied, never edited). `web/static/console.css` defines no token and carries no literal colour; both are asserted by `gates/verify_design.sh` (mutations: edited token file, unpinned file, token redefinition all rc 1).

## 4. §11.4.217 brand contract (VERIFIED for presence, PROPOSED for the rest)

The contract artefact is the umbrella's `design-system/brand-vasic-digital/DESIGN.md`. It is consumed by reference, not copied (a copy would be a second source). Read at source on 2026-11-24 the file is a short portable brand contract (voice, accent `#dc3545` / `#ff6b6b`, three font families, signature devices, light and dark). It is NOT the nine-section DESIGN.md plus `tokens.css` twin that §11.4.217 describes: that gap is upstream, in the umbrella design system, and is reported here, not patched from this repository. Consequence: the console is "consuming the contract as it exists"; a nine-section conformance claim would be a bluff and is not made.

## 5. §11.4.218 library catalogue (location declared as DATA)

```yaml
# docs/design/catalogue-location.yaml equivalent, inline
library_catalogue:
  location: <umbrella>/design-system/preview/components.html   # rendered component library (VERIFIED present, 131 od- class references)
  self_contained_claim: UNVERIFIED   # zero-external-request property was not measured here
  console_components_used: [od-btn, od-card, od-field, od-label, od-input, od-textarea, od-radio, od-table, od-alert, od-empty-state, od-header, od-nav, od-skip-link, od-visually-hidden]   # measured from web/templates by grep
```

The console adds no component of its own to the library: every `od-*` class it uses exists upstream; the few `console-*` layout helpers live in `console.css` and are layout only (no colour, no token).

## 6. Fonts and licences (VERIFIED)

Inter, JetBrains Mono and Space Grotesk are served from `web/static/design/fonts/` (self-hosted, no CDN), each directory carries its `OFL.txt` (SIL OFL 1.1), and the display font Bricolage Grotesque carries `brand/display/OFL-Bricolage.txt`. OFL permits bundling and web use with the licence text retained, which the sync preserves (hash-pinned in the manifest). A legal review was not performed. Upstream defect carried: the brand CSS references `display/` fonts that live in `brand-vasic-digital/fonts`; `sync-design` places them so the reference resolves.
