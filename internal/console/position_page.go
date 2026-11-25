package console

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/api"
)

// JSK-T196 (FR-084): the Position page shows ONE merged view of a job with, for every merged field, which providers
// report which value. Listings stay individually reachable (/listings/{id}); /postings/{id} redirects to the Position.

type provValue struct {
	Value     string
	Providers []string // provider names reporting exactly this value, in listing order
	Merged    bool     // true for the value shown in the merged view
}

type fieldView struct {
	Key    string // title, company, location, remote, salary, posted
	Merged string // "" renders an em dash
	Values []provValue
}

type listingView struct {
	ID                                    int64
	Provider, Title, State, Decision, URL string
	Score                                 float64
	HasScore, Representative              bool
	Attribution                           bool
}

type positionView struct {
	ID                                           int64
	Employer, State, Seniority, Override, Reason string
	OverrideListing                              int64
	Fields                                       []fieldView
	Listings                                     []listingView
}

func salaryText(min, max sql.NullInt64, cur, period sql.NullString) string {
	if !min.Valid && !max.Valid {
		return ""
	}
	var s string
	switch {
	case min.Valid && max.Valid && min.Int64 != max.Int64:
		s = fmt.Sprintf("%d-%d", min.Int64, max.Int64)
	case min.Valid:
		s = strconv.FormatInt(min.Int64, 10)
	default:
		s = strconv.FormatInt(max.Int64, 10)
	}
	if cur.String != "" {
		s += " " + cur.String
	}
	if period.String != "" {
		s += "/" + period.String
	}
	return s
}

// buildFields merges per-listing values: the merged value is the representative's when it has one, else the first
// non-empty in listing order; provenance lists every provider per distinct value. Pure, so it is tested directly.
func buildFields(keys []string, vals map[string][]string, providers []string, rep int) []fieldView {
	var out []fieldView
	for _, k := range keys {
		fv := fieldView{Key: k}
		idx := map[string]int{}
		for i, v := range vals[k] {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			j, ok := idx[v]
			if !ok {
				j = len(fv.Values)
				idx[v] = j
				fv.Values = append(fv.Values, provValue{Value: v})
			}
			if !contains(fv.Values[j].Providers, providers[i]) {
				fv.Values[j].Providers = append(fv.Values[j].Providers, providers[i])
			}
		}
		pick := ""
		if rep >= 0 && rep < len(vals[k]) {
			pick = strings.TrimSpace(vals[k][rep])
		}
		if pick == "" && len(fv.Values) > 0 {
			pick = fv.Values[0].Value
		}
		fv.Merged = pick
		for i := range fv.Values {
			fv.Values[i].Merged = fv.Values[i].Value == pick
		}
		out = append(out, fv)
	}
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

var errNoPosition = fmt.Errorf("no position")

// loadPosition reads one Position (following merged_into, bounded) and its listings.
func (c *Console) loadPosition(ctx context.Context, id int64) (*positionView, int64, error) {
	db := c.DB.Read()
	for hops := 0; hops < 16; hops++ {
		var st, sen, ovr string
		var emp, reason, name sql.NullString
		var mergedInto, rep, ovl sql.NullInt64
		err := db.QueryRowContext(ctx, `SELECT p.state, coalesce(p.seniority,''), p.apply_override, p.apply_override_reason, p.apply_override_listing, p.merged_into, p.representative_listing_id, e.display_name, coalesce(e.name_norm,'')
			FROM position p LEFT JOIN employer e ON e.id=p.employer_id WHERE p.id=?`, id).Scan(&st, &sen, &ovr, &reason, &ovl, &mergedInto, &rep, &emp, &name)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, 0, errNoPosition
			}
			return nil, 0, err
		}
		if st == "merged" && mergedInto.Valid {
			return nil, mergedInto.Int64, nil
		}
		pv := &positionView{ID: id, Employer: emp.String, State: st, Seniority: sen, Override: ovr, Reason: reason.String, OverrideListing: ovl.Int64}
		rs, err := db.QueryContext(ctx, `SELECT p.id, pr.name, p.title, coalesce(p.company,''), coalesce(p.location_raw,''), coalesce(p.remote_mode,''),
				p.salary_min, p.salary_max, p.salary_cur, p.salary_period, coalesce(p.posted_at,''), coalesce(p.source_url,''), p.state,
				coalesce((SELECT decision FROM position_link l WHERE l.listing_id=p.id AND l.is_current=1 LIMIT 1),''),
				(SELECT score FROM position_link l WHERE l.listing_id=p.id AND l.is_current=1 LIMIT 1),
				coalesce((SELECT attribution_required FROM provider_profile pp WHERE pp.provider_id=p.provider_id),1)
			FROM posting p JOIN provider pr ON pr.id=p.provider_id WHERE p.position_id=? ORDER BY (p.state='expired'), p.id`, id)
		if err != nil {
			return nil, 0, err
		}
		vals := map[string][]string{}
		var provs []string
		repIdx := -1
		for rs.Next() {
			var l listingView
			var title, company, loc, remote, posted, src string
			var smin, smax sql.NullInt64
			var scur, sper sql.NullString
			var sc sql.NullFloat64
			var attr int
			if err := rs.Scan(&l.ID, &l.Provider, &title, &company, &loc, &remote, &smin, &smax, &scur, &sper, &posted, &src, &l.State, &l.Decision, &sc, &attr); err != nil {
				rs.Close()
				return nil, 0, err
			}
			l.Title, l.Score, l.HasScore, l.Attribution = title, sc.Float64, sc.Valid, attr == 1
			if strings.HasPrefix(src, "https://") {
				l.URL = src // https only: anything else is never a link
			}
			if rep.Valid && rep.Int64 == l.ID {
				l.Representative, repIdx = true, len(pv.Listings)
			}
			vals["title"] = append(vals["title"], title)
			vals["company"] = append(vals["company"], company)
			vals["location"] = append(vals["location"], loc)
			vals["remote"] = append(vals["remote"], remote)
			vals["salary"] = append(vals["salary"], salaryText(smin, smax, scur, sper))
			vals["posted"] = append(vals["posted"], posted)
			provs = append(provs, l.Provider)
			pv.Listings = append(pv.Listings, l)
		}
		err = rs.Err()
		rs.Close()
		if err != nil {
			return nil, 0, err
		}
		pv.Fields = buildFields([]string{"title", "company", "location", "remote", "salary", "posted"}, vals, provs, repIdx)
		return pv, 0, nil
	}
	return nil, 0, errNoPosition
}

