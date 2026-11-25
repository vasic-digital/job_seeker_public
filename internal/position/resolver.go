package position

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
)

//go:embed resolver_weights.tsv
var weightsTSV string

// Params are the resolver tunables loaded from data.
type Params struct {
	Version        string
	TAuto, TReview float64
	DateWindowDays float64
	WTitle         float64
	WDate, WSalary float64
	WApplyHost     float64
	WBodyMH        float64
}

// LoadParams parses a weights table.
func LoadParams(tsv string) (Params, error) {
	m := map[string]string{}
	for _, f := range rows(tsv) {
		if len(f) != 2 {
			return Params{}, fmt.Errorf("position: bad params row %q", f)
		}
		m[f[0]] = f[1]
	}
	num := func(k string) float64 {
		v, err := strconv.ParseFloat(m[k], 64)
		if err != nil {
			panic(fmt.Sprintf("position: params %q: %v", k, err))
		}
		return v
	}
	p := Params{Version: m["version"], TAuto: num("t_auto"), TReview: num("t_review"), DateWindowDays: num("date_window_days"),
		WTitle: num("w_title"), WDate: num("w_date"), WSalary: num("w_salary"), WApplyHost: num("w_apply_host"), WBodyMH: num("w_body_mh")}
	if p.Version == "" || p.TReview >= p.TAuto {
		return Params{}, errors.New("position: params need a version and t_review < t_auto")
	}
	return p, nil
}

// DefaultParams are the embedded parameters.
func DefaultParams() Params {
	p, err := LoadParams(weightsTSV)
	if err != nil {
		panic(err)
	}
	return p
}

// Listing is the projection of a posting row the resolver needs.
type Listing struct {
	ID, ProviderID              int64
	ProviderJobID               string
	Title, Company, LocationRaw string
	RemoteMode                  string
	Countries                   []string
	ApplyURL, SourceURL         string
	PostedAt                    time.Time
	SalaryMin, SalaryMax        int64
	FirstSeen, LastSeen         string
	State                       string
	EmployerID, PositionID      int64
	ATS                         ATSIdentity
	ApplyCanon                  string
	Sig                         *Signature // optional body signature (JSK-T193); nil = feature absent
	title                       TitleInfo
	scope                       RemoteScope
	employerKind                string
}

// Signature is a body-derived signature used for f_body_mh (see minhash.go).
type Signature struct{ MinHash []uint32 }

// Outcome is what Resolve decided.
type Outcome struct {
	Position int64
	Decision string // auto|new|queued|existing
	Score    float64
	Reason   string
	Features map[string]float64
}

// Resolver attaches Listings to Positions. It is pure given (store state, Params).
type Resolver struct {
	P     Params
	Now   func() time.Time
	Actor string // audit actor for automatic decisions (default "system")
	// Keys opens the sealed body signatures (JSK-T193). Nil means f_body_mh is absent for every pair, which is neutral (never an auto-merge on its own).
	Keys *crypto.Keyring
}

