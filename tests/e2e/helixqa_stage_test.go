package e2e

// Setup and sink-side read-back helpers for the state-changing HelixQA bank (JSK-T084 / JSK-T111).
//
// stageStateFlows prepares the SYNTHETIC rows through the real console endpoints (the same POSTs a human's browser sends), so that
// the bank has something to approve, reject and prepare. That is SETUP, not evidence: no verdict is derived from it.
// readBack opens the daemon's encrypted database after helixqa exited and records what the console actions did to it, plus
// whether the audit hash chain still verifies. That is the second, non-vision evidence source (umbrella 11.4.68).

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/store"
)

var csrfRe = regexp.MustCompile(`name="_csrf" value="([^"]+)"`)

type consoleClient struct {
	base, cookie string
	hc           *http.Client
	t            *testing.T
}

func newConsoleClient(t *testing.T, base, caFile, cookieFile string) *consoleClient {
	pem, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pem)
	raw, err := os.ReadFile(cookieFile)
	if err != nil {
		t.Fatal(err)
	}
	var cs []struct{ Name, Value string }
	if err := json.Unmarshal(raw, &cs); err != nil || len(cs) != 1 {
		t.Fatalf("cookie file: %v (%d entries)", err, len(cs))
	}
	return &consoleClient{base: base, cookie: cs[0].Name + "=" + cs[0].Value, t: t,
		hc: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *consoleClient) do(method, path string, form url.Values) (int, string, string) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, c.base+path, body)
	req.Header.Set("Cookie", c.cookie)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", c.base)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Location"), string(b)
}

func (c *consoleClient) csrf() string {
	_, _, b := c.do("GET", "/settings", nil)
	m := csrfRe.FindStringSubmatch(b)
	if m == nil {
		c.t.Fatal("no CSRF token on /settings")
	}
	return m[1]
}

// stageStateFlows: application 1 (posting 1) and 2 (posting 2) are prepared (awaiting_human_approval); application 3 (posting 6)
// is only shortlisted (pending_draft). The ids are asserted, because the bank taps rows by position.
func stageStateFlows(t *testing.T, c *consoleClient) {
	tok := c.csrf()
	for i, pid := range []int{1, 2, 6} {
		st, loc, _ := c.do("POST", fmt.Sprintf("/postings/%d/shortlist", pid), url.Values{"_csrf": {tok}})
		want := fmt.Sprintf("/applications/%d/draft", i+1)
		if st != 303 || loc != want {
			t.Fatalf("shortlist posting %d: status %d location %q, want 303 %s", pid, st, loc, want)
		}
		if pid != 6 {
			if st, _, b := c.do("POST", want[:len(want)-len("draft")]+"prepare", url.Values{"_csrf": {tok}}); st != 303 {
				t.Fatalf("prepare app %d: %d %s", i+1, st, regexp.MustCompile(`(?s)<main.*</main>`).FindString(b))
			}
		}
	}
}

type stateRow struct {
	ID        int64  `json:"id"`
	Posting   int64  `json:"posting_id"`
	State     string `json:"state"`
	Approvals int    `json:"approval_rows"`
	Subs      int    `json:"submission_rows"`
	SubKinds  string `json:"submission_kinds"`
}

type readBackDoc struct {
	Applications     []stateRow        `json:"applications"`
	PostingStates    map[string]string `json:"posting_states"`
	Mapping5         string            `json:"posting5_mapping_status"`
	AuditTransitions []string          `json:"audit_transitions"`
	ChainOK          bool              `json:"audit_chain_ok"`
	ChainErr         string            `json:"audit_chain_error,omitempty"`
	LiveMode         string            `json:"live_mode_setting"`
	KillSwitch       string            `json:"kill_switch_setting"`
	HandoffRows      int               `json:"handoff_rows"`
}

func readBack(t *testing.T, dbPath, out string) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Path: dbPath, BackupDir: dbPath + ".rb"})
	if err != nil {
		t.Fatalf("read-back open: %v", err)
	}
	defer db.Close()
	var d readBackDoc
	d.PostingStates = map[string]string{}
	rs, err := db.Read().QueryContext(ctx, `SELECT a.id,a.posting_id,a.state,
		(SELECT count(*) FROM approval x WHERE x.application_id=a.id),
		(SELECT count(*) FROM submission s WHERE s.application_id=a.id),
		coalesce((SELECT group_concat(s.kind) FROM submission s WHERE s.application_id=a.id),'') FROM application a ORDER BY a.id`)
	if err != nil {
		t.Fatalf("read-back applications: %v", err)
	}
	for rs.Next() {
		var r stateRow
		if err := rs.Scan(&r.ID, &r.Posting, &r.State, &r.Approvals, &r.Subs, &r.SubKinds); err != nil {
			t.Fatal(err)
		}
		d.Applications = append(d.Applications, r)
	}
	rs.Close()
	pr, err := db.Read().QueryContext(ctx, `SELECT id,state FROM posting WHERE id IN (1,2,5,6,7) ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for pr.Next() {
		var id int64
		var s string
		pr.Scan(&id, &s)
		d.PostingStates[fmt.Sprint(id)] = s
	}
	pr.Close()
	_ = db.Read().QueryRowContext(ctx, `SELECT status FROM mapping WHERE posting_id=5 AND is_current=1`).Scan(&d.Mapping5)
	for _, k := range []string{"live_mode", "kill_switch"} {
		var v sql.NullString
		_ = db.Read().QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, k).Scan(&v)
		if k == "live_mode" {
			d.LiveMode = v.String
		} else {
			d.KillSwitch = v.String
		}
	}
	_ = db.Read().QueryRowContext(ctx, `SELECT count(*) FROM submission WHERE kind='handoff'`).Scan(&d.HandoffRows)
	tr, err := db.Read().QueryContext(ctx, `SELECT coalesce('app'||application_id,'posting'||posting_id)||' '||coalesce(from_state,'')||'->'||coalesce(to_state,'')||' ['||reason_code||']' FROM audit_log ORDER BY id`)
	if err == nil {
		for tr.Next() {
			var s string
			tr.Scan(&s)
			d.AuditTransitions = append(d.AuditTransitions, s)
		}
		tr.Close()
	} else {
		d.AuditTransitions = []string{"audit query failed: " + err.Error()}
	}
	vr := audit.Verify(ctx, db.Read(), "")
	d.ChainOK, d.ChainErr = vr.ChainOK, vr.String()
	b, _ := json.MarshalIndent(d, "", "  ")
	if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
