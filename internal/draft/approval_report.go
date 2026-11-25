package draft

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The approval report (JSK-T187, FR-108, gap F-27): everything an operator should see before approving, assembled
// from the frozen draft only. It never changes anything; the console renders it, and an approval posted without the
// digest of the report currently on screen is refused (ErrReportRequired), so "approve" cannot be reached without
// the report having been produced for this exact draft.

// MaxDiffLines bounds each listed side of the text diff; the counts are always exact.
const MaxDiffLines = 40

// ErrReportRequired is returned when an approval did not carry the digest of the current approval report.
var ErrReportRequired = errors.New("draft: the approval report was not shown for this draft")

// ErrAreaConfirmRequired is returned when the approval report demands the operator confirm the CV area (the chosen
// area is not the best fit for the posting, or leads the runner-up by no more than the margin) and the approval did
// not carry that confirmation. Contract code area_confirmation_required (409).
var ErrAreaConfirmRequired = errors.New("draft: the CV area overlap must be confirmed before approval")

// LinkRow is one link annotation of a frozen file as a parser sees it.
type LinkRow struct {
	File   string `json:"file"` // cv | cover
	Scheme string `json:"scheme"`
	Target string `json:"target"`
	Label  string `json:"label"`
	OK     bool   `json:"ok"` // https or mailto with a target
}

// TextDiff compares the CV's extracted text with the CV of the most recent send.
type TextDiff struct {
	Available  bool     `json:"available"`
	Reason     string   `json:"reason,omitempty"` // no_previous_send | last_send_file_changed | last_send_unreadable
	AgainstApp int64    `json:"against_app,omitempty"`
	SentAt     string   `json:"sent_at,omitempty"`
	Added      []string `json:"added,omitempty"`
	Removed    []string `json:"removed,omitempty"`
	AddedN     int      `json:"added_n"`
	RemovedN   int      `json:"removed_n"`
}

// ApprovalReport is the schema approval_report.v1 payload.
type ApprovalReport struct {
	Schema   string         `json:"schema"`
	Company  string         `json:"company"`
	Date     string         `json:"date"` // the day the draft was frozen: the date a cover letter carries
	Links    []LinkRow      `json:"links"`
	Diff     TextDiff       `json:"diff"`
	Coverage CoverageReport `json:"coverage"`
	Digest   string         `json:"digest"`
}

func normLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// DiffLines compares two texts as multisets of whitespace-normalised lines (order-insensitive: a re-flowed page is
// not a change, a changed sentence is). Added are lines of cur beyond prev, Removed lines of prev beyond cur.
func DiffLines(prev, cur string) TextDiff {
	count := map[string]int{}
	for _, l := range normLines(prev) {
		count[l]++
	}
	var d TextDiff
	d.Available = true
	for _, l := range normLines(cur) {
		if count[l] > 0 {
			count[l]--
			continue
		}
		d.AddedN++
		if len(d.Added) < MaxDiffLines {
			d.Added = append(d.Added, l)
		}
	}
	rest := map[string]int{}
	for _, l := range normLines(cur) {
		rest[l]++
	}
	for _, l := range normLines(prev) {
		if rest[l] > 0 {
			rest[l]--
			continue
		}
		d.RemovedN++
		if len(d.Removed) < MaxDiffLines {
			d.Removed = append(d.Removed, l)
		}
	}
	return d
}