func (r *Resolver) now() string {
	if r.Now != nil {
		return r.Now().UTC().Format(time.RFC3339)
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func (r *Resolver) actor() string {
	if r.Actor != "" {
		return r.Actor
	}
	return "system"
}

func parseTS(s string) time.Time {
	for _, l := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02"} {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func loadListing(ctx context.Context, tx *sql.Tx, id int64) (Listing, error) {
	var l Listing
	var company, loc, mode, countries, apply, source, posted, state, ek, ak, at, aj, ac sql.NullString
	var smin, smax, emp, pos sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT p.id, p.provider_id, p.provider_job_id, p.title, p.company, p.location_raw, p.remote_mode, p.countries_json,
		p.apply_url, p.source_url, p.posted_at, p.salary_min, p.salary_max, p.first_seen, p.last_seen, p.state, p.employer_id, p.position_id, e.kind, p.ats_kind, p.ats_tenant, p.ats_job_id, p.apply_url_canon
		FROM posting p LEFT JOIN employer e ON e.id=p.employer_id WHERE p.id=?`, id).
		Scan(&l.ID, &l.ProviderID, &l.ProviderJobID, &l.Title, &company, &loc, &mode, &countries, &apply, &source, &posted, &smin, &smax, &l.FirstSeen, &l.LastSeen, &state, &emp, &pos, &ek, &ak, &at, &aj, &ac)
	if err != nil {
		return l, err
	}
	l.Company, l.LocationRaw, l.RemoteMode, l.ApplyURL, l.SourceURL, l.State = company.String, loc.String, mode.String, apply.String, source.String, state.String
	l.SalaryMin, l.SalaryMax, l.EmployerID, l.PositionID, l.employerKind = smin.Int64, smax.Int64, emp.Int64, pos.Int64, ek.String
	if countries.Valid {
		_ = json.Unmarshal([]byte(countries.String), &l.Countries)
	}
	if posted.Valid {
		l.PostedAt = parseTS(posted.String)
	}
	l.ATS = ATSIdentity{Kind: ak.String, Tenant: at.String, JobID: aj.String}
	l.ApplyCanon = ac.String
	l.title = NormTitle(l.Title)
	l.scope = ParseRemoteScope(l.LocationRaw, l.RemoteMode, l.Countries)
	return l, nil
}

// atsHosts are never employer domains (they host many employers).
var boardHost = func() map[string]bool {
	m := map[string]bool{}
	for _, h := range []string{"greenhouse.io", "lever.co", "ashbyhq.com", "workable.com", "smartrecruiters.com", "remotive.com", "remoteok.com", "remoteok.io",
		"himalayas.app", "weworkremotely.com", "linkedin.com", "indeed.com", "recruitee.com", "personio.de", "personio.com", "teamtailor.com"} {
		m[h] = true
	}
	return m
}()

func hostIsBoard(raw string) bool {
	c, err := CanonURL(raw)
	if err != nil {
		return true
	}
	h := strings.TrimPrefix(strings.SplitN(strings.TrimPrefix(strings.TrimPrefix(c.URL, "https://"), "http://"), "/", 2)[0], "www.")
	parts := strings.Split(h, ".")
	for i := range parts {
		if boardHost[strings.Join(parts[i:], ".")] {
			return true
		}
	}
	return false
}

// resolveEmployer implements steps 1, 3 and 4 of section 2.1 (ats_tenant alias, exact name_norm, name alias, else create). Domain aliases are
// human-added only and fuzzy matching is not performed (it would never attach silently anyway), so an unseen spelling creates a new employer.
func (r *Resolver) resolveEmployer(ctx context.Context, tx *sql.Tx, l *Listing) (int64, error) {
	if l.Company == "" {
		return 0, nil
	}
	norm := NormEmployer(l.Company)
	if norm == "" {
		return 0, nil
	}
	var id int64
	if l.ATS.Kind != "" {
		err := tx.QueryRowContext(ctx, `SELECT employer_id FROM employer_alias WHERE kind='ats_tenant' AND value=?`, l.ATS.Kind+":"+l.ATS.Tenant).Scan(&id)
		if err == nil {
			return id, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}
	err := tx.QueryRowContext(ctx, `SELECT id FROM employer WHERE name_norm=?`, norm).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT employer_id FROM employer_alias WHERE kind='name' AND value=?`, norm).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		res, e := tx.ExecContext(ctx, `INSERT INTO employer(name_norm, display_name, created_at) VALUES(?,?,?)`, norm, l.Company, r.now())
		if e != nil {
			return 0, e
		}
		id, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	if l.ATS.Kind != "" {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO employer_alias(employer_id,kind,value,source) VALUES(?, 'ats_tenant', ?, 'auto')`, id, l.ATS.Kind+":"+l.ATS.Tenant); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func pair(a, b int64) (int64, int64) {
	if a < b {
		return a, b
	}
	return b, a
}

func (r *Resolver) constraint(ctx context.Context, tx *sql.Tx, a, b int64) (string, error) {
	x, y := pair(a, b)
	var k string
	err := tx.QueryRowContext(ctx, `SELECT kind FROM position_constraint WHERE a_listing=? AND b_listing=?`, x, y).Scan(&k)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return k, err
}

type cand struct {
	Listing
	pos int64
}

func (r *Resolver) candidates(ctx context.Context, tx *sql.Tx, l Listing, where string, args ...any) ([]cand, error) {
	rs, err := tx.QueryContext(ctx, `SELECT p.id FROM posting p JOIN position pos ON pos.id=p.position_id
		WHERE p.id<>? AND pos.state<>'merged' AND p.state<>'expired' AND `+where+` ORDER BY p.id`, append([]any{l.ID}, args...)...)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rs.Next() {
		var id int64
		if err := rs.Scan(&id); err != nil {
			rs.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rs.Close()
	var out []cand
	for _, id := range ids {
		c, err := loadListing(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		c.Sig = LoadSignature(ctx, tx, r.Keys, id)
		out = append(out, cand{c, c.PositionID})
	}
	return out, rs.Err()
}

func lev(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func titleRatio(a, b string) float64 {
	x, y := []rune(a), []rune(b)
	if len(x) == 0 && len(y) == 0 {
		return 1
	}
	return 1 - float64(lev(x, y))/float64(max(len(x), len(y)))
}

func jaccard(a, b []string) float64 {
	sa := map[string]bool{}
	for _, x := range a {
		sa[x] = true
	}
	inter, union := 0, len(sa)
	seen := map[string]bool{}
	for _, y := range b {
		if seen[y] {
			continue
		}
		seen[y] = true
		if sa[y] {
			inter++
		} else {
			union++
		}
	}
	if union == 0 {
		return 1
	}
	return float64(inter) / float64(union)
}

func round4(f float64) float64 { return math.Round(f*10000) / 10000 }

// Score computes the soft score s, the feature vector and the hard-guard verdict for a pair of Listings.
// guard is "" when no hard guard fires, otherwise the first guard name that forces "not auto".
func (r *Resolver) Score(a, b Listing) (s float64, f map[string]float64, guard string) {
	f = map[string]float64{}
	var num, den float64
	add := func(name string, w, v float64) {
		f[name] = round4(v)
		num += w * v
		den += w
	}
	ratio := titleRatio(a.title.Norm, b.title.Norm)
	tokJ := jaccard(strings.Fields(a.title.Norm), strings.Fields(b.title.Norm))
	f["f_tokens"] = round4(tokJ)
	add("f_title", r.P.WTitle, math.Max(ratio, tokJ)) // robust to word order; equals the legacy title ratio when the ratio is larger
	if !a.PostedAt.IsZero() && !b.PostedAt.IsZero() {
		d := math.Abs(a.PostedAt.Sub(b.PostedAt).Hours() / 24)
		add("f_date", r.P.WDate, math.Exp(-d/30))
	}
	if a.SalaryMax > 0 && b.SalaryMax > 0 {
		v := 0.0
		if a.SalaryMin <= b.SalaryMax && b.SalaryMin <= a.SalaryMax {
			v = 1
		}
		add("f_salary", r.P.WSalary, v)
	}
	if a.ApplyCanon != "" && b.ApplyCanon != "" && !hostIsBoard(a.ApplyURL) && !hostIsBoard(b.ApplyURL) {
		v := 0.0
		if hostOf(a.ApplyCanon) == hostOf(b.ApplyCanon) {
			v = 1
		}
		add("f_apply_host", r.P.WApplyHost, v)
	}
	if a.Sig != nil && b.Sig != nil && len(a.Sig.MinHash) == len(b.Sig.MinHash) && len(a.Sig.MinHash) > 0 {
		eq := 0
		for i := range a.Sig.MinHash {
			if a.Sig.MinHash[i] == b.Sig.MinHash[i] {
				eq++
			}
		}
		add("f_body_mh", r.P.WBodyMH, float64(eq)/float64(len(a.Sig.MinHash)))
	}
	// guard-only features (stored for explainability; not part of s)
	f["f_seniority"] = 0.5
	if a.title.Seniority != "" && b.title.Seniority != "" {
		f["f_seniority"] = 1
		if a.title.Seniority != b.title.Seniority {
			f["f_seniority"] = 0
		}
	}
	f["f_tech"] = 0.5
	if len(a.title.Tech) > 0 && len(b.title.Tech) > 0 {
		f["f_tech"] = round4(jaccard(a.title.Tech, b.title.Tech))
	}
	f["f_loc"] = LocCompat(a.scope, b.scope)
	if den > 0 {
		s = round4(num / den)
	}
	f["s"] = s
	switch {
	case SeniorityMismatch(a.title.Seniority, b.title.Seniority):
		guard = "seniority"
	case f["f_loc"] == 0:
		guard = "location"
	case f["f_tech"] == 0:
		guard = "technology"
	case a.EmployerID == 0 || b.EmployerID == 0 || a.EmployerID != b.EmployerID:
		guard = "employer"
	case a.ATS.JobID != "" && b.ATS.JobID != "" && a.ATS.Kind == b.ATS.Kind && a.ATS.Tenant == b.ATS.Tenant && a.ATS.JobID != b.ATS.JobID:
		guard = "ats_identity_differs"
	}
	return s, f, guard
}

// jobSpecificURL reports whether a canonical apply URL points at ONE job: at least two path segments or a query (identity parameters such as
// gh_jid survive canonicalisation). A bare careers page is shared by every opening of the employer and is never an identity.
func jobSpecificURL(canon string) bool {
	rest := strings.TrimPrefix(strings.TrimPrefix(canon, "https://"), "http://")
	if strings.Contains(rest, "?") {
		return true
	}
	parts := strings.Split(rest, "/")
	n := 0
	for _, p := range parts[1:] {
		if p != "" {
			n++
		}
	}
	return n >= 2
}

func hostOf(canon string) string {
	return strings.SplitN(strings.TrimPrefix(strings.TrimPrefix(canon, "https://"), "http://"), "/", 2)[0]
}

func featJSON(f map[string]float64, extra map[string]any) string {
	m := map[string]any{}
	for k, v := range f {
		m[k] = v
	}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m) // map keys sorted: deterministic
	return string(b)
}

// Resolve attaches listing id to an existing Position, starts a new one, or queues a merge candidate. It is idempotent: a Listing that
// already has a current position_link is returned unchanged. All writes happen in tx together with their audit rows (invariant 6).
func (r *Resolver) Resolve(ctx context.Context, tx *sql.Tx, id int64) (Outcome, error) {
	l, err := loadListing(ctx, tx, id)
	if err != nil {
		return Outcome{}, err
	}
	l.Sig = LoadSignature(ctx, tx, r.Keys, id)
	var cur int64
	switch err := tx.QueryRowContext(ctx, `SELECT position_id FROM position_link WHERE listing_id=? AND is_current=1`, id).Scan(&cur); {
	case err == nil:
		return Outcome{Position: cur, Decision: "existing"}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return Outcome{}, err
	}
	if err := r.identify(ctx, tx, &l); err != nil {
		return Outcome{}, err
	}

	type target struct {
		pos    int64
		score  float64
		reason string
		f      map[string]float64
	}
	targets := map[int64]*target{}
	vetoed := map[int64]bool{}
	note := func(c cand, s float64, reason string, f map[string]float64) {
		if vetoed[c.pos] {
			return
		}
		if t, ok := targets[c.pos]; !ok || s > t.score {
			targets[c.pos] = &target{c.pos, s, reason, f}
		}
	}
	// 1. hard keys, honouring human constraints first
	var hard []cand
	if l.ATS.JobID != "" {
		cs, err := r.candidates(ctx, tx, l, `p.ats_kind=? AND p.ats_tenant=? AND p.ats_job_id=?`, l.ATS.Kind, l.ATS.Tenant, l.ATS.JobID)
		if err != nil {
			return Outcome{}, err
		}
		for _, c := range cs {
			hard = append(hard, c)
			note(c, 1.0, "ats_identity", map[string]float64{"ats_identity": 1})
		}
	}
	if l.ApplyCanon != "" && jobSpecificURL(l.ApplyCanon) && !hostIsBoard(l.ApplyURL) {
		cs, err := r.candidates(ctx, tx, l, `p.apply_url_canon=? AND p.provider_id<>?`, l.ApplyCanon, l.ProviderID)
		if err != nil {
			return Outcome{}, err
		}
		for _, c := range cs {
			note(c, 0.99, "apply_url", map[string]float64{"apply_url": 1})
		}
	}
	all, err := r.allConstrained(ctx, tx, id)
	if err != nil {
		return Outcome{}, err
	}
	for oid, kind := range all {
		var pos sql.NullInt64
		_ = tx.QueryRowContext(ctx, `SELECT position_id FROM posting WHERE id=?`, oid).Scan(&pos)
		if !pos.Valid {
			continue
		}
		switch kind {
		case "cannot_link":
			vetoed[pos.Int64] = true
			delete(targets, pos.Int64)
		case "must_link":
			if !vetoed[pos.Int64] {
				targets[pos.Int64] = &target{pos.Int64, 1.0, "must_link", map[string]float64{"must_link": 1}}
			}
		}
	}
	_ = hard
	// 2-4. soft stage, only when employer is resolved; same-provider pairs are never soft-compared (invariant 5)
	queued := map[int64]float64{}
	queuedF := map[int64]map[string]float64{}
	if l.EmployerID != 0 {
		cs, err := r.candidates(ctx, tx, l, `p.employer_id=? AND p.provider_id<>?`, l.EmployerID, l.ProviderID)
		if err != nil {
			return Outcome{}, err
		}
		for _, c := range cs {
			if vetoed[c.pos] {
				continue
			}
			if !l.PostedAt.IsZero() && !c.PostedAt.IsZero() && math.Abs(l.PostedAt.Sub(c.PostedAt).Hours()/24) > r.P.DateWindowDays {
				continue
			}
			if k, _ := r.constraint(ctx, tx, id, c.ID); k == "cannot_link" {
				vetoed[c.pos] = true
				delete(targets, c.pos)
				continue
			}
			s, f, guard := r.Score(l, c.Listing)
			if guard != "" {
				continue // hard negative: distinct, not even queued
			}
			agency := l.employerKind == "agency"
			switch {
			case s >= r.P.TAuto && !agency:
				note(c, s, "soft_score", f)
			case s >= r.P.TReview:
				if s > queued[c.ID] {
					queued[c.ID] = s
					queuedF[c.ID] = f
				}
			}
		}
	}
	// decide
	var order []*target
	for _, t := range targets {
		order = append(order, t)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].score != order[j].score {
			return order[i].score > order[j].score
		}
		return order[i].pos < order[j].pos
	})
	if len(order) > 0 {
		keep := order[0]
		// transitive bridge: every other target is merged into the lowest-id Position when safe, else queued
		var group []int64
		for _, t := range order {
			group = append(group, t.pos)
		}
		sort.Slice(group, func(i, j int) bool { return group[i] < group[j] })
		root := group[0]
		if err := r.attach(ctx, tx, l, keep.pos, keep.score, keep.reason, keep.f, "position_auto_attach", "auto"); err != nil {
			return Outcome{}, err
		}
		for _, p := range group {
			if p == keep.pos {
				continue
			}
			if err := r.bridge(ctx, tx, keep.pos, p); err != nil {
				return Outcome{}, err
			}
		}
		_ = root
		final, err := r.rootOf(ctx, tx, keep.pos)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{Position: final, Decision: "auto", Score: keep.score, Reason: keep.reason, Features: keep.f}, nil
	}
	// new Position, plus review-queue rows for the uncertain band
	pid, err := r.newPosition(ctx, tx, l)
	if err != nil {
		return Outcome{}, err
	}
	feat := map[string]float64{}
	decision := "new"
	best := 0.0
	var qids []int64
	for cid := range queued {
		qids = append(qids, cid)
	}
	sort.Slice(qids, func(i, j int) bool { return qids[i] < qids[j] })
	for _, cid := range qids {
		a, b := pair(id, cid)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO merge_candidate(a_listing,b_listing,score,features_json) VALUES(?,?,?,?)`,
			a, b, queued[cid], featJSON(queuedF[cid], nil)); err != nil {
			return Outcome{}, err
		}
		if queued[cid] > best {
			best, feat, decision = queued[cid], queuedF[cid], "queued"
		}
		if err := audit.Append(ctx, tx, audit.Entry{PostingID: id, Actor: r.actor(), Reason: "position_review_queued",
			Evidence: map[string]any{"listing": id, "other": cid, "score": queued[cid], "resolver_version": r.P.Version}}); err != nil {
			return Outcome{}, err
		}
	}
	if err := r.link(ctx, tx, id, pid, "new", best, feat, nil); err != nil {
		return Outcome{}, err
	}
	if err := audit.Append(ctx, tx, audit.Entry{PostingID: id, Actor: r.actor(), Reason: "position_new",
		Evidence: map[string]any{"listing": id, "position": pid, "resolver_version": r.P.Version}}); err != nil {
		return Outcome{}, err
	}
	return Outcome{Position: pid, Decision: decision, Score: best, Features: feat}, nil
}

