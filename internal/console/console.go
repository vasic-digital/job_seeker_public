// Package console is the server-rendered operator console (FR-037..FR-041): html/template pages, hash-pinned htmx,
// every string through internal/i18n. Handlers are thin: all approval, handoff and cap logic lives in internal/draft,
// and no handler can write application state `sent` (the draft package owns that path).
package console

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/api"
	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/draft"
	"github.com/vasic-digital/job_seeker/internal/fsm"
	"github.com/vasic-digital/job_seeker/internal/i18n"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/matchstore"
	"github.com/vasic-digital/job_seeker/internal/privacy"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/templates"
	"github.com/vasic-digital/job_seeker/internal/tracker"
	"github.com/vasic-digital/job_seeker/web"
)

// HtmxSHA256 pins the vendored htmx build (web/static/vendor/htmx.min.js, htmx 2.0.4). A different file refuses to start.
const HtmxSHA256 = "e209dda5c8235479f3166defc7750e1dbcd5a5c1808b7792fc2e6733768fb447"

// Health is one provider-health row for the settings page.
type Health struct{ Name, Circuit, NextAllowed, Legal, LegalChangedAt, LegalDiff string }

// Deps are the console's collaborators.
type Deps struct {
	DB     *store.DB
	Keys   *crypto.Keyring
	Draft  *draft.Service
	Cat    *i18n.Catalogue
	Health func(context.Context) ([]Health, error) // nil: the settings page says no providers
	Now    func() time.Time
	// Tracker serves the tracker page (JSK-T086); nil disables the page and the due-follow-up block.
	Tracker *tracker.Service
	// Retention is the configuration in force (zero fields select the C9 defaults); shown on the settings page.
	Retention ingest.Retention
}

// Console holds parsed templates and dependencies.
type Console struct {
	Deps
	pages   map[string]*template.Template
	htmxSRI string
	static  http.Handler
}

var pageNames = []string{"login", "inbox", "queue", "job", "draft", "applications", "tracking", "settings", "audit"}

