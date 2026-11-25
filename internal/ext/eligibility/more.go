package eligibility

import (
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// WorkAuth: the operator must be authorised to work in at least one country the posting accepts. A posting that
// names no country gives the rule nothing to decide on (Unknown, never an assumed Yes).
type WorkAuth struct{}

func (WorkAuth) Info() ext.Info {
	return ext.Info{ID: "work_auth", Kind: "eligibility", Version: "1.0.0", Priority: 15, Spec: "FR-096; docs/research/protocols/06 section 2"}
}

func (WorkAuth) Evaluate(p providers.Posting, pr ext.Profile, _ time.Time) ext.Decision {
	if pr == nil {
		return noData("work_auth", "work_auth_countries")
	}
	auth, ok := pr.WorkAuthCountries()
	if !ok {
		return noData("work_auth", "work_auth_countries")
	}
	if len(p.Countries) == 0 {
		return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "work_auth_unknown", Refs: map[string]string{"field": "posting_countries"}}}}
	}
	for _, pc := range p.Countries {
		for _, a := range auth {
			if strings.EqualFold(pc, a) {
				return ext.Decision{Verdict: ext.Yes}
			}
		}
	}
	return no("work_auth_region_excluded", nil)
}

// Timezone: for a remote posting with a UTC-offset hint, the overlap between the posting's local working hours
// and the operator's UTC window must reach MinOverlap hours. Onsite and hybrid postings are out of scope (Yes).
type Timezone struct {
	MinOverlap       float64 // hours, default 4
	HoursFrom, Until float64 // posting local working hours, default 9 and 17
}

func (Timezone) Info() ext.Info {
	return ext.Info{ID: "timezone", Kind: "eligibility", Version: "1.0.0", Priority: 25, Spec: "FR-096; docs/research/protocols/06 section 3"}
}

func (t Timezone) Evaluate(p providers.Posting, pr ext.Profile, _ time.Time) ext.Decision {
	switch strings.ToLower(p.RemoteMode) {
	case "onsite", "hybrid":
		return ext.Decision{Verdict: ext.Yes}
	case "remote":
	default:
		return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "remote_mode_unknown"}}}
	}
	if pr == nil {
		return noData("timezone", "tz_window")
	}
	ws, we, ok := pr.TZWindow()
	if !ok {
		return noData("timezone", "tz_window")
	}
	off, ok := match.ParseTZ(p.TZHint)
	if !ok {
		return noData("timezone", "posting_tz_hint")
	}
	min, from, until := t.MinOverlap, t.HoursFrom, t.Until
	if min <= 0 {
		min = 4
	}
	if from == 0 && until == 0 {
		from, until = 9, 17
	}
	ps, pe := from-off, until-off // posting hours in UTC
	we2 := float64(we)
	if we2 < float64(ws) {
		we2 += 24 // an operator window that wraps past midnight UTC
	}
	hi := 0.0 // overlap on the 24 h circle: try the window shifted by whole days
	for k := -2; k <= 2; k++ {
		lo, up := float64(ws)+24*float64(k), we2+24*float64(k)
		if ps > lo {
			lo = ps
		}
		if pe < up {
			up = pe
		}
		if up > lo {
			hi += up - lo
		}
	}
	if hi < min {
		return no("tz_overlap_insufficient", nil)
	}
	return ext.Decision{Verdict: ext.Yes}
}

// Language: the posting's detected language must be one the operator can work in. A missing or low-confidence
// detection is Unknown (match.LangMinConfidence), never a guess.
type Language struct{}

func (Language) Info() ext.Info {
	return ext.Info{ID: "language", Kind: "eligibility", Version: "1.0.0", Priority: 35, Spec: "FR-105; BCP 47 primary subtag"}
}

func (Language) Evaluate(p providers.Posting, pr ext.Profile, _ time.Time) ext.Decision {
	if pr == nil {
		return noData("language", "languages")
	}
	langs, ok := pr.Languages()
	if !ok {
		return noData("language", "languages")
	}
	if !match.LangTrusted(p.Lang, p.LangConf) {
		return noData("language", "posting_lang")
	}
	pl := strings.ToLower(strings.SplitN(p.Lang, "-", 2)[0])
	for _, l := range langs {
		if strings.EqualFold(strings.SplitN(l, "-", 2)[0], pl) {
			return ext.Decision{Verdict: ext.Yes}
		}
	}
	return no("lang_not_supported", map[string]string{"lang": pl})
}