// identify fills and persists the identity columns of a Listing: ATS identity, canonical apply URL and employer.
func (r *Resolver) identify(ctx context.Context, tx *sql.Tx, l *Listing) error {
	var err error
	l.ATS, _ = ExtractATS(l.ApplyURL)
	if c, err := CanonURL(l.ApplyURL); err == nil {
		l.ApplyCanon = c.URL
	}
	if l.EmployerID, err = r.resolveEmployer(ctx, tx, l); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT kind FROM employer WHERE id=?),'')`, l.EmployerID).Scan(&l.employerKind); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE posting SET ats_kind=?, ats_tenant=?, ats_job_id=?, apply_url_canon=?, employer_id=? WHERE id=?`,
		nz(l.ATS.Kind), nz(l.ATS.Tenant), nz(l.ATS.JobID), nz(l.ApplyCanon), nzi(l.EmployerID), l.ID)
	return err
}

func nz(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nzi(i int64) any {
	if i == 0 {
		return nil
	}
	return i
}

func (r *Resolver) allConstrained(ctx context.Context, tx *sql.Tx, id int64) (map[int64]string, error) {
	rs, err := tx.QueryContext(ctx, `SELECT a_listing, b_listing, kind FROM position_constraint WHERE a_listing=? OR b_listing=?`, id, id)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	m := map[int64]string{}
	for rs.Next() {
		var a, b int64
		var k string
		if err := rs.Scan(&a, &b, &k); err != nil {
			return nil, err
		}
		o := a
		if a == id {
			o = b
		}
		m[o] = k
	}
	return m, rs.Err()
}

func (r *Resolver) rootOf(ctx context.Context, tx *sql.Tx, pos int64) (int64, error) {
	for i := 0; i < 64; i++ {
		var next sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT merged_into FROM position WHERE id=?`, pos).Scan(&next); err != nil {
			return 0, err
		}
		if !next.Valid {
			return pos, nil
		}
		pos = next.Int64
	}
	return 0, errors.New("position: merged_into chain too long")
}

func (r *Resolver) newPosition(ctx context.Context, tx *sql.Tx, l Listing) (int64, error) {
	scope, _ := json.Marshal(l.scope)
	res, err := tx.ExecContext(ctx, `INSERT INTO position(employer_id,state,title_norm,seniority,remote_scope_json,posted_first,posted_last,first_seen,last_seen,representative_listing_id,resolver_version)
		VALUES(?,'open',?,?,?,?,?,?,?,?,?)`, nzi(l.EmployerID), l.title.Norm, nz(l.title.Seniority), string(scope), tsOrNil(l.PostedAt), tsOrNil(l.PostedAt), l.FirstSeen, l.LastSeen, l.ID, r.P.Version)
	if err != nil {
		return 0, err
	}
	pid, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE posting SET position_id=? WHERE id=?`, pid, l.ID)
	return pid, err
}

func tsOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// link retires the listing's current link and writes the new current one.
func (r *Resolver) link(ctx context.Context, tx *sql.Tx, listing, pos int64, decision string, score float64, f map[string]float64, extra map[string]any) error {
	if _, err := tx.ExecContext(ctx, `UPDATE position_link SET is_current=0 WHERE listing_id=? AND is_current=1`, listing); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO position_link(listing_id,position_id,decision,score,features_json,resolver_version,is_current,ts) VALUES(?,?,?,?,?,?,1,?)`,
		listing, pos, decision, nullScore(score, decision), featJSON(f, extra), r.P.Version, r.now()); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE posting SET position_id=? WHERE id=?`, pos, listing)
	return err
}

