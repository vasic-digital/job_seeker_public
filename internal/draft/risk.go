package draft

import (
	"context"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Scam / ghost-posting risk flags (JSK-T185, F-17). Heuristics over the stored posting; no network, no model. Hard flags (a request
// for money, identity documents or bank data) block live approval; soft flags are shown at approval and never block. The repost-churn
// signal named in the backlog needs history this table does not keep and is NOT implemented.

type RiskInput struct {
	Title, Company, ApplyURL, Body string
	SalaryMin, SalaryMax           int
	SalaryCur, SalaryPeriod        string
}

type Risk struct {
	ID   string
	Hard bool
}

var (
	negation = regexp.MustCompile(`(?i)\b(never|no|not|without|don'?t|do not|won'?t|will not)\b[^.\n]{0,60}$`)

	rePay  = regexp.MustCompile(`(?i)\b(registration|application|training|processing|onboarding|security|administration|admin)\s+fee\b|\bpay\s+(?:a|an|the)?\s*(?:fee|deposit)\b|\bpay\s+to\s+apply\b|\bpurchase\s+(?:your\s+own\s+)?(?:laptop|equipment|software)\b|\bbuy\s+(?:your\s+own\s+)?(?:laptop|equipment)\b|\bdeposit\s+(?:is\s+)?required\b`)
	reID   = regexp.MustCompile(`(?i)\b(passport|national\s+id|id\s+card|driver'?s\s+licen[cs]e|social\s+security(?:\s+number)?|ssn|bank\s+account|bank\s+details|credit\s+card|card\s+number|iban)\b`)
	reMsg  = regexp.MustCompile(`(?i)\b(telegram|whatsapp|signal|wechat|skype)\b`)
	reMail = regexp.MustCompile(`(?i)[a-z0-9._%+-]+@(gmail|yahoo|outlook|hotmail|proton(?:mail)?|aol|gmx|mail|yandex|icloud)\.[a-z.]+`)
)

// atsHosts are applicant-tracking hosts whose domain legitimately differs from the company's.
var atsHosts = []string{"greenhouse.io", "lever.co", "ashbyhq.com", "workable.com", "smartrecruiters.com", "myworkdayjobs.com", "bamboohr.com",
	"recruitee.com", "teamtailor.com", "personio.de", "personio.com", "jobs.personio.de", "workday.com", "icims.com", "breezy.hr", "join.com", "himalayas.app", "remoteok.com"}

// matchesUnnegated reports a match of re in s that is not preceded (in its sentence) by a negation word.
func matchesUnnegated(re *regexp.Regexp, s string) bool {
	for _, loc := range re.FindAllStringIndex(s, -1) {
		start := strings.LastIndexAny(s[:loc[0]], ".\n") + 1
		if !negation.MatchString(s[start:loc[0]]) {
			return true
		}
	}
	return false
}

func companyTokens(c string) []string {
	var out []string
	for _, w := range strings.Fields(NormCompany(c)) {
		if len(w) >= 3 {
			out = append(out, strings.ReplaceAll(w, "-", ""))
		}
	}
	return out
}

func applyHostRelated(company, applyURL string) bool {
	u, err := url.Parse(strings.TrimSpace(applyURL))
	if err != nil || u.Hostname() == "" {
		return true // nothing to judge
	}
	h := strings.ToLower(u.Hostname())
	for _, a := range atsHosts {
		if h == a || strings.HasSuffix(h, "."+a) {
			return true
		}
	}
	toks := companyTokens(company)
	if len(toks) == 0 {
		return true // no company to compare against
	}
	hn := strings.ReplaceAll(h, "-", "")
	for _, t := range toks {
		if strings.Contains(hn, t) {
			return true
		}
	}
	return false
}

var hardCurrencies = map[string]bool{"USD": true, "EUR": true, "GBP": true, "CHF": true}
var annualFactor = map[string]float64{"year": 1, "month": 12, "week": 52, "day": 260, "hour": 2080}

// AssessRisk returns the risk flags of a posting, hard flags first, each id at most once.
func AssessRisk(in RiskInput) []Risk {
	var out []Risk
	text := in.Title + "\n" + in.Body
	if matchesUnnegated(rePay, text) {
		out = append(out, Risk{"pay_to_apply", true})
	}
	if matchesUnnegated(reID, text) {
		out = append(out, Risk{"id_or_bank_data_request", true})
	}
	if reMail.MatchString(text) {
		out = append(out, Risk{"freemail_contact", false})
	}
	if matchesUnnegated(reMsg, text) {
		out = append(out, Risk{"messaging_app_interview", false})
	}
	if in.ApplyURL != "" && !isMailto(in.ApplyURL) && !applyHostRelated(in.Company, in.ApplyURL) {
		out = append(out, Risk{"apply_host_unrelated", false})
	}
	if f, ok := annualFactor[in.SalaryPeriod]; ok && hardCurrencies[strings.ToUpper(in.SalaryCur)] {
		top := in.SalaryMax
		if in.SalaryMin > top {
			top = in.SalaryMin
		}
		if float64(top)*f >= 1_000_000 {
			out = append(out, Risk{"salary_implausible", false})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Hard && !out[j].Hard })
	return out
}

func (s *Service) riskOf(ctx context.Context, q rowQuerier, p postingRow) []Risk {
	return AssessRisk(RiskInput{Title: p.Title, Company: p.Company, ApplyURL: p.ApplyURL, Body: s.bodyOf(ctx, q, p.ID),
		SalaryMin: p.SalaryMin, SalaryMax: p.SalaryMax, SalaryCur: p.SalaryCur, SalaryPeriod: p.SalaryPeriod})
}

func riskIDs(rs []Risk, hardOnly bool) []any {
	var ids []any
	for _, r := range rs {
		if !hardOnly || r.Hard {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

// checkRisk refuses a live approval or handoff of a posting that carries a hard flag. Evidence is flag ids only (never posting text).
func (s *Service) checkRisk(ctx context.Context, q rowQuerier, p postingRow) *refusal {
	if ids := riskIDs(s.riskOf(ctx, q, p), true); len(ids) > 0 {
		return &refusal{err: ErrPostingRisk, reason: "refused_posting_risk", evidence: map[string]any{"flags": ids}}
	}
	return nil
}

// Advisories lists what the approval screen shows for an application: "risk:<flag>" for each risk flag (hard first) and
// "outside_business_hours" when the posting's local window is closed now.
func (s *Service) Advisories(ctx context.Context, appID int64) ([]string, error) {
	a, err := loadApp(ctx, s.DB.Read(), appID)
	if err != nil {
		return nil, err
	}
	p, err := loadPosting(ctx, s.DB.Read(), a.PostingID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range s.riskOf(ctx, s.DB.Read(), p) {
		out = append(out, "risk:"+r.ID)
	}
	if hoursOutside(p.TZHint, s.now()) {
		out = append(out, "outside_business_hours")
	}
	return out, nil
}

// DupView is one other application on the same Position as the one under review (JSK-T208, FR-089). It carries no personal data.
type DupView struct {
	AppID    int64
	Title    string
	Provider string
	State    string
}

// dupStates are the application states that make a second application to one Position a duplicate (same set as gate G9d).
const dupStates = `('draft_prepared','awaiting_human_approval','live_approved','sent','tracked')`

// Duplicates lists the OTHER open applications to the same Position as appID so the review page can warn before approval. It writes nothing
// and returns an empty list for a Listing with no Position. The acknowledgement itself is enforced by gate G9d at approval, never by this view.
func (s *Service) Duplicates(ctx context.Context, appID int64) ([]DupView, error) {
	rs, err := s.DB.Read().QueryContext(ctx, `SELECT o.id, po.title, v.name, o.state
		FROM application a JOIN posting me ON me.id=a.posting_id
		JOIN posting po ON po.position_id=me.position_id
		JOIN application o ON o.posting_id=po.id
		JOIN provider v ON v.id=po.provider_id
		WHERE a.id=? AND me.position_id IS NOT NULL AND o.id<>a.id AND o.state IN `+dupStates+` ORDER BY o.id LIMIT 20`, appID)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []DupView
	for rs.Next() {
		var d DupView
		if err := rs.Scan(&d.AppID, &d.Title, &d.Provider, &d.State); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rs.Err()
}
