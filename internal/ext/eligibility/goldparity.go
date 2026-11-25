package eligibility

import (
	"fmt"
	"sort"
	"time"

	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/match/gold"
)

// ParityReport is the SC-034 measurement over the gold rows (JSK-T244 step 3).
type ParityReport struct {
	Items, Rows                      int
	Snap                             Snapshot
	Hard                             Snapshot       // the approve-time HardGates shadow over the same rows
	GateFired                        map[string]int // legacy gate id -> rows on which it fired (non-vacuity)
	Unexercised                      []string       // legacy gates in the set that never fired: parity for them is NOT shown by this run
	HardMismatch                     int            // rows where HardGates differs from the overlay-derived gates ScorePosting collected (must be 0)
	OutcomeChanged                   int            // rows whose score differs with the shadow installed (must be 0)
	ByOverlay                        []OverlayRow   // legacy-vs-plugin blocked counts per overlay class
	ModesUsed, OverlaysUsed, Salarys int
	// JSK-T244 step 2: every row is also scored with the plugin as the decider (DeciderMode "plugin"); the Total, Band and
	// gate list must equal the legacy ones, and so must HardGates.
	PluginModeRows     int
	PluginModeMismatch int
	PluginHardMismatch int
}

// Equivalent is true when, row by row, the plugin blocked decision equals the legacy one (no permissive and no
// stricter row), no rule contradicted its legacy gate and the shadow never changed an outcome. Absent data is neutral
// in the plugin path, so Unknown no longer makes a row stricter; it is still counted in Snap.RuleUnknown.
func (r ParityReport) Equivalent() bool {
	return r.Rows > 0 && r.Snap.Postings == r.Rows && r.Hard.Postings == r.Rows && r.Snap.Panics == 0 && r.Hard.Panics == 0 && r.Snap.RuleDivergent == 0 && r.Snap.Permissive == 0 && r.Snap.Stricter == 0 && r.OutcomeChanged == 0 &&
		r.Hard.Permissive == 0 && r.Hard.Stricter == 0 && r.Hard.RuleDivergent == 0 && r.HardMismatch == 0 &&
		r.PluginModeRows == r.Rows && r.PluginModeMismatch == 0 && r.PluginHardMismatch == 0
}

// OverlayRow is the per-overlay breakdown: where plugin strictness (Unknown blocks) comes from.
type OverlayRow struct {
	Name                                         string
	Rows, LegacyBlocked, PluginBlocked, Stricter int
}

type salaryFacet struct {
	max      int
	cur, per string
}

// GoldParity scores every gold item (real title, body, tags, company, location) under a fixed grid of structured
// facets the gold corpus does not carry (remote mode, countries, salary) and overlays whose blocked companies and
// allowed cities are taken from the corpus itself, so each legacy gate can fire on real text. The facets are
// synthetic and labelled as such in the evidence; the text is real.
func GoldParity(r *match.Rules, items []gold.Item, now time.Time) ParityReport {
	return GoldParitySet(r, NewLegacySet(r), items, now)
}