func nullScore(s float64, decision string) any {
	if decision == "new" && s == 0 {
		return nil
	}
	return s
}

func (r *Resolver) attach(ctx context.Context, tx *sql.Tx, l Listing, pos int64, score float64, reason string, f map[string]float64, auditReason, decision string) error {
	if err := r.link(ctx, tx, l.ID, pos, decision, score, f, map[string]any{"reason": reason}); err != nil {
		return err
	}
	if err := RefreshPosition(ctx, tx, pos, r.P.Version); err != nil {
		return err
	}
	return audit.Append(ctx, tx, audit.Entry{PostingID: l.ID, Actor: r.actor(), Reason: auditReason,
		Evidence: map[string]any{"listing": l.ID, "position": pos, "score": score, "match": reason, "resolver_version": r.P.Version}})
}

// bridge merges Position b into a Position a when no cannot_link crosses and no application is live_approved; otherwise it queues the
// representatives for human review.
func (r *Resolver) bridge(ctx context.Context, tx *sql.Tx, a, b int64) error {
	ra, err := r.rootOf(ctx, tx, a)
	if err != nil {
		return err
	}
	rb, err := r.rootOf(ctx, tx, b)
	if err != nil {
		return err
	}
	if ra == rb {
		return nil
	}
	keep, absorb := ra, rb
	if rb < ra {
		keep, absorb = rb, ra
	}
	err = Merge(ctx, tx, MergeSpec{Keep: keep, Absorb: absorb, Actor: r.actor(), Reason: "transitive_bridge", Auto: true, Version: r.P.Version, Now: r.now()})
	if errors.Is(err, ErrCannotLink) || errors.Is(err, ErrPositionBusy) || errors.Is(err, ErrBothLocked) {
		return r.queueRepresentatives(ctx, tx, keep, absorb)
	}
	return err
}

func (r *Resolver) queueRepresentatives(ctx context.Context, tx *sql.Tx, a, b int64) error {
	var x, y int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(representative_listing_id,0) FROM position WHERE id=?`, a).Scan(&x); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(representative_listing_id,0) FROM position WHERE id=?`, b).Scan(&y); err != nil {
		return err
	}
	if x == 0 || y == 0 || x == y {
		return nil
	}
	p, q := pair(x, y)
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO merge_candidate(a_listing,b_listing,score,features_json) VALUES(?,?,?,?)`, p, q, r.P.TAuto, `{"bridge":1}`)
	return err
}