// ApprovalReport assembles the report for an application's frozen draft. Read-only. It needs the Coverage
// configuration with a Doc extractor; without one it returns ErrCoverageOff.
func (s *Service) ApprovalReport(ctx context.Context, appID int64) (ApprovalReport, error) {
	c := s.Coverage
	if c == nil || c.Dict == nil || c.Doc == nil {
		return ApprovalReport{}, ErrCoverageOff
	}
	q := s.DB.Read()
	a, err := loadApp(ctx, q, appID)
	if err != nil {
		return ApprovalReport{}, err
	}
	p, err := loadPosting(ctx, q, a.PostingID)
	if err != nil {
		return ApprovalReport{}, err
	}
	r := ApprovalReport{Schema: "approval_report.v1", Company: p.Company, Links: []LinkRow{}}
	fs, err := loadDraft(ctx, q, appID)
	if err != nil {
		return r, err
	}
	var cvText string
	for _, kind := range []string{"cv", "cover"} {
		f, _, ferr := s.File(ctx, appID, kind)
		if ferr != nil {
			return r, ferr
		}
		doc, derr := c.Doc(ctx, f)
		if derr != nil {
			return r, fmt.Errorf("approval report: %s not extractable", kind)
		}
		if kind == "cv" {
			cvText = doc.Text
		}
		for _, l := range doc.Links {
			r.Links = append(r.Links, LinkRow{File: kind, Scheme: l.Scheme, Target: l.Target, Label: l.Label,
				OK: (l.Scheme == "https" || l.Scheme == "mailto") && l.Target != ""})
		}
	}
	for _, d := range fs {
		if d.Kind == "cover" && len(d.CreatedAt) >= 10 {
			r.Date = d.CreatedAt[:10]
		}
	}
	r.Diff = s.diffAgainstLastSend(ctx, q, appID, cvText)
	if r.Coverage, err = s.CoverageReport(ctx, appID); err != nil {
		return r, err
	}
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	r.Digest = hex.EncodeToString(h[:])
	return r, nil
}

func (s *Service) diffAgainstLastSend(ctx context.Context, q rowQuerier, appID int64, cur string) TextDiff {
	var prevID int64
	var sentAt, slot, sha sql.NullString
	err := q.QueryRowContext(ctx, `SELECT a.id,a.sent_at,d.slot,d.content_sha256 FROM application a JOIN draft d ON d.application_id=a.id AND d.kind='cv'
		WHERE a.state IN ('sent','tracked') AND a.id<>? AND a.sent_at IS NOT NULL ORDER BY a.sent_at DESC, a.id DESC LIMIT 1`, appID).Scan(&prevID, &sentAt, &slot, &sha)
	if err != nil {
		return TextDiff{Reason: "no_previous_send"}
	}
	f, err := s.Tpl.ByName(slot.String)
	if err != nil || f.SHA256 != sha.String {
		return TextDiff{Reason: "last_send_file_changed", AgainstApp: prevID, SentAt: sentAt.String}
	}
	doc, err := s.Coverage.Doc(ctx, f)
	if err != nil {
		return TextDiff{Reason: "last_send_unreadable", AgainstApp: prevID, SentAt: sentAt.String}
	}
	d := DiffLines(doc.Text, cur)
	d.AgainstApp, d.SentAt = prevID, sentAt.String
	return d
}

// CheckReportSeen enforces that an approval carries the digest of the report currently produced for the draft.
// A Service without the report layer (tests, tools) requires nothing; a configured Service that cannot produce the
// report refuses (never a silent pass).
//
// areaConfirmed is the operator's explicit confirmation of the area overlap shown on the report: when the report
// demands one (Coverage.Area.ConfirmRequired) an approval without it is refused with ErrAreaConfirmRequired.
func (s *Service) CheckReportSeen(ctx context.Context, appID int64, digest string, areaConfirmed bool) error {
	if s.Coverage == nil {
		return nil
	}
	// the report belongs to the approval screen: for any other state Approve itself refuses with its own code
	if a, err := loadApp(ctx, s.DB.Read(), appID); err != nil || a.State != "awaiting_human_approval" {
		return nil
	}
	r, err := s.ApprovalReport(ctx, appID)
	if errors.Is(err, ErrApprovalHashMismatch) {
		// the frozen files no longer match the draft hash: Approve is the authority that refuses and withdraws the
		// approval path with its own code, so it must run; the report layer adds nothing it could weaken
		return nil
	}
	if err != nil || digest == "" || digest != r.Digest {
		return ErrReportRequired
	}
	if r.Coverage.Area.ConfirmRequired && !areaConfirmed {
		return ErrAreaConfirmRequired
	}
	return nil
}
