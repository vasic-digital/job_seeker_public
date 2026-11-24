# 21 - Extension interfaces and plugin lifecycle

| | |
|---|---|
| Part | C of the protocols research |
| Date | 2026-11-24 |
| Compiles | Every code block in sections 2-4 is inlined by script from [appendix-compiled-sketch/ext/](appendix-compiled-sketch/ext/); `go vet` and `go test -race` output is in [appendix-sketch-output.txt](appendix-sketch-output.txt). Package paths in prose are the **proposed** production paths; the sketch module is `example.com/jsk-sketch` and flattens the ports into one package. |
| Production code | unchanged by this PR |
| Related | Provider/Position research: [../providers/03-canonical-position-model.md](../providers/03-canonical-position-model.md) (`position.Resolver`), [../providers/05-apply-policy-and-provider-selection.md](../providers/05-apply-policy-and-provider-selection.md) (gate G9) |

## Contents

1. [Conventions](#1-conventions)
2. [Kernel: verdicts, reasons, registry](#2-kernel)
3. [Ports](#3-ports)
4. [Evaluation helper and policy resolver](#4-evaluation-and-policy)
5. [Mapping onto today's code](#5-mapping-onto-todays-code)
6. [Plugin lifecycle](#6-plugin-lifecycle)
7. [The approval hard rule](#7-the-approval-hard-rule)
8. [Reference implementations in the sketch](#8-reference-implementations)
9. [Part A standards to ports](#9-part-a-standards-to-ports)
10. [Ports owned by the provider research](#10-ports-owned-by-the-provider-research)

## 1. Conventions

* Package root `internal/ext` (kernel) and `internal/ext/<kind>` per port, adapters one level below (20 section 8).
* `context.Context` first; no global state; no goroutines started by a plugin; plugins are safe for concurrent use after registration (immutable config only).
* IDs are `lower_snake`. `Info().Version` is semver of the plugin **logic** and is stored with every verdict (23).
* A plugin returns `error` only for faults (I/O, malformed input to be counted). A business "no" is a `Decision`, never an error.
* Reason `Code`s must exist in `internal/audit/reasons.tsv`; the conformance suite checks it (25).

## 2. Kernel

### 2.1 `internal/ext/verdict.go`

```go
// Package ext is the compiled sketch of the job_seeker extension interfaces (docs/research/protocols/21).
// In the real tree this code lives in internal/ext (verdicts, registry) and internal/ext/<kind> (ports).
package ext

import "fmt"

// Verdict is three-valued. Unknown is never a pass: Combine maps it to a block for gates.
type Verdict uint8

const (
	// Unknown means the rule could not decide (missing data, plugin fault). It blocks like No.
	Unknown Verdict = iota
	// No means the rule decided against.
	No
	// Yes means the rule decided in favour.
	Yes
)

func (v Verdict) String() string {
	switch v {
	case Yes:
		return "yes"
	case No:
		return "no"
	}
	return "unknown"
}

// Reason is one explainable cause. Code comes from a closed vocabulary (reasons.tsv); Refs are ids and
// hashes only, never overlay values or posting text (audit evidence rule, internal/audit/audit.go:109).
type Reason struct {
	Code string
	Refs map[string]string
}

// Decision is what a rule returns.
type Decision struct {
	Verdict Verdict
	Reasons []Reason
}

// Outcome is the combination of many Decisions.
type Outcome struct {
	Verdict Verdict
	Trail   []Trail
}

// Trail records which rule said what, in evaluation order.
type Trail struct {
	RuleID  string
	Version string
	Decision
}

// Combine folds decisions: any No wins, else any Unknown wins, else Yes. An empty set is Unknown
// (no rule ran is not consent).
func Combine(ts []Trail) Outcome {
	o := Outcome{Verdict: Yes, Trail: ts}
	if len(ts) == 0 {
		o.Verdict = Unknown
		return o
	}
	for _, t := range ts {
		switch t.Verdict {
		case No:
			o.Verdict = No
			return o
		case Unknown:
			o.Verdict = Unknown
		}
	}
	return o
}

// ErrFailClosed wraps every refusal produced by the framework itself.
type ErrFailClosed struct{ Why string }

func (e *ErrFailClosed) Error() string { return fmt.Sprintf("ext: fail closed: %s", e.Why) }
```

### 2.2 `internal/ext/registry.go`

```go
package ext

import (
	"fmt"
	"sort"
)

// Kind names an extension point.
type Kind string

// Info is the registration record every plugin returns. ID is lower_snake and globally unique within a Kind.
type Info struct {
	ID       string
	Kind     Kind
	Version  string          // semver of the plugin logic; stored with every verdict
	Priority int             // lower runs first; ties broken by ID, so ordering is deterministic
	Caps     map[string]bool // capability flags, e.g. "direct_send": false
	Spec     string          // standard or rule implemented, with its version/date
}

// Plugin is implemented by every extension.
type Plugin interface{ Info() Info }

// Settings is the read-only view of operator configuration (the setting table in the real tree).
type Settings interface {
	// Enabled reports the explicit switch for kind/id; ok=false means the operator never decided.
	Enabled(kind Kind, id string) (on, ok bool)
}

// Registry holds plugins of one port type. Registration happens in code at start-up (one line per plugin);
// there is no dynamic loading. Not safe for concurrent Register; read-only after Freeze.
type Registry[T Plugin] struct {
	kind   Kind
	byID   map[string]T
	frozen bool
	// DefaultOn decides plugins the operator never mentioned. Gates use false (fail closed for features),
	// but the Active step below refuses an empty gate set separately.
	DefaultOn bool
	// Vet is an optional admission check, e.g. reject channels with Caps["direct_send"].
	Vet func(Info) error
}

// NewRegistry returns an empty registry for kind.
func NewRegistry[T Plugin](kind Kind) *Registry[T] {
	return &Registry[T]{kind: kind, byID: map[string]T{}}
}

// Register adds p. It refuses duplicates, wrong kind, empty id/version and failed Vet.
func (r *Registry[T]) Register(p T) error {
	if r.frozen {
		return &ErrFailClosed{"register after freeze"}
	}
	i := p.Info()
	switch {
	case i.Kind != r.kind:
		return fmt.Errorf("ext: %q has kind %q, registry is %q", i.ID, i.Kind, r.kind)
	case i.ID == "" || i.Version == "":
		return &ErrFailClosed{"plugin without id or version"}
	}
	if _, dup := r.byID[i.ID]; dup {
		return fmt.Errorf("ext: duplicate %s plugin %q", r.kind, i.ID)
	}
	if r.Vet != nil {
		if err := r.Vet(i); err != nil {
			return err
		}
	}
	r.byID[i.ID] = p
	return nil
}

// Freeze ends registration.
func (r *Registry[T]) Freeze() { r.frozen = true }

// Active returns enabled plugins in deterministic (Priority, ID) order. Unknown ids named in settings are
// not an error here; Audit reports them.
func (r *Registry[T]) Active(s Settings) []T {
	var out []T
	for _, p := range r.byID {
		on, ok := s.Enabled(r.kind, p.Info().ID)
		if !ok {
			on = r.DefaultOn
		}
		if on {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(a, b int) bool {
		ia, ib := out[a].Info(), out[b].Info()
		if ia.Priority != ib.Priority {
			return ia.Priority < ib.Priority
		}
		return ia.ID < ib.ID
	})
	return out
}

// IDs lists registered ids, sorted (for capability discovery and the conformance suite).
func (r *Registry[T]) IDs() []string {
	var ids []string
	for id := range r.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Get returns a plugin by id.
func (r *Registry[T]) Get(id string) (T, bool) { p, ok := r.byID[id]; return p, ok }
```

## 3. Ports

### 3.1 Core ports and the approval token: `internal/ext/{reader,eligibility,match,channel}`

```go
package ext

import (
	"context"
	"time"
)

// Posting is the canonical record (mirrors providers.Posting, internal/providers/providers.go:94).
type Posting struct {
	Provider, JobID, URL, ApplyURL, Title, Company, Location string
	RemoteMode                                               string // remote | hybrid | onsite | ""
	Countries, Tags                                          []string
	TZHint, Body                                             string
	PostedAt                                                 time.Time
}

// StandardReader turns a raw document in some standard (schema.org JSON-LD, HR Open, ...) into a Posting.
// ok=false with nil error means "this document is not mine" (so several readers can be tried).
type StandardReader interface {
	Plugin
	Sniff(contentType string, head []byte) bool
	Read(ctx context.Context, raw []byte) (p Posting, ok bool, err error)
}

// Profile is the operator's private view. Fields are accessed only through methods so plugins cannot log values.
type Profile interface {
	EligibleCountries() ([]string, bool) // ok=false: not configured
	WorkMode() (mode string, ok bool)
}

// EligibilityRule decides one hard condition. It must be pure and deterministic.
type EligibilityRule interface {
	Plugin
	Evaluate(p Posting, pr Profile, now time.Time) Decision
}

// ScoreFeature contributes one component in [0,1] with a weight owned by data, never by code.
type ScoreFeature interface {
	Plugin
	Component(p Posting, pr Profile) (value float64, why []Reason)
}

// Approved is the only thing a channel accepts. It can be produced only by NewApproved, which refuses a
// draft hash that differs from the approved hash or an empty approver (FR-026 (2)(3)).
type Approved struct {
	AppID                 int64
	DraftHash, approvedBy string
	URL                   string
}

// NewApproved validates and seals an approval record.
func NewApproved(appID int64, approver, approvedHash, draftHash, url string) (Approved, error) {
	if approver == "" || approvedHash == "" || approvedHash != draftHash {
		return Approved{}, &ErrFailClosed{"approval missing or hash mismatch"}
	}
	return Approved{AppID: appID, DraftHash: draftHash, approvedBy: approver, URL: url}, nil
}

// Valid reports whether a was produced by NewApproved.
func (a Approved) Valid() bool { return a.approvedBy != "" && a.DraftHash != "" }

// Handoff is what the human is shown. A channel never performs the final act in v1.
type Handoff struct {
	Kind  string // "open_url" | "mailto" | "manual"
	Link  string
	Notes []string
}

// ApplicationChannel prepares the human's next action. Caps["direct_send"] must be false in v1; the apply
// registry's Vet function rejects anything else.
type ApplicationChannel interface {
	Plugin
	Supports(p Posting) bool
	Prepare(ctx context.Context, a Approved) (Handoff, error)
}

// ChannelVet is the admission check for the channel registry.
func ChannelVet(i Info) error {
	if i.Caps["direct_send"] {
		return &ErrFailClosed{"channel " + i.ID + " declares direct_send; not allowed (FR-027)"}
	}
	return nil
}
```

### 3.2 Remaining ports: `internal/ext/{source,render,ats,knockout,compliance,deliver,track,evidence}`

```go
package ext

import (
	"context"
	"io"
	"time"
)

// ---- ingest side -----------------------------------------------------------------------------------------

// Fetcher is the only network handle a source adapter gets: the egress client's GET with the allow-list,
// IP denylist and proof capture already applied (mirrors internal/providers/providers.go:139 get()).
type Fetcher interface {
	Get(ctx context.Context, rawURL string, hdr map[string]string) (body []byte, proof CallProof, err error)
}

// CallProof is the evidence of one request (no bodies); same fields as providers.CallProof.
type CallProof struct {
	Host, ResolvedIP, TLSSubject, RespSHA256 string
	Status                                   int
	At                                       time.Time
}

// Limits are policy limits enforced by the limiter, not by the adapter (providers.Limits).
type Limits struct {
	MinInterval                   time.Duration
	MaxPages, MaxPerDay, PageSize int
}

// Query narrows one request.
type Query struct {
	Search, Category, Cursor string
	Limit                    int
}

// Batch is one request's result.
type Batch struct {
	Postings   []Posting
	Skipped    map[string]int // error class -> count
	Call       CallProof
	NextCursor string
	Raw        []byte
}

// SourceAdapter is the generalisation of providers.Provider (internal/providers/providers.go:111): any feed,
// API, JSON-LD page set or RSS. Adding a source = new package + Register line + allow-list host + provider row.
type SourceAdapter interface {
	Plugin
	Limits() Limits
	// Fetch performs exactly one request. It must not sleep, retry or loop over pages.
	Fetch(ctx context.Context, f Fetcher, q Query) (Batch, error)
	// LegalNotice returns the provider's terms text, re-read each run and hashed by the caller.
	LegalNotice(b Batch) string
}

// Enricher adds advisory tags (ESCO skills, scam flags). It may not change identity fields.
type Enricher interface {
	Plugin
	Enrich(p Posting) (tags []string, flags []Reason)
}

// ---- matching --------------------------------------------------------------------------------------------

// MatchRule contributes hits to the area classifier (data-driven rules stay in rules/area_rules.yaml; a Go
// MatchRule is for logic data cannot express).
type MatchRule interface {
	Plugin
	Match(p Posting) (area string, weight float64, why []Reason)
}

// ---- drafting / rendering ---------------------------------------------------------------------------------

// Slot addresses one prebuilt artifact (templates.SlotName, internal/templates/templates.go:115).
type Slot struct{ Kind, Area, Lang string }

// Artifact is a resolved file with its identity.
type Artifact struct {
	Slot   Slot
	Name   string
	SHA256 string
	Bytes  int64
	MIME   string
}

// FormatProfile describes the output constraints of a format (what a renderer promises, what a gate checks).
type FormatProfile struct {
	ID            string // "pdf_tagged", "pdfa_2b", "docx", "txt", "europass_xml", "cv_de"
	MIME          string
	MaxBytes      int64
	MaxPages      int
	RequiresTags  bool // /StructTreeRoot present
	RequiresXMP   bool
	LinkSchemes   []string // allowed annotation schemes, e.g. {"https","mailto"}
	FilenameTmpl  string   // e.g. "{candidate}_{kind}_{role}_{yyyymmdd}.pdf"
	LocaleHeading map[string][]string
}

// DocumentRenderer produces or converts an Artifact for a slot. In v1 the umbrella career engine builds the
// PDFs offline and job_seeker only verifies them; a Go renderer is only for derived formats (TXT, DOCX).
type DocumentRenderer interface {
	Plugin
	Profile() FormatProfile
	Render(ctx context.Context, src Artifact, w io.Writer) error
}

// Finding is one check result with a severity.
type Finding struct {
	Reason
	Severity string // "info" | "warn" | "block"
}

// ParserSimulator extracts text/structure the way an ATS parser would, so checks run on what a machine sees.
type ParserSimulator interface {
	Plugin
	Extract(ctx context.Context, a Artifact, r io.Reader) (ExtractedDoc, error)
}

// ExtractedDoc is the machine view of a document.
type ExtractedDoc struct {
	Text     string
	Headings []string
	Links    []Link
	Pages    int
	Tagged   bool
}

// Link is one annotation as a parser sees it.
type Link struct{ Scheme, Target, Label string }

// ATSProfile holds per-ATS (or per-provider) upload and parsing expectations and checks a document against them.
type ATSProfile interface {
	Plugin
	Applies(host string) bool
	Limits() UploadLimits
	Check(d ExtractedDoc, a Artifact) []Finding
}

// UploadLimits are the documented limits of an upload form.
type UploadLimits struct {
	Types    []string
	MaxBytes int64
	MaxFiles int
}

// Question is one knockout question found in a posting or form.
type Question struct {
	ID, Text, Kind string // kind: "work_auth" | "sponsorship" | "years" | "free"
}

// KnockoutHandler recognises a class of knockout questions and says what the human must confirm. It never
// answers on the human's behalf.
type KnockoutHandler interface {
	Plugin
	Recognize(p Posting) []Question
}

// ---- compliance / delivery ---------------------------------------------------------------------------------

// Stage names where a ComplianceGate is consulted.
type Stage string

const (
	StageIngest  Stage = "ingest"
	StageDraft   Stage = "draft"
	StageApprove Stage = "approve"
)

// ComplianceGate is a fail-closed policy check (GDPR retention, ToS/robots allow, AI-Act transparency).
type ComplianceGate interface {
	Plugin
	Check(ctx context.Context, st Stage, p Posting, s Settings) Decision
}

// DeliverabilityCheck verifies mail-sender hygiene before a mail channel may be enabled.
type DeliverabilityCheck interface {
	Plugin
	Check(ctx context.Context, domain string, f Fetcher) Decision
}

// ---- tracking / telemetry ----------------------------------------------------------------------------------

// StatusEvent is a tracker status change (tracking.status vocabulary in 0001_init.sql:107).
type StatusEvent struct {
	AppID  int64
	Status string
	At     time.Time
}

// TrackerHook observes status events. Errors are logged and never block the status write.
type TrackerHook interface {
	Plugin
	OnStatus(ctx context.Context, ev StatusEvent) error
}

// EvidenceSink receives metrics/evidence events. Implementations must be non-blocking and bounded.
type EvidenceSink interface {
	Plugin
	Emit(ev EvidenceEvent)
}

// EvidenceEvent carries ids and hashes only.
type EvidenceEvent struct {
	Name string
	Refs map[string]string
	At   time.Time
}
```

Design notes per port:

| Port | Notes that are not obvious from the signature |
|---|---|
| `SourceAdapter` | Same four methods as `providers.Provider` (`internal/providers/providers.go:111`) plus `Info()`. `Fetcher` hides `*egress.Client` so adapters cannot dial; the production `Fetcher` wraps `providers.get` (`providers.go:139`), which already records `CallProof`. One `Fetch` = one request: pagination, sleeping and retries stay in `ingest.Runner` (`internal/ingest/ingest.go:163`) and the limiter. |
| `StandardReader` | `Sniff` is cheap and lets sources that return mixed documents (RSS items with embedded JSON-LD) pick a reader without parsing twice. `ok=false, err=nil` means "not mine"; a reader that recognises the type but cannot parse returns an error, counted as `Skipped[schema]` (`providers.ClassSchema`). Absent data stays empty (`RemoteMode ""`), never defaulted: absent is `Unknown` downstream. |
| `Enricher` | Advisory only. Scam/ghost flags (T185) are `Reason`s stored on the posting; only a `ComplianceGate` or `EligibilityRule` can block. |
| `EligibilityRule` | Pure function of `(Posting, Profile, now)`. `Profile` is an interface of accessors returning `(value, ok)`; `ok=false` is how a rule learns the operator never configured something and must answer `Unknown`, not guess. Rules never see the raw overlay struct, so `Reason.Refs` cannot carry private values by construction. |
| `ScoreFeature` | Returns a value in `[0,1]`; the weight lives in `rules/score.yaml`, so a feature needs one weight line (22). Replaces inline component code in `ScorePosting` (`internal/match/score.go:97`). |
| `MatchRule` | Escape hatch for classifier logic YAML cannot express; most area rules stay data (`rules/area_rules.yaml`). |
| `FormatProfile` / `DocumentRenderer` | v1 PDFs come from the umbrella career engine (10). job_seeker verifies them through `ParserSimulator` + `ATSProfile` and renders derived formats (TXT now, DOCX later, T171). A `FormatProfile` is data first (YAML) and code only where a check needs it. |
| `ATSProfile` | `Applies(host)` matches the apply host (`postingRow.Host`, `internal/draft/service.go:111`). `Check` returns `Finding`s; only `Severity=="block"` can stop a handoff. Encodes Greenhouse/Lever/Workday quirks as data plus small checks (24 section 7). |
| `KnockoutHandler` | Recognition only. Answers are confirmed by the human and live in the private overlay (T177); a handler can never fill them. |
| `ApplicationChannel` | See section 7. |
| `ComplianceGate` | Consulted at `StageIngest`, `StageDraft`, `StageApprove`. A gate that cannot read its settings returns `Unknown`. |
| `DeliverabilityCheck` | SPF/DKIM/DMARC lookups go through `Fetcher` (DNS-over-HTTPS host on the allow-list); a mail channel can be enabled only after `Yes` (Part A 04). |
| `TrackerHook` | Called after the `tracking` row commits (`internal/tracker/tracker.go:87`). |
| `EvidenceSink` | Non-blocking; a full buffer drops and increments a counter, it never back-pressures the apply path. |

## 4. Evaluation and policy

```go
package ext

import (
	"fmt"
	"time"
)

// EvaluateEligibility runs rules in registry order and folds the result. A panicking rule yields Unknown with a
// reason (fail closed); an empty rule set yields Unknown (Combine).
func EvaluateEligibility(rules []EligibilityRule, p Posting, pr Profile, now time.Time) Outcome {
	trail := make([]Trail, 0, len(rules))
	for _, r := range rules {
		trail = append(trail, runRule(r, p, pr, now))
	}
	return Combine(trail)
}

func runRule(r EligibilityRule, p Posting, pr Profile, now time.Time) (t Trail) {
	i := r.Info()
	t.RuleID, t.Version = i.ID, i.Version
	defer func() {
		if rec := recover(); rec != nil {
			t.Decision = Decision{Verdict: Unknown, Reasons: []Reason{{Code: "plugin_panic", Refs: map[string]string{"plugin": i.ID, "kind": fmt.Sprintf("%T", rec)}}}}
		}
	}()
	t.Decision = r.Evaluate(p, pr, now)
	return t
}
```

```go
package ext

import "sort"

// Layer is one level of policy data (built-in defaults, policy.yaml, provider, ATS, country, operator,
// per-application). The YAML schema in 22 maps 1:1 onto this struct.
type Layer struct {
	Name     string
	Enabled  map[string]bool              // "kind/id" -> on/off
	Params   map[string]map[string]string // "kind/id" -> key -> value
	Strategy map[string]string            // port -> chosen strategy id
}

// Resolved is the effective policy with provenance, so every decision can say which layer decided.
type Resolved struct {
	Enabled  map[string]Sourced[bool]
	Strategy map[string]Sourced[string]
	Params   map[string]map[string]Sourced[string]
}

// Sourced is a value and the layer that set it.
type Sourced[V any] struct {
	Value V
	From  string
}

// Resolve merges layers from lowest to highest precedence; a later layer overrides an earlier one per key.
// locked lists "kind/id" keys that are fail-closed gates: they can be turned ON by any layer but turned OFF
// only by a layer named in offBy (normally the operator layer), never by a provider/ATS/country layer.
func Resolve(layers []Layer, locked map[string]bool, offBy string) Resolved {
	r := Resolved{Enabled: map[string]Sourced[bool]{}, Strategy: map[string]Sourced[string]{}, Params: map[string]map[string]Sourced[string]{}}
	for _, l := range layers {
		for k, v := range l.Enabled {
			if !v && locked[k] && l.Name != offBy {
				continue // a lower-authority layer cannot disable a locked gate
			}
			r.Enabled[k] = Sourced[bool]{v, l.Name}
		}
		for k, v := range l.Strategy {
			r.Strategy[k] = Sourced[string]{v, l.Name}
		}
		for id, kv := range l.Params {
			if r.Params[id] == nil {
				r.Params[id] = map[string]Sourced[string]{}
			}
			for k, v := range kv {
				r.Params[id][k] = Sourced[string]{v, l.Name}
			}
		}
	}
	return r
}

// Keys returns the sorted enabled-set keys (stable output for audit evidence).
func (r Resolved) Keys() []string {
	var ks []string
	for k, v := range r.Enabled {
		if v.Value {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	return ks
}
```

## 5. Mapping onto today's code

| New | Replaces / wraps | Where the change lands | Compatibility |
|---|---|---|---|
| `SourceAdapter` | `providers.Provider` | `providers.Provider` stays; a small shim type embeds it and adds `Info()` (24 section 2) | `ingest.Runner.Providers` keeps its type until all adapters moved |
| `EligibilityRule`s | gate blocks at `internal/match/score.go:141-205` | `ScorePosting` keeps its signature (`score.go:97`) and calls `ext.EvaluateEligibility`; reason codes map to the existing gate ids (`excluded_region`, `onsite_outside_allowed`, `blocked_company`, `salary_below_floor`) so stored `gates_json` and gold baselines do not change | golden test: old vs new gate ids over `internal/match/gold` |
| `ScoreFeature`s | inline components `score.go:110-175` | same function loops over active features; weights from `rules/score.yaml` | `score_version` bumps when the feature set changes |
| Approval-time re-evaluation | nothing | `draft.Service.checkLive` (`internal/draft/service.go:510`) calls the same `EvaluateEligibility` (T182) | new error `ErrGateRegressed` |
| `ApplicationChannel` | `HandoffComplete` (`service.go:665`) records the outcome only | after `live_approved` the console asks the active channel for a `Handoff` | no FSM change |
| `ComplianceGate`s | kill switch / caps in `checkLive`/`checkCaps` (`service.go:510-531`) | those two stay (core safety, 20 section 7); new gates wrap them | core checks first |
| Registry state | provider row `0001_init.sql:5` | new tables (23) | additive migration |

## 6. Plugin lifecycle

| Phase | Mechanism | Rule |
|---|---|---|
| Registration | `cmd/*/main.go` calls `Register` once per plugin; `Freeze()` before serving | duplicates, wrong kind, missing id/version, failed `Vet` abort start-up (fail loud) |
| Capability discovery | `Info().Caps` plus `Registry.IDs()`; read-only `GET /api/ext` and `jsctl ext list` print `(kind, id, version, enabled, caps, spec)` | no secrets in output |
| Versioning | `Info().Version` semver; MAJOR bump when verdicts for existing fixtures may change; stored with every verdict | golden suite pins version -> output; a version change without fixture review fails the gate |
| Enable/disable | `setting` keys `ext.<kind>.<id>.enabled` = `0|1` (23) and policy layers (22); operator-only, audited | locked gates cannot be disabled by provider/ATS/country layers (`Resolve`) |
| Config schema | each plugin package exports `ConfigSchema() []Param` (key, type, default, bounds); unknown keys in policy YAML are an error naming only the key path (as `LoadOverlay`, `internal/match/overlay.go:43`) | defaults in code, values in data |
| Deterministic ordering | `(Priority, ID)` | `Registry.Active` sorts, never map order |
| Conflict resolution | readers fill **empty** `Posting` fields only; the first reader (by order) returning `ok` wins and the rest are not asked; enrichers add tags/flags only | two writers to one field is a registration-time review item |
| Feature flags | `setting` `ext.flag.<name>`; flags gate **registration of experimental plugins** (default off) | a flag-off plugin is not in `Active` nor in discovery |
| Fail-closed defaults | `DefaultOn=false` for gate kinds the operator never configured; zero active gates = `Unknown` | shipped defaults enable reference gates explicitly in `rules/policy.yaml` (22) |
| Conformance | per-plugin suite + golden fixtures + mutation pair (25); `gates/verify_plugins.sh` | registering a plugin without them fails the gate |
| Removal | disable for one release, then delete with fixtures; stored verdict rows keep `plugin_id` | operator decision first (never silently remove a shipped component) |

Adding a standard therefore costs: one package, one `Register` line, one policy line, fixtures. Section 24 gives the exact edits.

## 7. The approval hard rule

Nothing is sent without human approval. The architecture enforces it in four independent places:

1. **Type**: `ApplicationChannel.Prepare` accepts only `Approved`, whose unexported field is set only by `NewApproved`, which refuses an empty approver or `approvedHash != draftHash` (FR-026 (2)(3)). A zero `Approved` is rejected (`TestNothingWithoutApproval`).
2. **Registration**: `ChannelVet` rejects any channel with `Caps["direct_send"]`. v1 has none. A future SMTP channel is `needs_human` (it composes a message the human sends) until the operator records a spec amendment to FR-027.
3. **Minting**: in the real tree `NewApproved` is called only from `draft.Service.Approve` (`internal/draft/service.go:432`) after the live checks; `gates/verify_nosend.sh` (already enumerates writers of `sent`) is extended to enumerate callers of `NewApproved` (25).
4. **Fixture**: SC-005's path-enumeration test gains the channel registry: every registered channel, enumerated from `Registry.IDs()`, must error on a zero `Approved`.

A `Handoff` is data for the human (link, notes). The only state change that follows is the existing human-confirmed `HandoffComplete` (`service.go:665`).

## 8. Reference implementations

| Port | Sketch package | Proves |
|---|---|---|
| `StandardReader` | `refs/jsonld` | schema.org `JobPosting`; absent location type stays empty; malformed JSON is an error; non-JobPosting is "not mine" |
| `EligibilityRule` | `refs/remotemode` | three-valued verdict, golden table, **mutant** `yesOnUnknown` is caught by the shared suite |
| `ScoreFeature` | `refs/keyword` | token-exact with synonyms: Django does not match Go, k8s matches Kubernetes |
| `ApplicationChannel` | `refs/handoff` | zero `Approved` refused, plain http refused, admission passes |

## 9. Part A standards to ports

| Part A topic | Port | Package | Priority (Part A) |
|---|---|---|---|
| schema.org JobPosting (01) | StandardReader | `reader/jsonld` | MUST |
| HR Open Standards (01) | StandardReader | `reader/hropen` | MAY |
| ESCO / O*NET skills tagging (01, 07) | Enricher | `reader/esco` | SHOULD |
| Greenhouse/Lever/Ashby public boards (02) | SourceAdapter | `source/{greenhouse,lever,ashby}` | SHOULD |
| RSS/Atom feeds (01) | SourceAdapter | `source/rss` | SHOULD |
| Workday and others without public API (02) | ATSProfile only (no adapter) | `ats/workday` | MAY |
| PDF links, tagged PDF, PDF/A (03) | FormatProfile + ATSProfile checks | `render/pdflinks`, `render/pdftag` | MUST/SHOULD |
| DOCX, TXT, Europass (03) | DocumentRenderer | `render/{docx,txt,europass}` | MAY |
| mailto/SMTP/OAuth (04) | ApplicationChannel (+ DeliverabilityCheck) | `channel/{mailto,smtp}` | SHOULD |
| SPF/DKIM/DMARC (04) | DeliverabilityCheck | `deliver/{spf,dkim,dmarc}` | SHOULD |
| GDPR retention, AI Act, ToS, robots (05) | ComplianceGate | `compliance/*` | MUST |
| Remote/work-auth/tz/language (06) | EligibilityRule | `eligibility/*` | MUST |
| Matching/ranking signals (07) | ScoreFeature, MatchRule | `match/*` | SHOULD |
| Observability, conformance (08) | EvidenceSink, conformance kit | `evidence/*`, `ext/conformance.go` | MUST |

## 10. Ports owned by the provider research

Two seams are designed in `docs/research/providers/` and are only **referenced** here:

| Seam | Defined in | How it composes with this part |
|---|---|---|
| `position.Resolver` (`Resolve`, `Merge`, `Split`) | [providers/03 section 4](../providers/03-canonical-position-model.md) | Runs after `StandardReader` output is stored as a Listing; it is a kernel-level service (not user-pluggable) because dedup identity must have one implementation. `Enricher` outputs (employer tenant ids) feed its hard keys. |
| Apply policy gate G9a-d (`apply_mode`, favourites, employer policy, `position_apply_lock`) | [providers/05 section 4](../providers/05-apply-policy-and-provider-selection.md) | Implemented as a `ComplianceGate` with `Stage=approve` plus the transactional lock in `Approve`; its `policy_sha` is stored beside this part's `ext_verdict` rows (23). Strategy selection of the *provider* is the `ApplyPolicy` strategy in 22 section 5. |

## Appendix A: compiled sketch output (captured 2026-11-24)

Command run from `docs/research/protocols/appendix-compiled-sketch/` (also saved in [appendix-sketch-output.txt](appendix-sketch-output.txt)):

```text
$ go version
go version go1.26.8 linux/amd64
$ gofmt -l .
$ go vet ./...
(no output, rc 0)
$ go test -count=1 -v ./... | grep -v "^=== RUN"
--- PASS: TestResolvePrecedenceAndLockedGate (0.00s)
--- PASS: TestEvaluateFailsClosed (0.00s)
--- PASS: TestRegistryOrderDuplicateAndDefaults (0.00s)
--- PASS: TestCombineUnknownIsNotAPass (0.00s)
--- PASS: TestApprovedCannotBeForged (0.00s)
--- PASS: TestChannelVetRejectsDirectSend (0.00s)
--- PASS: TestFavouritesFirst (0.00s)
PASS
ok  	example.com/jsk-sketch/ext	0.003s
--- PASS: TestNothingWithoutApproval (0.00s)
--- PASS: TestAdmission (0.00s)
PASS
ok  	example.com/jsk-sketch/refs/handoff	0.003s
--- PASS: TestReadGolden (0.00s)
PASS
ok  	example.com/jsk-sketch/refs/jsonld	0.003s
--- PASS: TestWordBoundaryTable (0.00s)
--- PASS: TestSubstringMutantWouldFail (0.00s)
PASS
ok  	example.com/jsk-sketch/refs/keyword	0.003s
--- PASS: TestConformance (0.00s)
    remotemode_test.go:49: mutant produced 1 conformance failures (expected >0)
--- PASS: TestMutantIsCaught (0.00s)
PASS
ok  	example.com/jsk-sketch/refs/remotemode	0.003s
$ go test -race -count=1 ./...
ok  	example.com/jsk-sketch/ext	1.027s
ok  	example.com/jsk-sketch/refs/handoff	1.028s
ok  	example.com/jsk-sketch/refs/jsonld	1.029s
ok  	example.com/jsk-sketch/refs/keyword	1.026s
ok  	example.com/jsk-sketch/refs/remotemode	1.018s
```

The line `mutant produced 1 conformance failures (expected >0)` is the mutation pairing working: the shared eligibility suite rejects the `yesOnUnknown` mutant. Files in the module: `ext/` (verdict, registry, ports, ports_more, evaluate, policy, strategy, conformance and 5 test files) and `refs/{jsonld,remotemode,keyword,handoff}`.
