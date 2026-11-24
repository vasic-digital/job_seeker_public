# 24 - Code-level integration guide (cookbook)

| | |
|---|---|
| Part | C of the protocols research |
| Date | 2026-11-24 |
| Status | Skeletons are **illustrative**: they use the production package paths and signatures of [21](21-extension-interfaces.md) and the real seams cited by `file:line` at `github/main`. The shapes of the JSON-LD reader, the RemoteMode rule, the keyword feature and the hand-off channel are compiled and tested in [appendix-compiled-sketch/](appendix-compiled-sketch/); the other skeletons are not compiled. Where a vendor field or endpoint is not confirmed by a captured sample it is marked UNCONFIRMED and the test must be written against a recorded sample first. |
| Numbers | new tasks cited here use ids from JSK-T225 upward (26); existing ids T160..T219 are cited as they are. |

## Contents

1. [The six-step recipe (any plugin)](#1-the-six-step-recipe)
2. [Step 0: the kernel (once)](#2-step-0-the-kernel)
3. [Integration 1: JSON-LD JobPosting reader](#3-integration-1-json-ld-jobposting-reader)
4. [Integration 2: Greenhouse, Lever, Ashby board adapters](#4-integration-2-greenhouse-lever-ashby)
5. [Integration 3: RemoteMode eligibility](#5-integration-3-remotemode-eligibility)
6. [Integration 4: keyword matcher with synonyms and word boundaries](#6-integration-4-keyword-matcher)
7. [Integration 5: PDF link and tagging profile](#7-integration-5-pdf-link-and-tagging-profile)
8. [Integration 6: SMTP/OAuth channel hand-off](#8-integration-6-smtpoauth-channel-hand-off)
9. [Cookbook: add a provider / ATS profile / CV format / rule](#9-cookbook-shorts)
10. [Checklist before opening the PR](#10-checklist)

## 1. The six-step recipe

| Step | Do | Evidence (§11.4 anti-bluff) |
|---|---|---|
| 1 | Write the **fixtures first**: golden-good, golden-bad, unknown/missing-data case, from a recorded real sample or a synthetic persona (no personal data) | fixture files under `testdata/` |
| 2 | Write `conformance_test.go` calling the kernel suite; run it and **record the RED** (compile error or failing assertion) | pasted failing output in `evidence/JSK-Txxx/red.txt` |
| 3 | Implement the plugin package; `Info()` with id, kind, version, spec, caps | green test output |
| 4 | Add the **mutation pair**: a mutant that weakens the plugin (e.g. Unknown -> Yes); the same suite must fail on it | mutant failure count printed |
| 5 | Register: **one line** in the composition root, **one line** in `rules/policy.yaml`, reason codes appended to `internal/audit/reasons.tsv` | `jsctl ext list` and `jsctl policy check` output |
| 6 | Run `gates/verify_plugins.sh` and `gates/run_all.sh` | rc 0 for both, `--prove-failure` rc 0 |

## 2. Step 0: the kernel

Land once, before any plugin (task JSK-T225). Edits:

| File | Edit |
|---|---|
| `internal/ext/{verdict,registry,evaluate,policy,strategy}.go` | copy from [appendix-compiled-sketch/ext/](appendix-compiled-sketch/ext/), change the module path only |
| `internal/ext/conformance.go` | the `Reporter`-based suites; the sketch holds `ConformEligibility`, add one suite per port (reader, source, channel, feature) following it |
| `internal/audit/reasons.tsv` | append the codes of 22 section 6 (tab-separated: code, description, covered call) |
| `internal/store/migrations/0003_ext_registry.sql`, `0004_ext_verdicts.sql` | from 23 |
| `cmd/jsobserve/main.go:81-85` | replace the literal slice with `reg := ext.NewRegistry[...]`, `Register` calls, `Freeze()`; the `providers.Provider` slice stays as input to the shim in section 4 |

Shim so existing providers become `SourceAdapter`s without touching them:

```go
// internal/ext/source/legacy.go
package source

type legacy struct{ providers.Provider; spec string }

func (l legacy) Info() ext.Info {
	return ext.Info{ID: l.Name(), Kind: "source", Version: "legacy-1", Priority: 50, Spec: l.spec}
}

// Wrap adapts today's providers (remotive, remoteok, himalayas) unchanged.
func Wrap(p providers.Provider, spec string) legacy { return legacy{p, spec} }
```

`ingest.Runner.Providers` (`internal/ingest/ingest.go:35-40`) keeps `[]providers.Provider`; a source registered through `Wrap` is unwrapped for the runner (`legacy.Provider`). New adapters implement `providers.Provider` **and** `ext.Plugin` (embedding `Info()`), so the runner needs no change until the adapters all move. That is the incremental path; nothing in `cmd/jsobserve` breaks.

## 3. Integration 1: JSON-LD JobPosting reader

Task JSK-T226. Input standard: schema.org `JobPosting` ([01](01-job-posting-data-standards.md)). Compiled reference: `appendix-compiled-sketch/refs/jsonld/`.

Files:

```
internal/ext/reader/jsonld/jsonld.go            # Reader (ext.StandardReader)
internal/ext/reader/jsonld/jsonld_test.go
internal/ext/reader/jsonld/conformance_test.go  # kernel suite + mutation pair
internal/ext/reader/jsonld/testdata/good_remote.json
internal/ext/reader/jsonld/testdata/good_onsite.json
internal/ext/reader/jsonld/testdata/bad_not_jobposting.json
internal/ext/reader/jsonld/testdata/bad_broken.json
internal/ext/reader/jsonld/testdata/graph_wrapper.json   # {"@graph":[...]} and arrays: UNCONFIRMED shapes, capture real ones first
internal/ext/reader/jsonld/PROVENANCE.md                  # where each sample came from (as internal/providers/PROVENANCE.md)
```

Mapping table (the reader's contract; every row has a test):

| schema.org field | `providers.Posting` field | Rule |
|---|---|---|
| `title` | `Title` | required, else skip `missing_field` (same class as `remotiveOne`, `internal/providers/remotive.go:101`) |
| `hiringOrganization.name` | `Company` | |
| `description` (HTML) | `Body` raw | sanitized later by `Finalize` (`providers.go:260`) |
| `url` or `directApply`+`url` | `URL`, `ApplyURL` | `httpsURL` check (`providers.go:230`); non-https -> skip `bad_url` |
| `jobLocationType == "TELECOMMUTE"` | `RemoteMode="remote"` | absent -> `""` (Unknown downstream), a present other value -> `"onsite"` |
| `applicantLocationRequirements[].name` | `Countries` | only for TELECOMMUTE postings |
| `jobLocation.address.addressCountry` | `LocationRaw`, `Countries` | |
| `datePosted`, `validThrough` | `PostedAt`, `ExpiresAt` | date or datetime; invalid -> zero value |
| `baseSalary.value.{minValue,maxValue,unitText}`, `.currency` | `SalaryMin/Max/Period/Cur` | |
| `identifier.value` or URL | `ProviderJobID` | else `sha256(URL)` |

Wiring edits:

1. `cmd/jsobserve/main.go`: `readers.Register(jsonld.Reader{})`.
2. The generic page adapter (task JSK-T213, providers/06) calls `readers.Active(settings)` and tries `Sniff` then `Read` for each `<script type="application/ld+json">` block; first `ok` wins.
3. `rules/policy.yaml`: `reader/schemaorg_jobposting: true`.
4. Legal: FR-010 forbids **publishing** JobPosting markup; reading it is allowed. Add a conformance assertion that the package exposes no encoder (grep gate in the plugin's conformance test: no `json.Marshal` of a `JobPosting`-typed value).

## 4. Integration 2: Greenhouse, Lever, Ashby

Tasks JSK-T227 (kernel `source` helpers) then the adapters in providers' JSK-T205.. group (P2); this part gives the shape. Endpoints are from [02 section 3](02-ats-and-application-apis.md) and `../providers/01-provider-landscape.md:41-43`.

Common skeleton (`internal/ext/source/greenhouse/greenhouse.go`):

```go
package greenhouse

// Adapter is a providers.Provider + ext.Plugin. Tenants (board tokens) are DATA, not code: Tokens comes from
// settings/overlay-free config `ext.source.greenhouse.boards` (comma list), never hard-coded.
type Adapter struct {
	Endpoint string   // https://boards-api.greenhouse.io/v1/boards
	Boards   []string // one request per board per cycle
}

func New(boards []string) *Adapter {
	return &Adapter{Endpoint: "https://boards-api.greenhouse.io/v1/boards", Boards: boards}
}

func (*Adapter) Name() string { return "greenhouse" }
func (*Adapter) Info() ext.Info {
	return ext.Info{ID: "greenhouse", Kind: "source", Version: "1.0.0", Priority: 30,
		Spec: "Greenhouse Job Board API (public GET); apply = redirect"}
}
func (*Adapter) Limits() providers.Limits {
	return providers.Limits{MinInterval: 2 * time.Hour, MaxPages: 1} // policy value; vendor rate limit undocumented (02)
}

// Fetch performs ONE request: the board named by q.Cursor (cursor = index into Boards). The runner pages
// by cursor (ingest.go:163..), so one board = one limiter-counted request.
func (a *Adapter) Fetch(ctx context.Context, c *egress.Client, q providers.Query) (providers.Batch, error) {
	i, _ := strconv.Atoi(q.Cursor)
	if i >= len(a.Boards) {
		return providers.Batch{}, nil
	}
	u := a.Endpoint + "/" + url.PathEscape(a.Boards[i]) + "/jobs?content=true"
	body, proof, err := providers_get(ctx, c, u) // exported wrapper of providers.get (providers.go:139)
	b := providers.Batch{Call: proof}
	if err != nil {
		return b, err
	}
	var env struct{ Jobs []json.RawMessage `json:"jobs"` }
	if json.Unmarshal(body, &env) != nil || env.Jobs == nil {
		return b, &providers.Error{Class: providers.ClassSchema, Status: proof.Status, Err: errors.New("envelope has no jobs array")}
	}
	b.Raw = body
	b.Postings, b.Skipped = providers_translate(env.Jobs, q.Limit, a.one(a.Boards[i]))
	if i+1 < len(a.Boards) {
		b.NextCursor = strconv.Itoa(i + 1)
	}
	return b, nil
}
```

Required small production edits (each a separate micro-task in JSK-T227):

| Edit | File:line | Why |
|---|---|---|
| export `Get`/`Translate`/`HTTPSURL` wrappers (thin aliases) | `internal/providers/providers.go:139,230`, `remotive.go:82` (`translate`) | adapters outside `package providers` need them; keeps one implementation |
| attribution registration | `internal/providers/providers.go:121-126` (`attributions` map literal) | **every new provider needs an attribution entry (FR-004)**; replace the literal with `RegisterAttribution(name, Attribution)` called from each adapter's `init`-free `New` caller, and keep the existing three entries |
| allow-list lines | `deploy/egress-allowlist.txt` | `boards-api.greenhouse.io`, `api.lever.co`, `api.ashbyhq.com` (exact hosts; EU Lever `api.eu.lever.co` only if the operator opts in) |
| provider row | created by `Runner.EnsureProvider` (`ingest.go:102`) | automatic |
| `kind` column | `EnsureProvider` inserts `"api"` (`ingest.go:117`) | fine for all three |

Mapping (verify each field against a captured sample first; Greenhouse `content=true` returns the HTML body in `content`, entities escaped: UNCONFIRMED, test with a recorded response):

| Vendor | id | title | company | location | remote | apply url | posted |
|---|---|---|---|---|---|---|---|
| Greenhouse | `id` | `title` | board token display name (config) | `location.name` | `""` unless text says remote: do **not** infer; Unknown | `absolute_url` | `updated_at` (first-published not given: UNCONFIRMED) |
| Lever | `id` | `text` | site (config) | `categories.location` | `workplaceType` (`remote` / `hybrid` / `on-site`) | `hostedUrl`, `applyUrl` | `createdAt` (ms epoch) |
| Ashby | `id` | `title` | board name (config) | `location` | `workplaceType` (`Remote`/`Hybrid`/`OnSite`); `isRemote` is true for Hybrid too, so ignore it | `applyUrl`, `jobUrl` | `publishedAt` |

Tests per adapter (the existing provider tests are the model: `internal/providers/providers_test.go:19-57`): recorded sample in `testdata/<vendor>_sample.json` captured by one real read (P0 task), translate table with malformed records counted by class, `Limits` equals the documented/policy values, no Authorization header ever sent, error classes via the shared `TestHTTPErrorClasses` pattern (`providers_test.go:80`), live test guarded like `internal/providers/live_test.go`.

## 5. Integration 3: RemoteMode eligibility

Task JSK-T228 (rule), then T182 (re-check at approval, existing id). Compiled reference: `appendix-compiled-sketch/refs/remotemode/` incl. the mutant.

Edits, in order:

1. `internal/ext/eligibility/remotemode/remotemode.go` from the sketch. Production reads `allow_hybrid` from its config (`policy params`), default `false`.
2. `internal/match/score.go`: today `mode := strings.ToLower(p.RemoteMode)` (`:135`) drives location scoring and `onsite_outside_allowed` (`:152-166`). **Leave scoring as is** in step 1; add the new call *beside* it:

```go
// internal/match/score.go, inside ScorePosting after the existing gates (around :205)
if r.Eligibility != nil { // nil until wired: old behaviour unchanged
	out := ext.EvaluateEligibility(r.Eligibility.Active(), toExtPosting(p), profileOf(ov), now)
	for _, t := range out.Trail {
		for _, rs := range t.Reasons {
			if gate, ok := legacyGateID[rs.Code]; ok && out.Verdict != ext.Yes {
				s.Gates = appendUnique(s.Gates, gate)
			}
		}
	}
	s.Eligibility = out // new field, serialized into explain_json
}
```

3. Parity test (RED first): over `internal/match/gold` and the score tests (`internal/match/score_test.go`) the gate ids produced by the legacy blocks equal the ids produced via plugins; then delete the legacy blocks in a follow-up task (the operator is asked first, dead-code rule).
4. `draft.Service.checkLive` (`internal/draft/service.go:510`) adds, after the overlay check, `out := EvaluateEligibility(...)`; `out.Verdict != Yes` returns `ErrGateRegressed` (T182) and writes `gate_regressed_at_approval` with `Refs{gate, plugin_version}`.
5. `rules/policy.yaml`: `eligibility/remote_mode: true`, and `locked` (22).
6. `reasons.tsv`: `not_remote`, `remote_mode_unknown`, `gate_regressed_at_approval`.

Golden table (all must exist): `remote -> Yes`; `Hybrid` (case) `-> No not_remote`; `onsite -> No`; `"" -> Unknown remote_mode_unknown`; `"remote "` (space) `-> Yes`; mutation: `Unknown->Yes` fails the table at row 4.

## 6. Integration 4: keyword matcher

Tasks T164, T165, T166 (existing). The umbrella's `_has_keyword` (Part B G-04) is a squash-substring matcher; the Go side gets the token-exact one. Compiled reference: `appendix-compiled-sketch/refs/keyword/` (table: Django vs Go, JavaScript vs Java, C++/C#, k8s <-> Kubernetes, golang <-> Go).

Edits:

1. `internal/ext/match/keyword/keyword.go` (Feature) + `terms.go` (dictionary loader) + `rules/synonyms.yaml`:

```yaml
schema: synonyms.v1
terms:                      # canonical -> variants (all matched as WHOLE TOKENS, case-insensitive)
  Kubernetes: [k8s]
  Go: [golang]
  "Amazon Web Services": [aws]
```
   Multi-word canonical terms are matched as consecutive tokens (extend `Tokens` to produce bigrams; add the test row `Amazon Web Services` vs `AWS`).
2. `internal/match/score.go:111`: `s.Components["skills"] = math.Min(1, m.S1/sr.SkillsCap)` stays the classifier-driven component; the keyword feature adds `components["keywords"]` with a weight line in `rules/score.yaml` (`weights.keywords`), `score_version` bumped (`score.yaml` field `score_version`), and `weights` normalised so they still sum to 1; the existing `TestWeightsSumToOneAndAreTheSpecifiedOnes` (`internal/match/score_test.go:18`) pins the six specified weights, so adding a seventh needs a spec amendment (proposed as FR-097 in 26) and an updated test written RED-first.
3. Honesty (T165): the feature returns the *matched terms* as `Reason{keyword_hit}`; the per-application coverage report lists matched / missing / unclaimable (terms not in the profile). The allow-list of emphasizable terms is data in the private overlay and is read through `Profile`, not a plugin parameter.
4. Gold: add `internal/match/gold` rows for the false-positive table so a regression fails `gates/verify_gold.sh`.

## 7. Integration 5: PDF link and tagging profile

Tasks T162 (link annotations) and T170 (tagged PDF/XMP) are **umbrella** work (renderer). The job_seeker side is the *verification* of what the umbrella built, so the plugin is a `FormatProfile` + `ParserSimulator` + checks:

```
internal/ext/render/pdflinks/pdflinks.go     # FormatProfile{ID:"pdf_links", LinkSchemes: ["https","mailto"]}
internal/ext/ats/parsersim/pdftext.go        # ParserSimulator: extracts text+links (see below)
internal/ext/ats/parsersim/testdata/         # golden-good PDF, golden-bad PDFs: GoToR link, scheme-less URI, label != target
```

* Extraction: pure-Go PDF parsing is **not** assumed available (`go.mod` has no PDF library; adding a dependency is an operator decision). The reference path shells out to `pdftotext`/`pdfinfo`, which exist on the host (Part B 10 section 7), through the existing exec-guard pattern used by `pdgate` (`internal/pdgate/pdgate.go` imports `os/exec`). `tesseract` is present for OCR cross-check (T179).
* Check set (`Finding` severities): `link_not_uri` (GoToR/Launch) = block; `link_scheme_not_allowed` = block; `link_label_target_mismatch` = warn; `no_struct_tree` = warn until T170 lands, then block; `page_budget_exceeded` = warn (T173).
* Where it runs: `draft.Service.Prepare` (`internal/draft/service.go:245`) after `assemble` (`:196`), results stored as `ext_verdict` rows `stage='draft'`; approval is refused if any `block` finding is current (T187 shows the table).
* Manifest freshness (T172): `internal/templates/templates.go:57-137` additionally records `gate_stamp` per file; a stamp older than the manifest epoch is stale.
* Mutation pair: a golden-bad PDF with a `GoToR` annotation must make the profile return a block finding; the mutant that only checks `/Link` presence (not the action type) must fail the same fixture. Expected on today's umbrella set: 260 of 300 PDFs flagged (Part B 11 G-02), which is the RED to record.

## 8. Integration 6: SMTP/OAuth channel hand-off

Task JSK-T229 (kernel `channel` + `openurl`), T230 (`mailto`), T231 (SMTP/OAuth compose-only). **No path sends.** Compiled reference: `appendix-compiled-sketch/refs/handoff/`.

Stages:

| Stage | Channel id | Does | Caps |
|---|---|---|---|
| 1 (T229) | `provider_url` | returns `Handoff{open_url}`; human submits on the provider's page (FR-028) | `direct_send:false, needs_human:true` |
| 2 (T230) | `mailto` | parses the posting's `mailto:` per RFC 6068 (reject unknown headers, CR/LF; [04](04-email-and-transport-protocols.md) section 4), builds a `mailto:` link whose subject/body are **fixed templates** (no generated prose, FR-029) and returns `Handoff{mailto}` plus the attachments list for the human to attach | `direct_send:false, needs_human:true` |
| 3 (T231) | `smtp_compose` | with OAuth (XOAUTH2) or app password in the operator's mail account, **creates a draft in the operator's mailbox** (IMAP APPEND to Drafts / Gmail or Graph draft endpoint) and returns `Handoff{open_url: webmail draft link}`; the human presses Send in their mail client | `direct_send:false, creates_draft:true, needs_human:true` |

The operator asked for "SMTP/OAuth hand-off": a draft the human sends is the form compatible with FR-027 and `ChannelVet`. An unattended SMTP send would need a recorded amendment of FR-026/FR-027 and is **not** designed here.

Edits for stage 3:

1. `internal/ext/channel/smtpcompose/` with a `Mailer` port (`CreateDraft(ctx, msg) (draftRef string, err)`), implementations per vendor behind it; credentials come from the environment by name only (CONST-042), never logged; the egress allow-list gains the exact mail API hosts.
2. `internal/ext/deliver/{spf,dkim,dmarc}`: `Check(domain)` must return `Yes` before `ext.channel.allow_mailto=1` can be set (23 section 4).
3. `draft.Service.Approve` (`service.go:432`) mints `Approved`; the console calls `channel.Prepare(ctx, approved)` only after `live_approved`; the returned `Handoff` is stored as `submission(kind='handoff')` payload (sealed, `0001_init.sql:93`).
4. Tests: zero `Approved` refused for every registered channel (enumerated), a CR/LF in a header value rejected, a `mailto:` with an unknown header rejected, `Prepare` makes no network call when only `Handoff` data is requested (egress client with an empty allow-list must not be touched), `ChannelVet` rejects a mutant that declares `direct_send`.

## 9. Cookbook shorts

**Add a provider** (feed/API): 1) `internal/providers/<name>.go` or `internal/ext/source/<name>/` implementing `Provider`+`Info`; 2) recorded sample + `PROVENANCE.md`; 3) test table copied from `providers_test.go:19`; 4) one line in `deploy/egress-allowlist.txt`; 5) attribution entry (`providers.go:121`); 6) `Register` line in `cmd/jsobserve/main.go`; 7) policy line `source/<name>: true`; 8) `jsctl provider add` or `EnsureProvider` creates the row. Terms of service re-read is a separate recorded step (providers/06 section 2).

**Add an ATS profile**: file `rules/ats/<id>.yaml` (23 section 5) + optional `internal/ext/ats/<id>/` only if a check needs code; fixtures: one good, one over-limit, one wrong type; one policy line; no code at all for pure-data profiles (the generic `ats/data_profile` plugin interprets the YAML).

**Add a CV/format profile**: `FormatProfile` YAML under `rules/format/<id>.yaml` (type, page budget, heading aliases per locale for T174, filename template for T175) + a country mapping in `policy.yaml scopes.country`; the umbrella builds the artifact, job_seeker verifies it with the profile; fixture = one real built PDF per locale (hash in manifest).

**Add an eligibility rule**: copy the RemoteMode package; keep to `(Posting, Profile, now) -> Decision`; every missing input answers `Unknown`; add reason codes; add to `locked` if it is a hard gate.

**Add a standard reader** (e.g. HR Open): copy the JSON-LD package; `Sniff` must not match other readers' documents (cross-fixture test: each reader's golden-good is `ok=false` for every other reader).

## 10. Checklist

- [ ] RED recorded before code; evidence directory `evidence/JSK-Txxx/`
- [ ] fixtures synthetic or recorded real samples, no personal data (`gates/check_no_personal_data.sh`)
- [ ] reasons in `reasons.tsv`; `Refs` ids/enums only
- [ ] conformance suite + mutation pair; `gates/verify_plugins.sh` rc 0
- [ ] policy line + (if a gate) `locked` decision recorded
- [ ] migrations additive, backup path exercised
- [ ] spec/FR/SC additions proposed in 26, not silently edited
- [ ] independent review before merge