func (c *Console) positionPage(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	c.renderPosition(g, id, http.StatusOK, "")
}

// positionErr re-renders the Position page with a refusal code (the override form's errors).
func (c *Console) positionErr(g *gin.Context, id int64, status int, code string) {
	c.renderPosition(g, id, status, code)
}

func (c *Console) renderPosition(g *gin.Context, id int64, status int, code string) {
	pv, to, err := c.loadPosition(g.Request.Context(), id)
	switch {
	case err == errNoPosition:
		api.WriteError(g.Writer, g.Request, http.StatusNotFound, api.CodeNotFound, "error.not_found", nil)
		return
	case err != nil:
		api.WriteError(g.Writer, g.Request, http.StatusInternalServerError, api.CodeInternal, "error.internal", nil)
		return
	case pv == nil:
		g.Redirect(http.StatusFound, fmt.Sprintf("/positions/%d", to))
		return
	}
	p := c.newPage(g, "position.title", "/")
	p.Pos, p.Error, p.OverrideModes = pv, code, []string{"inherit", "all", "single", "none"}
	c.render(g, status, "position", p)
}

// listing is the single-listing page (the former /postings/{id} body); /postings/{id} now redirects to the Position.
func (c *Console) listing(g *gin.Context) { c.job(g) }

// postingEntry redirects a posting that has a Position to that Position's page; a legacy posting keeps its own page.
func (c *Console) postingEntry(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	var pos sql.NullInt64
	if err := c.DB.Read().QueryRowContext(g.Request.Context(), `SELECT position_id FROM posting WHERE id=?`, id).Scan(&pos); err == nil && pos.Valid {
		g.Redirect(http.StatusFound, fmt.Sprintf("/positions/%d#listing-%d", pos.Int64, id))
		return
	}
	c.job(g)
}