// New parses every page against the layout and verifies the vendored htmx hash.
func New(d Deps) (*Console, error) {
	c := &Console{Deps: d, pages: map[string]*template.Template{}}
	if c.Now == nil {
		c.Now = time.Now
	}
	raw, err := fs.ReadFile(web.FS, "static/vendor/htmx.min.js")
	if err != nil {
		return nil, errors.New("console: vendored htmx missing")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != HtmxSHA256 {
		return nil, errors.New("console: vendored htmx differs from its pinned SHA-256")
	}
	c.htmxSRI = "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
	funcs := template.FuncMap{"fdate": func(lang string, t time.Time) string { return i18n.Date(lang, t) },
		"t": func(lang, key string, args ...any) string { return d.Cat.Msg(lang, key, args...) }}
	for _, n := range pageNames {
		t, err := template.New(n).Funcs(funcs).ParseFS(web.FS, "templates/layout.html", "templates/"+n+".html")
		if err != nil {
			return nil, fmt.Errorf("console: template %s: %w", n, err)
		}
		c.pages[n] = t
	}
	sub, err := fs.Sub(web.FS, "static")
	if err != nil {
		return nil, err
	}
	c.static = http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
	return c, nil
}

type navItem struct {
	Href, Key string
	Current   bool
}

var navAll = []navItem{{"/", "nav.inbox", false}, {"/queue", "nav.queue", false}, {"/applications", "nav.applications", false}, {"/settings", "nav.settings", false}, {"/audit", "nav.audit", false}}

type page struct {
	Lang, Dir, TitleKey, CSRF, HtmxSRI, Error, Notice string
	LangOpts                                          []langOpt
	LangWanted                                        string // a preferred language that is not shipped yet (shown, never silently replaced)
	Authed, PasskeyJS                                 bool
	Nav                                               []navItem
	// page-specific
	Rows                any
	Filter              filter
	States              []string
	Bands               []string
	Providers           []string
	Areas               []string
	Langs               []string
	P                   *postingView
	Score               *scoreView
	Body                []string
	CanShortlist        bool
	CanStart            bool     // shortlisted but no application yet (e.g. refused as a company duplicate)
	OverrideReasons     []string // draft.OverrideReasons, the closed vocabulary of the audited override
	AppID               int64
	A                   *appView
	Files               []fileView
	NeedsLang           bool
	LiveMode            bool
	Kill, EnvKill, Live bool
	CapDaily, CapHost   int
	Health              []Health
	// tracker page, inbox due block, retention display, review queue detail
	Due                       []dueView
	Timeline                  []tracker.Entry
	Statuses                  []string
	FollowUp                  string
	PostingDays, SnapshotDays int
	LastPrune                 string
	QueueDetail               map[int64]queueDetail
	AllAreas                  []string
	CanDismiss                bool
	Latest                    int64
}

type dueView struct {
	AppID                  int64
	Title, Company, Status string
	At                     time.Time
}

type queueDetail struct {
	Status string
	S1, S2 float64
	Top3   []match.Candidate
	Hits   int
	Excl   string
}

type filter struct{ State, Lang, Provider, Band string }

func (c *Console) newPage(g *gin.Context, titleKey, nav string) *page {
	p := &page{TitleKey: titleKey, HtmxSRI: c.htmxSRI}
	p.Lang, p.LangWanted = c.pickLang(g)
	p.Dir = i18n.Dir(p.Lang)
	p.LangOpts = c.langOpts(p.Lang)
	if s := api.SessionFrom(g.Request); s != nil {
		p.Authed, p.CSRF = true, s.CSRF
		for _, n := range navAll {
			n.Current = n.Href == nav
			p.Nav = append(p.Nav, n)
		}
	}
	return p
}

func (c *Console) render(g *gin.Context, status int, name string, p *page) {
	var buf bytes.Buffer
	if err := c.pages[name].ExecuteTemplate(&buf, "layout", p); err != nil {
		api.WriteError(g.Writer, g.Request, http.StatusInternalServerError, api.CodeInternal, "error.internal", nil)
		return
	}
	g.Data(status, "text/html; charset=utf-8", buf.Bytes())
}

// statusOf maps a draft error to its HTTP status and code.
func statusOf(err error) (int, string) {
	code := draft.CodeOf(err)
	switch code {
	case "not_found":
		return http.StatusNotFound, code
	case "validation", "language_confirmation_required":
		return http.StatusBadRequest, code
	case "step_up_required", "live_mode_off", "overlay_absent", "kill_switch_on", "cap_exceeded", "company_confirmation_failed", "approval_required", "gate_regressed", "gate_undetermined":
		return http.StatusForbidden, code
	case "illegal_transition", "approval_hash_mismatch", "slot_missing", "duplicate_send", "approval_expired", "posting_closed", "cover_stale", "duplicate_company", "templates_stale":
		return http.StatusConflict, code
	}
	return http.StatusInternalServerError, "internal"
}

// Register mounts the console routes and /static on e.
func (c *Console) Register(e *gin.Engine) {
	e.GET("/static/*filepath", func(g *gin.Context) { c.static.ServeHTTP(g.Writer, g.Request) })
	e.GET("/login", func(g *gin.Context) {
		p := c.newPage(g, "login.title", "")
		p.PasskeyJS = true
		c.render(g, http.StatusOK, "login", p)
	})
	e.GET("/", c.inbox)
	e.GET("/queue", c.queue)
	e.POST("/queue/:id/map", c.mapPosting)
	e.POST("/postings/:id/dismiss", c.dismiss)
	e.GET("/applications/:id/tracking", c.trackingPage)
	e.POST("/applications/:id/tracking", c.trackingRecord)
	e.POST("/applications/:id/follow-up", c.followUp)
	e.GET("/postings/:id", c.job)
	e.POST("/postings/:id/shortlist", c.shortlist)
	e.POST("/postings/:id/start-application", c.startApplication)
	e.GET("/applications", c.applications)
	e.POST("/applications/:id/prepare", c.act("prepare"))
	e.GET("/applications/:id/draft", c.draftPage)
	e.GET("/applications/:id/files/:kind", c.file)
	e.POST("/applications/:id/language", c.act("language"))
	e.POST("/applications/:id/approve", c.act("approve"))
	e.POST("/applications/:id/reject", c.act("reject"))
	e.POST("/applications/:id/request-live", c.act("request-live"))
	e.POST("/applications/:id/handoff-complete", c.act("handoff"))
	e.GET("/settings", c.settings)
	e.POST("/settings/kill-switch", c.setKill)
	e.POST("/settings/live-mode", c.setLive)
	e.POST("/settings/caps", c.setCaps)
	e.POST("/settings/language", c.setLanguage)
	e.GET("/audit", c.auditPage)
	e.GET("/audit.jsonl", c.auditJSONL)
}

func pid(g *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(g.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		api.WriteError(g.Writer, g.Request, http.StatusNotFound, api.CodeNotFound, "error.not_found", nil)
		return 0, false
	}
	return id, true
}

// ---- inbox, queue, job

type row struct {
	ID                                    int64
	Title, Company, State, Lang, Provider string
	Score                                 float64
	HasScore                              bool
}

func (c *Console) postingRows(ctx context.Context, where string, args ...any) ([]row, error) {
	q := `SELECT p.id,p.title,coalesce(p.company,''),p.state,coalesce(p.lang,''),pr.name,
		(SELECT total FROM score s WHERE s.posting_id=p.id AND s.is_current=1) FROM posting p JOIN provider pr ON pr.id=p.provider_id ` + where + ` ORDER BY p.id DESC LIMIT 200`
	rs, err := c.DB.Read().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		var sc sql.NullFloat64
		if err := rs.Scan(&r.ID, &r.Title, &r.Company, &r.State, &r.Lang, &r.Provider, &sc); err != nil {
			return nil, err
		}
		r.Score, r.HasScore = sc.Float64, sc.Valid
		out = append(out, r)
	}
	return out, rs.Err()
}

