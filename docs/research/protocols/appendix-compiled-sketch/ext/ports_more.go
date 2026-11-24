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