// GoldParitySet is GoldParity with an explicit plugin set (the paired-mutation proof passes a weakened one).
func GoldParitySet(r *match.Rules, set *Set, items []gold.Item, now time.Time) ParityReport {
	rep := ParityReport{Items: len(items), GateFired: map[string]int{}}
	companies, cities := map[string]bool{}, map[string]bool{}
	for _, it := range items {
		if it.Company != "" {
			companies[it.Company] = true
		}
		if it.Location != "" {
			cities[it.Location] = true
		}
	}
	var cs, ls []string
	for c := range companies {
		cs = append(cs, c)
	}
	for l := range cities {
		ls = append(ls, l)
	}
	sort.Strings(cs)
	sort.Strings(ls)
	half := func(a []string, odd int) []string {
		var o []string
		for i, s := range a {
			if i%2 == odd {
				o = append(o, s)
			}
		}
		return o
	}
	overlays := []*match.Overlay{
		nil, {}, {Present: true},
		{Present: true, BlockedCompanies: half(cs, 0), EligibleCountries: []string{"DE", "AT"}, AllowedOnsiteCities: half(ls, 1),
			SalaryMin: 100000, SalaryCurrency: "EUR", SalaryPeriod: "year"},
		{Present: true, BlockedCompanies: half(cs, 1), EligibleCountries: []string{"US"}, AllowedOnsiteCities: half(ls, 0),
			SalaryMin: 5000, SalaryCurrency: "EUR", SalaryPeriod: "month"},
	}
	modes := []string{"", "remote", "hybrid", "onsite"}
	countries := [][]string{nil, {"DE"}, {"US", "FR"}}
	sals := []salaryFacet{{}, {5000, "EUR", "month"}, {200000, "EUR", "year"}, {40, "USD", "hour"}}
	rep.ModesUsed, rep.OverlaysUsed, rep.Salarys = len(modes), len(overlays), len(sals)

	rec := NewRecorder(set, nil)
	hrec := NewRecorder(hardSubset(set), nil)
	var plain match.Rules = *r // copy: legacy outcome without the hook
	plain.Shadow, plain.HardShadow = nil, nil
	hardShadowed := *r
	hardShadowed.Shadow, hardShadowed.HardShadow = nil, hrec.HardHook()
	shadowed := *r
	shadowed.Shadow = rec.Hook()
	pluginMode := *r
	pluginMode.Shadow, pluginMode.HardShadow = nil, nil
	pluginMode.DeciderMode = match.DeciderPlugin
	pluginMode.Decide, pluginMode.DecideHard = DeciderFunc(set), HardDeciderFunc(hardSubset(set))
	var id int64
	names := []string{"nil", "empty", "present-no-data", "full-A", "full-B"}
	for oi, ov := range overlays {
		before := rec.Snapshot()
		rows := 0
		for i, it := range items {
			for mi, mode := range modes {
				id++
				p := it.Posting(id)
				p.RemoteMode = mode
				p.Countries = countries[(i+mi)%len(countries)]
				sf := sals[(i+oi+mi)%len(sals)]
				p.SalaryMax, p.SalaryCur, p.SalaryPeriod = sf.max, sf.cur, sf.per
				p.IsDuplicate = (i+oi+mi)%7 == 0
				p.PostedAt = now.Add(-48 * time.Hour)
				m := r.Classify(p)
				want := plain.ScorePosting(p, m, ov, now)
				got := shadowed.ScorePosting(p, m, ov, now)
				rep.Rows++
				rows++
				if want.Total != got.Total || want.Band != got.Band || len(want.Gates) != len(got.Gates) {
					rep.OutcomeChanged++
				}
				for _, g := range want.Gates {
					rep.GateFired[g]++
				}
				// the approve-time re-check on the same row: shadowed, and it must equal the overlay-derived subset of the score gates
				pm := pluginMode.ScorePosting(p, m, ov, now)
				rep.PluginModeRows++
				if pm.Total != want.Total || pm.Band != want.Band || fmt.Sprint(pm.Gates) != fmt.Sprint(want.Gates) {
					rep.PluginModeMismatch++
				}
				if fmt.Sprint(pluginMode.HardGates(p, ov)) != fmt.Sprint(plain.HardGates(p, ov)) {
					rep.PluginHardMismatch++
				}
				hg := hardShadowed.HardGates(p, ov)
				plainHard := plain.HardGates(p, ov)
				if fmt.Sprint(hg) != fmt.Sprint(plainHard) || fmt.Sprint(hg) != fmt.Sprint(overlayGates(want.Gates)) {
					rep.HardMismatch++
				}
			}
		}
		after := rec.Snapshot()
		rep.ByOverlay = append(rep.ByOverlay, OverlayRow{names[oi], rows, after.LegacyBlocked - before.LegacyBlocked, after.PluginBlocked - before.PluginBlocked, after.Stricter - before.Stricter})
	}
	rep.Snap = rec.Snapshot()
	rep.Hard = hrec.Snapshot()
	for _, e := range set.Rules {
		if rep.GateFired[e.LegacyGate] == 0 {
			rep.Unexercised = append(rep.Unexercised, e.LegacyGate)
		}
	}
	return rep
}

// overlayGates filters a score's gates to the four HardGates reports, keeping score order.
func overlayGates(gates []string) []string {
	out := []string{}
	for _, g := range gates {
		if hardGateIDs[g] {
			out = append(out, g)
		}
	}
	return out
}