func (c *Console) providerNames(ctx context.Context) []string {
	var out []string
	rs, err := c.DB.Read().QueryContext(ctx, `SELECT name FROM provider ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rs.Close()
	for rs.Next() {
		var n string
		if rs.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	return out
}

func (c *Console) inbox(g *gin.Context) {
	f := filter{g.Query("state"), strings.ToLower(g.Query("lang")), g.Query("provider"), g.Query("band")}
	var conds []string
	var args []any
	if f.State != "" {
		conds, args = append(conds, "p.state=?"), append(args, f.State)
	}
	if f.Lang != "" {
		conds, args = append(conds, "p.lang=?"), append(args, f.Lang)
	}
	if f.Provider != "" {
		conds, args = append(conds, "pr.name=?"), append(args, f.Provider)
	}
	switch f.Band {
	case "shortlist":
		conds = append(conds, "(SELECT total FROM score s WHERE s.posting_id=p.id AND s.is_current=1) >= 70")
	case "review":
		conds = append(conds, "(SELECT total FROM score s WHERE s.posting_id=p.id AND s.is_current=1) >= 55 AND (SELECT total FROM score s WHERE s.posting_id=p.id AND s.is_current=1) < 70")
	case "discard":
		conds = append(conds, "(SELECT total FROM score s WHERE s.posting_id=p.id AND s.is_current=1) < 55")
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	rows, err := c.postingRows(g.Request.Context(), where, args...)
	if err != nil {
		api.WriteError(g.Writer, g.Request, http.StatusInternalServerError, api.CodeInternal, "error.internal", nil)
		return
	}
	p := c.newPage(g, "inbox.title", "/")
	p.Due = c.dueFollowUps(g.Request.Context())
	p.Rows, p.Filter, p.States, p.Bands, p.Providers = rows, f, fsm.PostingStates, []string{"shortlist", "review", "discard"}, c.providerNames(g.Request.Context())
	c.render(g, http.StatusOK, "inbox", p)
}

func (c *Console) queue(g *gin.Context) { c.renderQueue(g, http.StatusOK, "") }

// renderQueue lists postings whose state is unmapped or ambiguous with the matcher's own evidence (top-3 areas, hit
// count, exclusion) read through internal/matchstore.
func (c *Console) renderQueue(g *gin.Context, status int, code string) {
	rows, err := c.postingRows(g.Request.Context(), `WHERE p.state IN ('unmapped','ambiguous')`)
	if err != nil {
		api.WriteError(g.Writer, g.Request, http.StatusInternalServerError, api.CodeInternal, "error.internal", nil)
		return
	}
	p := c.newPage(g, "queue.title", "/queue")
	p.Rows, p.Error = rows, code
	p.AllAreas = matchstore.Areas()
	p.QueueDetail = map[int64]queueDetail{}
	if items, err := matchstore.ReviewQueue(g.Request.Context(), c.DB.Read()); err == nil {
		for _, it := range items {
			p.QueueDetail[it.PostingID] = queueDetail{Status: it.Status, S1: it.S1, S2: it.S2, Top3: it.Top3, Hits: len(it.Hits), Excl: it.Exclusion}
		}
	}
	c.render(g, status, "queue", p)
}

// mapPosting is the operator's manual map (FR-015) through matchstore.MapHuman: an audited, human-method mapping row
// becomes current, the machine row stays as history, and the posting moves to mapped (reason mapped_by_human).
func (c *Console) mapPosting(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	ctx := g.Request.Context()
	err := c.DB.Write(ctx, func(tx *sql.Tx) error {
		return matchstore.MapHuman(ctx, tx, id, g.PostForm("area_id"), "operator", c.Now())
	})
	if err != nil {
		code, st := "internal", http.StatusInternalServerError
		switch {
		case errors.Is(err, matchstore.ErrIllegalArea):
			code, st = "validation", http.StatusBadRequest
		case errors.Is(err, matchstore.ErrNotInQueue), errors.Is(err, audit.ErrIllegalTransition):
			code, st = "illegal_transition", http.StatusConflict
		}
		c.renderQueue(g, st, code)
		return
	}
	g.Redirect(http.StatusSeeOther, "/queue")
}

func statusFor(err error) int { s, _ := statusOf(err); return s }

// fail re-renders a list page with an error alert (works without script).
func (c *Console) fail(g *gin.Context, name, title, nav string, status int, code string) {
	if name == "queue" {
		c.renderQueue(g, status, code)
		return
	}
	p := c.newPage(g, title, nav)
	p.Error = code
	if name == "settings" {
		c.fillSettings(g.Request.Context(), p)
	}
	c.render(g, status, name, p)
}

// dismiss is the operator's triage "no": scored -> discarded with the closed reason dismissed_by_human (audited).
func (c *Console) dismiss(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	ctx := g.Request.Context()
	err := c.DB.Write(ctx, func(tx *sql.Tx) error {
		return audit.Transition(ctx, tx, audit.Spec{Kind: "posting", ID: id, From: "scored", To: "discarded", Actor: "operator", Reason: "dismissed_by_human"})
	})
	if err != nil {
		code, st := "illegal_transition", http.StatusConflict
		if errors.Is(err, sql.ErrNoRows) {
			code, st = "not_found", http.StatusNotFound
		}
		api.WriteError(g.Writer, g.Request, st, api.Code(code), "error."+code, nil)
		return
	}
	g.Redirect(http.StatusSeeOther, "/")
}

type postingView struct {
	ID                                               int64
	Title, Company, State, Lang, Provider, SourceURL string
}
type scoreRow struct {
	Name          string
	Value, Weight float64
}
type scoreView struct {
	Total float64
	Band  string
	Gates []string
	Rows  []scoreRow
}

func (c *Console) job(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	ctx := g.Request.Context()
	var pv postingView
	var body []byte
	var src sql.NullString
	err := c.DB.Read().QueryRowContext(ctx, `SELECT p.id,p.title,coalesce(p.company,''),p.state,coalesce(p.lang,''),pr.name,p.canonical_url,p.body_text FROM posting p JOIN provider pr ON pr.id=p.provider_id WHERE p.id=?`, id).
		Scan(&pv.ID, &pv.Title, &pv.Company, &pv.State, &pv.Lang, &pv.Provider, &src, &body)
	if err != nil {
		api.WriteError(g.Writer, g.Request, http.StatusNotFound, api.CodeNotFound, "error.not_found", nil)
		return
	}
	if u := src.String; strings.HasPrefix(u, "https://") {
		pv.SourceURL = u
	}
	p := c.newPage(g, "job.title", "/")
	p.P = &pv
	if len(body) > 0 {
		if pt, err := c.Keys.Open("posting", "body_text", id, body); err == nil {
			for _, l := range strings.Split(strings.ReplaceAll(string(pt), "\r", ""), "\n") {
				if l = strings.TrimSpace(l); l != "" {
					p.Body = append(p.Body, l)
				}
			}
		}
	}
	p.Score = c.scoreOf(ctx, id)
	p.CanShortlist = pv.State == "scored"
	p.CanDismiss = pv.State == "scored"
	_ = c.DB.Read().QueryRowContext(ctx, `SELECT id FROM application WHERE posting_id=?`, id).Scan(&p.AppID)
	p.CanStart = pv.State == "shortlisted" && p.AppID == 0
	p.OverrideReasons = draft.OverrideReasons
	c.render(g, http.StatusOK, "job", p)
}

func (c *Console) scoreOf(ctx context.Context, postingID int64) *scoreView {
	var total float64
	var blob []byte
	if c.DB.Read().QueryRowContext(ctx, `SELECT total, explain_json FROM score WHERE posting_id=? AND is_current=1`, postingID).Scan(&total, &blob) != nil {
		return nil
	}
	sv := &scoreView{Total: total}
	var sid int64
	_ = c.DB.Read().QueryRowContext(ctx, `SELECT id FROM score WHERE posting_id=? AND is_current=1`, postingID).Scan(&sid)
	if pt, err := c.Keys.Open("score", "explain_json", sid, blob); err == nil {
		var ex struct {
			Band       string             `json:"band"`
			Components map[string]float64 `json:"components"`
			Weights    map[string]float64 `json:"weights"`
			Gates      []string           `json:"gates"`
		}
		if jsonUnmarshal(pt, &ex) == nil {
			sv.Band, sv.Gates = ex.Band, ex.Gates
			for _, k := range sortedKeys(ex.Components) {
				sv.Rows = append(sv.Rows, scoreRow{k, ex.Components[k], ex.Weights[k]})
			}
		}
	}
	return sv
}

// shortlist is the operator's triage: scored -> shortlisted (audited) and the application row is created.
func (c *Console) shortlist(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	ctx := g.Request.Context()
	err := c.DB.Write(ctx, func(tx *sql.Tx) error {
		return audit.Transition(ctx, tx, audit.Spec{Kind: "posting", ID: id, From: "scored", To: "shortlisted", Actor: "operator", Reason: "shortlisted_by_human"})
	})
	if err != nil {
		code := "illegal_transition"
		if errors.Is(err, sql.ErrNoRows) {
			code = "not_found"
		}
		api.WriteError(g.Writer, g.Request, map[string]int{"not_found": 404, "illegal_transition": 409}[code], api.Code(code), "error."+code, nil)
		return
	}
	appID, err := c.Draft.CreateApplication(ctx, id, "operator")
	if err != nil {
		st, code := statusOf(err)
		api.WriteError(g.Writer, g.Request, st, api.Code(code), "error."+code, nil)
		return
	}
	g.Redirect(http.StatusSeeOther, fmt.Sprintf("/applications/%d/draft", appID))
}

// startApplication creates the application of an already shortlisted posting. The optional `override` form value is one code of
// draft.OverrideReasons: the audited, visible way past the company-level duplicate guard (JSK-T181).
func (c *Console) startApplication(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	appID, err := c.Draft.CreateApplicationOverride(g.Request.Context(), id, "operator", g.PostForm("override"))
	if err != nil {
		st, code := statusOf(err)
		api.WriteError(g.Writer, g.Request, st, api.Code(code), "error."+code, nil)
		return
	}
	g.Redirect(http.StatusSeeOther, fmt.Sprintf("/applications/%d/draft", appID))
}

// ---- applications and draft review

type appView struct {
	ID                                                                    int64
	Title, Company, State, DraftLang, LangRule, DraftHash, Host, ApplyURL string
}
type fileView struct{ Kind, Slot, SHA string }

func (c *Console) applications(g *gin.Context) {
	rs, err := c.DB.Read().QueryContext(g.Request.Context(), `SELECT a.id,p.title,coalesce(p.company,''),a.state FROM application a JOIN posting p ON p.id=a.posting_id ORDER BY a.id DESC LIMIT 200`)
	if err != nil {
		api.WriteError(g.Writer, g.Request, http.StatusInternalServerError, api.CodeInternal, "error.internal", nil)
		return
	}
	defer rs.Close()
	var out []appView
	for rs.Next() {
		var a appView
		if rs.Scan(&a.ID, &a.Title, &a.Company, &a.State) == nil {
			out = append(out, a)
		}
	}
	p := c.newPage(g, "apps.title", "/applications")
	p.Rows = out
	c.render(g, http.StatusOK, "applications", p)
}

func (c *Console) loadApp(ctx context.Context, id int64) (*appView, []fileView, error) {
	var a appView
	var lang, rule, hash, au, cu sql.NullString
	err := c.DB.Read().QueryRowContext(ctx, `SELECT a.id,p.title,coalesce(p.company,''),a.state,a.lang,a.lang_rule,a.draft_hash,p.apply_url,p.canonical_url FROM application a JOIN posting p ON p.id=a.posting_id WHERE a.id=?`, id).
		Scan(&a.ID, &a.Title, &a.Company, &a.State, &lang, &rule, &hash, &au, &cu)
	if err != nil {
		return nil, nil, draft.ErrNotFound
	}
	a.DraftLang, a.LangRule, a.DraftHash = lang.String, rule.String, hash.String
	for _, u := range []string{au.String, cu.String} {
		if strings.HasPrefix(u, "https://") {
			a.ApplyURL = u
			rest := strings.TrimPrefix(u, "https://")
			a.Host = strings.SplitN(strings.SplitN(rest, "/", 2)[0], "?", 2)[0]
			break
		}
	}
	rs, err := c.DB.Read().QueryContext(ctx, `SELECT kind,slot,content_sha256 FROM draft WHERE application_id=? ORDER BY kind`, id)
	if err != nil {
		return nil, nil, err
	}
	defer rs.Close()
	var files []fileView
	for rs.Next() {
		var f fileView
		if rs.Scan(&f.Kind, &f.Slot, &f.SHA) == nil {
			files = append(files, f)
		}
	}
	return &a, files, nil
}

func (c *Console) draftPage(g *gin.Context) {
	c.renderDraft(g, http.StatusOK, "", "")
}

func (c *Console) renderDraft(g *gin.Context, status int, errCode, notice string) {
	id, ok := pid(g)
	if !ok {
		return
	}
	a, files, err := c.loadApp(g.Request.Context(), id)
	if err != nil {
		api.WriteError(g.Writer, g.Request, http.StatusNotFound, api.CodeNotFound, "error.not_found", nil)
		return
	}
	p := c.newPage(g, "draft.title", "/applications")
	p.A, p.Files, p.Error, p.Notice = a, files, errCode, notice
	p.NeedsLang = strings.HasPrefix(a.LangRule, "confirm_")
	p.Langs, p.LiveMode = draft.SupportedLangs, c.Draft.Gate.LiveMode()
	c.render(g, status, "draft", p)
}

// act runs one draft-service action from a form post. Errors re-render the draft page with the closed code; success
// redirects (PRG), so the whole path works with scripting disabled.
func (c *Console) act(kind string) gin.HandlerFunc {
	return func(g *gin.Context) {
		id, ok := pid(g)
		if !ok {
			return
		}
		ctx := g.Request.Context()
		var err error
		switch kind {
		case "prepare":
			err = c.Draft.Prepare(ctx, id, "operator")
		case "language":
			err = c.Draft.ConfirmLanguage(ctx, id, g.PostForm("lang"), "operator")
		case "approve":
			in := draft.ApproveInput{AppID: id, Actor: "operator", Live: g.PostForm("live") == "1", CompanyConfirm: g.PostForm("company")}
			if s := api.SessionFrom(g.Request); s != nil {
				in.StepUpAt = s.StepUpAt
			}
			_, err = c.Draft.Approve(ctx, in)
		case "reject":
			err = c.Draft.Reject(ctx, id, "operator", g.PostForm("reason"))
		case "request-live":
			err = c.Draft.RequestLiveApproval(ctx, id, "operator")
		case "handoff":
			err = c.Draft.HandoffComplete(ctx, id, "operator", g.PostForm("ref"))
		}
		if err != nil {
			st, code := statusOf(err)
			c.renderDraft(g, st, code, "")
			return
		}
		g.Redirect(http.StatusSeeOther, fmt.Sprintf("/applications/%d/draft", id))
	}
}

func (c *Console) file(g *gin.Context) {
	id, ok := pid(g)
	if !ok {
		return
	}
	kind := g.Param("kind")
	if kind != "cv" && kind != "cover" {
		api.WriteError(g.Writer, g.Request, http.StatusNotFound, api.CodeNotFound, "error.not_found", nil)
		return
	}
	f, sha, err := c.Draft.File(g.Request.Context(), id, kind)
	if err == nil {
		err = c.servePDF(g, f, sha)
	}
	if err != nil && !g.Writer.Written() {
		st, code := statusOf(err)
		api.WriteError(g.Writer, g.Request, st, api.Code(code), "error."+code, nil)
	}
}

// servePDF stages the hash-verified bytes in a 0600 file on tmpfs (JSK-T094, FR-054), serves them from there so the
// viewer gets range requests, and zeroes and removes the file before returning. A host without tmpfs falls back to
// the in-memory stream, which never touches a disk either.
func (c *Console) servePDF(g *gin.Context, f templates.File, sha string) error {
	data, err := templates.ReadVerified(f, sha)
	if err != nil {
		return err
	}
	tmp, terr := privacy.NewTemp("", f.Name, data)
	if terr != nil {
		return templates.WriteVerified(g.Writer, f, sha, data)
	}
	defer tmp.Close()
	fh, err := os.Open(tmp.Path)
	if err != nil {
		return templates.WriteVerified(g.Writer, f, sha, data)
	}
	defer fh.Close()
	h := g.Writer.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Disposition", `inline; filename="`+f.Name+`"`)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Content-SHA256", sha)
	http.ServeContent(g.Writer, g.Request, f.Name, time.Time{}, fh)
	return nil
}

// ---- settings

func (c *Console) fillSettings(ctx context.Context, p *page) {
	p.Kill, p.EnvKill, p.Live = c.Draft.Gate.KillSwitch(ctx), c.Draft.Gate.EnvKill, c.Draft.Gate.LiveMode()
	p.CapDaily, p.CapHost = c.Draft.Gate.Caps(ctx)
	rt := c.Retention.Effective()
	p.PostingDays, p.SnapshotDays = rt.PostingDays, rt.SnapshotDays
	_ = c.DB.Read().QueryRowContext(ctx, `SELECT ts FROM audit_log WHERE reason_code='retention_prune' ORDER BY id DESC LIMIT 1`).Scan(&p.LastPrune)
	if c.Health != nil {
		p.Health, _ = c.Health(ctx)
	}
}

func (c *Console) settings(g *gin.Context) {
	p := c.newPage(g, "settings.title", "/settings")
	c.fillSettings(g.Request.Context(), p)
	p.PasskeyJS = true
	c.render(g, http.StatusOK, "settings", p)
}

func stepUpFresh(g *gin.Context, now time.Time) bool {
	s := api.SessionFrom(g.Request)
	return s != nil && s.StepUpFresh(now)
}

func (c *Console) settingsErr(g *gin.Context, status int, code string) {
	p := c.newPage(g, "settings.title", "/settings")
	p.Error, p.PasskeyJS = code, true
	c.fillSettings(g.Request.Context(), p)
	c.render(g, status, "settings", p)
}

// setKill: turning the kill switch ON never needs a step-up (it must always be one click); OFF does (contract).
func (c *Console) setKill(g *gin.Context) {
	on := g.PostForm("on") == "1"
	if !on && !stepUpFresh(g, c.Now()) {
		c.settingsErr(g, http.StatusForbidden, "step_up_required")
		return
	}
	if err := c.Draft.Gate.SetKillSwitch(g.Request.Context(), on, "operator"); err != nil {
		c.settingsErr(g, http.StatusInternalServerError, "internal")
		return
	}
	g.Redirect(http.StatusSeeOther, "/settings")
}

func (c *Console) setLive(g *gin.Context) {
	on := g.PostForm("on") == "1"
	if on && !stepUpFresh(g, c.Now()) {
		c.settingsErr(g, http.StatusForbidden, "step_up_required")
		return
	}
	if err := c.Draft.Gate.SetLiveMode(g.Request.Context(), on, "operator"); err != nil {
		c.settingsErr(g, http.StatusInternalServerError, "internal")
		return
	}
	g.Redirect(http.StatusSeeOther, "/settings")
}

func (c *Console) setCaps(g *gin.Context) {
	d, e1 := strconv.Atoi(g.PostForm("daily"))
	h, e2 := strconv.Atoi(g.PostForm("host"))
	if e1 != nil || e2 != nil || d < 0 || h < 0 || d > 1000 || h > 1000 {
		c.settingsErr(g, http.StatusBadRequest, "validation")
		return
	}
	if err := c.Draft.Gate.SetCaps(g.Request.Context(), d, h, "operator"); err != nil {
		c.settingsErr(g, http.StatusInternalServerError, "internal")
		return
	}
	g.Redirect(http.StatusSeeOther, "/settings")
}

// ---- audit

type auditRow struct {
	ID                              int64
	TS, Actor, From, To, Reason, Ev string
}

func (c *Console) auditRows(ctx context.Context, limit int) ([]auditRow, error) {
	rs, err := c.DB.Read().QueryContext(ctx, `SELECT id,ts,actor,coalesce(from_state,''),coalesce(to_state,''),reason_code,coalesce(evidence_json,'') FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []auditRow
	for rs.Next() {
		var r auditRow
		if err := rs.Scan(&r.ID, &r.TS, &r.Actor, &r.From, &r.To, &r.Reason, &r.Ev); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

func (c *Console) auditPage(g *gin.Context) {
	rows, err := c.auditRows(g.Request.Context(), 200)
	if err != nil {
		api.WriteError(g.Writer, g.Request, http.StatusInternalServerError, api.CodeInternal, "error.internal", nil)
		return
	}
	p := c.newPage(g, "audit.title", "/audit")
	p.Rows = rows
	c.render(g, http.StatusOK, "audit", p)
}

// auditJSONL exports the whole log, oldest first, one JSON object per line. Rows carry ids, states and reason codes only.
func (c *Console) auditJSONL(g *gin.Context) {
	rs, err := c.DB.Read().QueryContext(g.Request.Context(), `SELECT id,application_id,posting_id,coalesce(from_state,''),coalesce(to_state,''),actor,reason_code,coalesce(evidence_json,''),prev_hash,this_hash,ts FROM audit_log ORDER BY id`)
	if err != nil {
		api.WriteError(g.Writer, g.Request, http.StatusInternalServerError, api.CodeInternal, "error.internal", nil)
		return
	}
	defer rs.Close()
	g.Header("Content-Type", "application/x-ndjson")
	g.Header("Content-Disposition", `attachment; filename="audit.jsonl"`)
	g.Status(http.StatusOK)
	for rs.Next() {
		var id int64
		var app, post sql.NullInt64
		var from, to, actor, reason, ev, prev, this, ts string
		if rs.Scan(&id, &app, &post, &from, &to, &actor, &reason, &ev, &prev, &this, &ts) != nil {
			return
		}
		line, _ := jsonMarshal(map[string]any{"id": id, "application_id": nullable(app), "posting_id": nullable(post), "from": from, "to": to, "actor": actor, "reason": reason,
			"evidence": ev, "prev_hash": prev, "this_hash": this, "ts": ts})
		_, _ = g.Writer.Write(append(line, '\n'))
	}
}

// ---- language (FR-068..FR-070, JSK-T158)

const settingUILang = "ui.lang"

type langOpt struct {
	Code, Name        string
	Shipped, Selected bool
}

func (c *Console) shippedSet() map[string]bool {
	m := map[string]bool{}
	for _, l := range c.Cat.Shipped() {
		m[l] = true
	}
	return m
}

// pickLang: the operator's persisted choice if it is shipped, else Accept-Language negotiation. A preferred language that
// is not shipped yet comes back as wanted so the page says so; it is never silently replaced.
func (c *Console) pickLang(g *gin.Context) (lang, wanted string) {
	if c.DB != nil {
		var v string
		if c.DB.Read().QueryRowContext(g.Request.Context(), `SELECT value FROM setting WHERE key=?`, settingUILang).Scan(&v) == nil && c.shippedSet()[v] {
			return v, ""
		}
	}
	return i18n.Negotiate(g.GetHeader("Accept-Language"), c.Cat.Shipped())
}

func (c *Console) langOpts(cur string) []langOpt {
	sh := c.shippedSet()
	var out []langOpt
	for _, l := range i18n.Required {
		out = append(out, langOpt{l, i18n.Name(l), sh[l], l == cur})
	}
	return out
}

// setLanguage persists the operator's interface language. An unshipped or unknown language is refused visibly.
func (c *Console) setLanguage(g *gin.Context) {
	l := g.PostForm("lang")
	known := false
	for _, r := range i18n.Required {
		known = known || r == l
	}
	if !known {
		c.settingsErr(g, http.StatusBadRequest, "validation")
		return
	}
	if !c.shippedSet()[l] {
		c.settingsErr(g, http.StatusConflict, "lang_unavailable")
		return
	}
	err := c.DB.Write(g.Request.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(g.Request.Context(), `INSERT INTO setting(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, settingUILang, l)
		return err
	})
	if err != nil {
		c.settingsErr(g, http.StatusInternalServerError, "internal")
		return
	}
	g.Redirect(http.StatusSeeOther, "/settings")
}
