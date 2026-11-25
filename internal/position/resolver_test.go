package position

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/store"
)

// JSK-T191 (FR-080, FR-082, SC-027). Listings below are labelled synthetic: they exercise tool logic and never count as evidence of recall.

type fx struct {
	t  *testing.T
	db *store.DB
	r  *Resolver
}

func newFx(t *testing.T) *fx {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	f := &fx{t: t, db: db, r: &Resolver{P: DefaultParams()}}
	for _, n := range []string{"provA", "provB", "provC"} {
		f.exec(`INSERT INTO provider(name,base_url,kind) VALUES(?,?,?)`, n, "https://"+n+".example", "snapshot")
	}
	return f
}

func (f *fx) exec(q string, args ...any) {
	f.t.Helper()
	if err := f.db.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec(q, args...); return err }); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

type lst struct {
	prov                int // 1..3
	job, title, company string
	loc, mode, apply    string
	posted              string
	smin, smax          int
}

func (f *fx) add(l lst) int64 {
	f.t.Helper()
	var id int64
	err := f.db.Write(context.Background(), func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,company,location_raw,remote_mode,apply_url,source_url,posted_at,salary_min,salary_max,first_seen,last_seen)
			VALUES(?,?,?,'discovered','t',?,?,?,?,?,?,?,?,?, '2026-11-01T00:00:00Z','2026-11-01T00:00:00Z')`,
			l.prov, l.job, fmt.Sprintf("k-%d-%s", l.prov, l.job), l.title, nilS(l.company), nilS(l.loc), nilS(l.mode), nilS(l.apply), "https://src.example/"+l.job, nilS(l.posted), nilI(l.smin), nilI(l.smax))
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func nilS(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nilI(i int) any {
	if i == 0 {
		return nil
	}
	return i
}

func (f *fx) resolve(id int64) Outcome {
	f.t.Helper()
	var o Outcome
	err := f.db.Write(context.Background(), func(tx *sql.Tx) error {
		var err error
		o, err = f.r.Resolve(context.Background(), tx, id)
		return err
	})
	if err != nil {
		f.t.Fatalf("resolve %d: %v", id, err)
	}
	return o
}

func (f *fx) pos(id int64) int64 {
	var p sql.NullInt64
	if err := f.db.Read().QueryRow(`SELECT position_id FROM posting WHERE id=?`, id).Scan(&p); err != nil {
		f.t.Fatal(err)
	}
	return p.Int64
}

func (f *fx) count(q string, args ...any) int {
	var n int
	if err := f.db.Read().QueryRow(q, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *fx) invariants() {
	f.t.Helper()
	v, err := CheckInvariants(context.Background(), f.db.Read())
	if err != nil {
		f.t.Fatal(err)
	}
	if len(v) > 0 {
		f.t.Fatalf("invariants violated: %v", v)
	}
}

const gh = "https://job-boards.greenhouse.io/acme/jobs/%s"

func TestATSIdentityAcrossProvidersAutoAttaches(t *testing.T) {
	f := newFx(t)
	a := f.add(lst{prov: 1, job: "a1", title: "Senior Android Engineer", company: "Acme Inc", loc: "Remote", mode: "remote", apply: fmt.Sprintf(gh, "111")})
	b := f.add(lst{prov: 2, job: "b1", title: "Android Engineer (m/w/d)", company: "ACME", loc: "Worldwide", mode: "remote", apply: fmt.Sprintf(gh, "111") + "?gh_src=rem"})
	oa, ob := f.resolve(a), f.resolve(b)
	if oa.Decision != "new" || ob.Decision != "auto" || ob.Reason != "ats_identity" || ob.Score != 1 || oa.Position != ob.Position {
		t.Fatalf("a=%+v b=%+v", oa, ob)
	}
	if f.pos(a) != f.pos(b) || f.count(`SELECT count(*) FROM position`) != 1 {
		t.Fatal("not one Position")
	}
	// dup_of mirror: representative NULL, member points at it
	if f.count(`SELECT count(*) FROM posting WHERE id=? AND dup_of IS NULL`, a) != 1 || f.count(`SELECT count(*) FROM posting WHERE id=? AND dup_of=?`, b, a) != 1 {
		t.Fatal("dup_of mirror wrong")
	}
	if f.count(`SELECT count(*) FROM audit_log WHERE reason_code='position_auto_attach' AND posting_id=?`, b) != 1 || f.count(`SELECT count(*) FROM audit_log WHERE reason_code='position_new'`) != 1 {
		t.Fatal("audit rows missing")
	}
	f.invariants()
}

func TestIdempotentAndEmployerAliasFromTenant(t *testing.T) {
	f := newFx(t)
	a := f.add(lst{prov: 1, job: "a1", title: "Backend Engineer", company: "Acme", apply: fmt.Sprintf(gh, "5")})
	o1 := f.resolve(a)
	o2 := f.resolve(a)
	if o2.Decision != "existing" || o2.Position != o1.Position || f.count(`SELECT count(*) FROM position_link`) != 1 || f.count(`SELECT count(*) FROM position`) != 1 {
		t.Fatalf("not idempotent: %+v %+v", o1, o2)
	}
	if f.count(`SELECT count(*) FROM employer_alias WHERE kind='ats_tenant' AND value='greenhouse:acme'`) != 1 {
		t.Fatal("tenant alias not recorded")
	}
	// a different spelling of the company on the same tenant resolves to the same employer through the alias
	b := f.add(lst{prov: 2, job: "b1", title: "Backend Engineer", company: "Acme Corporation Holdings Ltd GmbH Zed", apply: fmt.Sprintf(gh, "5")})
	f.resolve(b)
	if f.count(`SELECT count(DISTINCT employer_id) FROM posting`) != 1 {
		t.Fatal("tenant alias did not win over the name")
	}
}

func TestApplyURLHardKeyOnEmployerHostOnly(t *testing.T) {
	f := newFx(t)
	a := f.add(lst{prov: 1, job: "a", title: "Data Engineer", company: "Foo", apply: "https://careers.foo.example/jobs/42?utm_source=x"})
	b := f.add(lst{prov: 2, job: "b", title: "Totally different words", company: "Foo", apply: "https://careers.foo.example/jobs/42/apply"})
	f.resolve(a)
	if o := f.resolve(b); o.Decision != "auto" || o.Reason != "apply_url" || o.Score != 0.99 {
		t.Fatalf("%+v", o)
	}
	// a board host is never an employer identity
	c := f.add(lst{prov: 1, job: "c", title: "Ops Lead", company: "Bar", apply: "https://remotive.com/r/1"})
	d := f.add(lst{prov: 2, job: "d", title: "Cook", company: "Baz", apply: "https://remotive.com/r/1"})
	f.resolve(c)
	if o := f.resolve(d); o.Decision == "auto" {
		t.Fatalf("board host treated as identity: %+v", o)
	}
	f.invariants()
}

func TestEdgeCasesStayDistinct(t *testing.T) {
	for name, c := range map[string][2]lst{
		"two openings same title, different countries onsite": {
			{prov: 1, job: "1", title: "Go Developer", company: "Acme", loc: "Berlin, Germany", mode: "onsite"},
			{prov: 2, job: "2", title: "Go Developer", company: "Acme", loc: "Paris, France", mode: "onsite"}},
		"same tenant two ats ids": {
			{prov: 1, job: "3", title: "Go Developer", company: "Acme", apply: fmt.Sprintf(gh, "100")},
			{prov: 2, job: "4", title: "Go Developer", company: "Acme", apply: fmt.Sprintf(gh, "200")}},
		"senior vs junior": {
			{prov: 1, job: "5", title: "Senior Android Engineer", company: "Acme", loc: "Remote", mode: "remote"},
			{prov: 2, job: "6", title: "Junior Android Engineer", company: "Acme", loc: "Remote", mode: "remote"}},
		"different technology": {
			{prov: 1, job: "7", title: "Flutter Developer", company: "Acme", loc: "Remote", mode: "remote"},
			{prov: 2, job: "8", title: "Android Developer", company: "Acme", loc: "Remote", mode: "remote"}},
		"different employers": {
			{prov: 1, job: "9", title: "Go Developer", company: "Acme", loc: "Remote", mode: "remote"},
			{prov: 2, job: "10", title: "Go Developer", company: "Globex", loc: "Remote", mode: "remote"}},
		"missing company": {
			{prov: 1, job: "11", title: "Go Developer", loc: "Remote", mode: "remote"},
			{prov: 2, job: "12", title: "Go Developer", loc: "Remote", mode: "remote"}},
	} {
		f := newFx(t) // one fixture per case: listings of other cases must not become candidates
		x, y := f.add(c[0]), f.add(c[1])
		f.resolve(x)
		o := f.resolve(y)
		if f.pos(x) == f.pos(y) || o.Decision == "auto" {
			t.Errorf("%s: merged (%+v)", name, o)
		}
		if f.count(`SELECT count(*) FROM merge_candidate WHERE a_listing=? OR b_listing=?`, x, x) != 0 && strings.Contains(name, "senior") {
			t.Errorf("%s: hard negative must not even be queued", name)
		}
		f.invariants()
	}
}

func TestSameProviderRelistingSharesATSIdentity(t *testing.T) {
	f := newFx(t)
	a := f.add(lst{prov: 1, job: "old", title: "Go Developer", company: "Acme", apply: fmt.Sprintf(gh, "77")})
	b := f.add(lst{prov: 1, job: "new", title: "Go Developer", company: "Acme", apply: fmt.Sprintf(gh, "77")})
	f.resolve(a)
	f.resolve(b)
	if f.pos(a) != f.pos(b) {
		t.Fatal("relisted job (same provider, new job id, same ATS id) must share a Position")
	}
	// same provider, no shared ATS identity: never auto-merged even with identical titles (invariant 5)
	c := f.add(lst{prov: 1, job: "c1", title: "QA Engineer", company: "Zed", loc: "Remote", mode: "remote"})
	d := f.add(lst{prov: 1, job: "c2", title: "QA Engineer", company: "Zed", loc: "Remote", mode: "remote"})
	f.resolve(c)
	f.resolve(d)
	if f.pos(c) == f.pos(d) {
		t.Fatal("two openings of one provider were merged without ATS identity")
	}
	f.invariants()
}

func TestThresholdBandsAndEdge(t *testing.T) {
	base := "abcdefghijklmnopqrstuvwxy" // 25 letters; k substitutions give ratio 1-k/25
	sub := func(k int) string {
		b := []byte(base)
		for i := 0; i < k; i++ {
			b[i] = 'Z'
		}
		return strings.ToLower(string(b))
	}
	for _, c := range []struct {
		k    int
		want string
	}{{0, "auto"}, {1, "auto"}, {2, "auto"} /* exactly 0.92 */, {3, "queued"} /* 0.88 */, {6, "queued"} /* 0.76 */, {7, "new"} /* 0.72 */} {
		f := newFx(t)
		x := f.add(lst{prov: 1, job: "x", title: base, company: "Acme", loc: "Berlin, Germany", mode: "onsite"})
		y := f.add(lst{prov: 2, job: "y", title: sub(c.k), company: "Acme", loc: "Berlin, Germany", mode: "onsite"})
		f.resolve(x)
		o := f.resolve(y)
		if o.Decision != c.want {
			t.Errorf("k=%d decision %s (score %v), want %s", c.k, o.Decision, o.Score, c.want)
		}
		if c.want == "queued" {
			a, b := pair(x, y)
			if f.count(`SELECT count(*) FROM merge_candidate WHERE a_listing=? AND b_listing=? AND status='open'`, a, b) != 1 || f.pos(x) == f.pos(y) {
				t.Errorf("k=%d queued without a candidate row or merged anyway", c.k)
			}
		}
		f.invariants()
	}
}

func TestHumanConstraintsOutrankEvidence(t *testing.T) {
	f := newFx(t)
	a := f.add(lst{prov: 1, job: "a", title: "Go Developer", company: "Acme", apply: fmt.Sprintf(gh, "9")})
	b := f.add(lst{prov: 2, job: "b", title: "Go Developer", company: "Acme", apply: fmt.Sprintf(gh, "9")})
	f.resolve(a)
	_ = f.db.Write(context.Background(), func(tx *sql.Tx) error {
		return AddConstraint(context.Background(), tx, "operator", a, b, "cannot_link")
	})
	f.resolve(b)
	if f.pos(a) == f.pos(b) {
		t.Fatal("cannot_link lost against identical ATS identity")
	}
	// must_link wins with nothing in common
	c := f.add(lst{prov: 1, job: "c", title: "Chef", company: "Foo"})
	d := f.add(lst{prov: 3, job: "d", title: "Plumber", company: "Bar"})
	_ = f.db.Write(context.Background(), func(tx *sql.Tx) error { return AddConstraint(context.Background(), tx, "operator", c, d, "must_link") })
	f.resolve(c)
	if o := f.resolve(d); o.Reason != "must_link" || f.pos(c) != f.pos(d) {
		t.Fatalf("%+v", o)
	}
	f.invariants()
}

func TestTransitiveBridgeMergesTwoPositions(t *testing.T) {
	f := newFx(t)
	base := "abcdefghijklmnopqrstuvwxy"
	mk := func(k int, s byte) string {
		b := []byte(base)
		for i := 0; i < k; i++ {
			b[i] = s
		}
		return string(b)
	}
	a := f.add(lst{prov: 1, job: "a", title: base, company: "Acme", loc: "Berlin, Germany", mode: "onsite"})
	b := f.add(lst{prov: 2, job: "b", title: mk(3, 'Z'), company: "Acme", loc: "Berlin, Germany", mode: "onsite"}) // 0.88 from a: queued, own Position
	cTitle := []byte(mk(3, 'Z'))
	cTitle[0] = 'a' // c: 1 substitution from a... and 2 from b
	c := f.add(lst{prov: 3, job: "c", title: string(cTitle), company: "Acme", loc: "Berlin, Germany", mode: "onsite"})
	f.resolve(a)
	if f.resolve(b).Decision != "queued" || f.pos(a) == f.pos(b) {
		t.Fatal("setup: b must be queued apart from a")
	}
	o := f.resolve(c)
	if o.Decision != "auto" || f.pos(a) != f.pos(b) || f.pos(a) != f.pos(c) {
		t.Fatalf("bridge did not merge: %+v pos %d %d %d", o, f.pos(a), f.pos(b), f.pos(c))
	}
	if f.count(`SELECT count(*) FROM position WHERE state='merged'`) != 1 {
		t.Fatal("expected one merged Position")
	}
	f.invariants()
}

func TestBridgeRefusedAcrossCannotLink(t *testing.T) {
	f := newFx(t)
	base := "abcdefghijklmnopqrstuvwxy"
	mk := func(k int, s byte) string {
		b := []byte(base)
		for i := 0; i < k; i++ {
			b[i] = s
		}
		return string(b)
	}
	a := f.add(lst{prov: 1, job: "a", title: base, company: "Acme", loc: "Berlin, Germany", mode: "onsite"})
	b := f.add(lst{prov: 2, job: "b", title: mk(3, 'Z'), company: "Acme", loc: "Berlin, Germany", mode: "onsite"})
	cTitle := []byte(mk(3, 'Z'))
	cTitle[0] = 'a'
	c := f.add(lst{prov: 3, job: "c", title: string(cTitle), company: "Acme", loc: "Berlin, Germany", mode: "onsite"})
	f.resolve(a)
	f.resolve(b)
	_ = f.db.Write(context.Background(), func(tx *sql.Tx) error {
		return AddConstraint(context.Background(), tx, "operator", a, b, "cannot_link")
	})
	f.resolve(c)
	if f.pos(a) == f.pos(b) {
		t.Fatal("bridge crossed a cannot_link")
	}
	f.invariants()
}

func TestMergeBusyGuardAndHumanMergeSplit(t *testing.T) {
	f := newFx(t)
	a := f.add(lst{prov: 1, job: "a", title: "Chef", company: "Foo"})
	b := f.add(lst{prov: 2, job: "b", title: "Plumber", company: "Bar"})
	f.resolve(a)
	f.resolve(b)
	pa, pb := f.pos(a), f.pos(b)
	f.exec(`INSERT INTO application(posting_id,idem_key,state,created_at,updated_at) VALUES(?, 'ap1','live_approved','t','t')`, b)
	err := f.db.Write(context.Background(), func(tx *sql.Tx) error { return MergeHuman(context.Background(), tx, "operator", pa, pb, "same_job") })
	if err != ErrPositionBusy {
		t.Fatalf("want ErrPositionBusy, got %v", err)
	}
	if f.pos(a) == f.pos(b) || f.count(`SELECT count(*) FROM audit_log WHERE reason_code='position_merge_human'`) != 0 {
		t.Fatal("refused merge left traces")
	}
	f.exec(`UPDATE application SET state='draft_prepared' WHERE idem_key='ap1'`)
	if err := f.db.Write(context.Background(), func(tx *sql.Tx) error { return MergeHuman(context.Background(), tx, "operator", pa, pb, "same_job") }); err != nil {
		t.Fatal(err)
	}
	if f.pos(a) != f.pos(b) || f.count(`SELECT position_id FROM application WHERE idem_key='ap1'`) != int(pa) ||
		f.count(`SELECT count(*) FROM position_constraint WHERE kind='must_link'`) != 1 || f.count(`SELECT count(*) FROM audit_log WHERE reason_code='position_merge_human'`) != 1 {
		t.Fatal("merge effects wrong")
	}
	f.invariants()
	// bad reason text is refused (audit evidence is codes only)
	err = f.db.Write(context.Background(), func(tx *sql.Tx) error {
		_, e := Split(context.Background(), tx, "operator", b, "Because I said so!", "")
		return e
	})
	if err != ErrBadReason {
		t.Fatalf("want ErrBadReason, got %v", err)
	}
	var np int64
	err = f.db.Write(context.Background(), func(tx *sql.Tx) (e error) {
		np, e = Split(context.Background(), tx, "operator", b, "wrong_merge", "")
		return
	})
	if err != nil || np == pa || f.pos(b) != np || f.pos(a) != pa {
		t.Fatalf("split: %v np=%d", err, np)
	}
	if f.count(`SELECT position_id FROM application WHERE idem_key='ap1'`) != int(np) || f.count(`SELECT count(*) FROM position_constraint WHERE kind='cannot_link'`) != 1 {
		t.Fatal("application or constraint did not follow the split")
	}
	if err := f.db.Write(context.Background(), func(tx *sql.Tx) error { _, e := Split(context.Background(), tx, "operator", b, "again", ""); return e }); err != ErrNothingToSplit {
		t.Fatalf("want ErrNothingToSplit, got %v", err)
	}
	f.invariants()
}

// Property: arrival order does not change the partition (SC-027, modulo documented tie-breaks; this scenario has none).
func TestArrivalOrderIndependent(t *testing.T) {
	set := []lst{
		{prov: 1, job: "a1", title: "Senior Go Developer", company: "Acme", apply: fmt.Sprintf(gh, "1"), loc: "Remote", mode: "remote"},
		{prov: 2, job: "a2", title: "Go Developer (m/f/d)", company: "Acme Inc", apply: fmt.Sprintf(gh, "1"), loc: "Remote", mode: "remote"},
		{prov: 3, job: "a3", title: "Senior Go Developer", company: "ACME", apply: fmt.Sprintf(gh, "1") + "?gh_src=x", loc: "Remote", mode: "remote"},
		{prov: 1, job: "b1", title: "Flutter Developer", company: "Globex", loc: "Remote", mode: "remote"},
		{prov: 2, job: "b2", title: "Flutter Developer", company: "Globex Ltd", loc: "Remote", mode: "remote"},
		{prov: 3, job: "c1", title: "Chef", company: "Initech"},
	}
	var want string
	r := rand.New(rand.NewSource(7))
	// SC-027 states 1,000 cases. A 6-element set has 720 permutations: every one is exercised once (sorted by a
	// seeded shuffle), then random permutations (repeats allowed) bring the total to arrivalCases.
	all := allPerms(len(set))
	r.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	seen := map[string]bool{}
	for n := 0; n < arrivalCases; n++ {
		f := newFx(t)
		var perm []int
		if n < len(all) {
			perm = all[n]
		} else {
			perm = r.Perm(len(set))
		}
		seen[fmt.Sprint(perm)] = true
		ids := map[string]int64{}
		for _, i := range perm {
			ids[set[i].job] = f.add(set[i])
		}
		for _, i := range perm {
			f.resolve(ids[set[i].job])
		}
		f.invariants()
		groups := map[int64][]string{}
		for job, id := range ids {
			groups[f.pos(id)] = append(groups[f.pos(id)], job)
		}
		var parts []string
		for _, g := range groups {
			sort.Strings(g)
			parts = append(parts, strings.Join(g, "+"))
		}
		sort.Strings(parts)
		got := strings.Join(parts, " | ")
		if n == 0 {
			want = got
			if want != "a1+a2+a3 | b1+b2 | c1" {
				t.Fatalf("unexpected partition %q", want)
			}
		} else if got != want {
			t.Fatalf("order %v gave %q, want %q", perm, got, want)
		}
	}
	if len(seen) != len(all) {
		t.Fatalf("exercised %d distinct arrival orders, want all %d", len(seen), len(all))
	}
}

// arrivalCases is the SC-027 case count (spec.md: "property test, 1,000 cases"); a lower value fails below.
const arrivalCases = 1000

func TestArrivalCasesMeetSC027(t *testing.T) {
	if arrivalCases < 1000 {
		t.Fatalf("SC-027 requires 1,000 cases, have %d", arrivalCases)
	}
}

func allPerms(n int) [][]int {
	var out [][]int
	var rec func(cur []int, used []bool)
	rec = func(cur []int, used []bool) {
		if len(cur) == n {
			out = append(out, append([]int(nil), cur...))
			return
		}
		for i := 0; i < n; i++ {
			if !used[i] {
				used[i] = true
				rec(append(cur, i), used)
				used[i] = false
			}
		}
	}
	rec(nil, make([]bool, n))
	return out
}

func TestParamsLoadAndValidate(t *testing.T) {
	p := DefaultParams()
	if p.Version != "r1" || p.TAuto != 0.92 || p.TReview != 0.75 {
		t.Fatalf("%+v", p)
	}
	if _, err := LoadParams("version\tx\nt_auto\t0.5\nt_review\t0.9\ndate_window_days\t1\nw_title\t1\nw_date\t1\nw_salary\t1\nw_apply_host\t1\nw_body_mh\t1\n"); err == nil {
		t.Error("t_review >= t_auto accepted")
	}
}

// Control arm: the invariant checker must actually see each corruption (a checker that always returns empty would make every other test vacuous).
func TestInvariantCheckerDetectsCorruption(t *testing.T) {
	for name, corrupt := range map[string]string{
		"dup_of wrong":              `UPDATE posting SET dup_of=2 WHERE id=1`,
		"representative has dup_of": `UPDATE posting SET dup_of=2 WHERE id=1`,
		"link position differs":     `UPDATE posting SET position_id=2 WHERE id=1`,
		"merged still has members":  `UPDATE position SET state='merged', merged_into=2 WHERE id=1`,
		"cannot_link inside one":    `UPDATE posting SET position_id=1 WHERE id=2; INSERT INTO position_constraint(a_listing,b_listing,kind,actor,ts) VALUES(1,2,'cannot_link','x','t')`,
	} {
		f := newFx(t)
		a := f.add(lst{prov: 1, job: "a", title: "Chef", company: "Foo"})
		b := f.add(lst{prov: 2, job: "b", title: "Plumber", company: "Bar"})
		f.resolve(a)
		f.resolve(b)
		f.invariants() // clean before the corruption
		f.exec(corrupt)
		v, err := CheckInvariants(context.Background(), f.db.Read())
		if err != nil || len(v) == 0 {
			t.Errorf("%s: corruption not detected (%v, %v)", name, v, err)
		}
	}
}

func TestInvariantCheckerSeesMemberWithoutMirror(t *testing.T) {
	f := newFx(t)
	a := f.add(lst{prov: 1, job: "a", title: "Go Developer", company: "Acme", apply: fmt.Sprintf(gh, "3")})
	b := f.add(lst{prov: 2, job: "b", title: "Go Developer", company: "Acme", apply: fmt.Sprintf(gh, "3")})
	f.resolve(a)
	f.resolve(b)
	f.invariants()
	f.exec(`UPDATE posting SET dup_of=NULL WHERE id=?`, b)
	if v, err := CheckInvariants(context.Background(), f.db.Read()); err != nil || len(v) == 0 {
		t.Fatalf("member without dup_of mirror not detected: %v %v", v, err)
	}
}

// PR 72 review follow-up: a generic careers page shared by two unrelated openings is not an identity (precision over recall).
func TestGenericCareersPageIsNotAnApplyIdentity(t *testing.T) {
	f := newFx(t)
	a := f.add(lst{prov: 1, job: "a", title: "Backend Engineer", company: "Foo", apply: "https://foo.example/careers"})
	b := f.add(lst{prov: 2, job: "b", title: "Product Designer", company: "Foo", apply: "https://foo.example/careers"})
	f.resolve(a)
	if o := f.resolve(b); o.Decision == "auto" || f.pos(a) == f.pos(b) {
		t.Fatalf("generic careers URL merged distinct jobs: %+v", o)
	}
	// a job-specific path or query identity still counts
	c := f.add(lst{prov: 1, job: "c", title: "Chef", company: "Bar", apply: "https://bar.example/jobs/42"})
	d := f.add(lst{prov: 2, job: "d", title: "Cook", company: "Bar", apply: "https://bar.example/jobs/42"})
	e := f.add(lst{prov: 1, job: "e", title: "Dev", company: "Baz", apply: "https://baz.example/careers?gh_jid=77"})
	g := f.add(lst{prov: 2, job: "g", title: "Developer", company: "Baz", apply: "https://baz.example/careers?gh_jid=77"})
	f.resolve(c)
	f.resolve(d)
	f.resolve(e)
	f.resolve(g)
	if f.pos(c) != f.pos(d) || f.pos(e) != f.pos(g) {
		t.Fatal("job-specific apply URLs must still be hard keys")
	}
}
