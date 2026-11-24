package smtpcompose

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ext"
)

var t0 = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

const synthToken = "SYNTHETIC-TOKEN-0123456789abcdef"

// memListener hands out in-memory pipe connections, so the transport test needs no real socket and no raw dialer
// (the egress ban lint forbids net.Dialer outside internal/egress). This is a TEST DOUBLE of the mail API: it proves
// only this package's own request construction and refusal logic, never that a real mailbox accepted a draft.
type memListener struct {
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
}

func newMem() *memListener { return &memListener{ch: make(chan net.Conn), done: make(chan struct{})} }
func (l *memListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *memListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *memListener) Addr() net.Addr { return &net.TCPAddr{} }
func (l *memListener) dial() (net.Conn, error) {
	a, b := net.Pipe()
	select {
	case l.ch <- b:
		return a, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

type resolver struct{}

func (resolver) LookupIP(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}

type rig struct {
	m     *GmailMailer
	paths *[]string
	auth  *[]string
	raws  *[]string
	hits  *int32
}

func newRig(t *testing.T, h http.HandlerFunc) *rig {
	t.Helper()
	var mu sync.Mutex
	var paths, auth, raws []string
	var hits int32
	ln := newMem()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		b, _ := io.ReadAll(r.Body)
		var body struct {
			Message struct {
				Raw string `json:"raw"`
			} `json:"message"`
		}
		_ = json.Unmarshal(b, &body)
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		auth = append(auth, r.Header.Get("Authorization"))
		raws = append(raws, body.Message.Raw)
		mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":"r-1234567890","message":{"id":"18c0ffee","threadId":"t1"}}`))
	}))
	srv.Listener = ln
	srv.StartTLS()
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	c, err := egress.New(egress.Policy{Hosts: []string{"example.com"}}, egress.Options{Resolver: resolver{}, RootCAs: pool,
		BaseDial: func(context.Context, string, string) (net.Conn, error) { return ln.dial() }})
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"JSK_MAIL_TOKEN": synthToken}
	m := &GmailMailer{Endpoint: "https://example.com/gmail/v1/users/me/drafts", HTTP: c.HTTP(), TokenEnv: "JSK_MAIL_TOKEN", Lookup: func(k string) (string, bool) { v, ok := env[k]; return v, ok }}
	return &rig{m: m, paths: &paths, auth: &auth, raws: &raws, hits: &hits}
}

func msg() Message {
	return Message{From: "me@example.test", To: "jobs@example.test", Subject: "Application: Backend Engineer", Body: "Dear team,\r\nfixed text é\r\n",
		MessageID: "<jsk.1@example.test>", Date: t0,
		Attachments: []Attachment{{Name: "cv.pdf", MIME: "application/pdf", Data: []byte("%PDF-1.4 synthetic\x00\xff")}}}
}

func TestBuildRoundTrips(t *testing.T) {
	raw, err := Build(msg())
	if err != nil {
		t.Fatal(err)
	}
	m, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	dec := new(mime.WordDecoder)
	subj, _ := dec.DecodeHeader(m.Header.Get("Subject"))
	if subj != "Application: Backend Engineer" || m.Header.Get("To") != "jobs@example.test" || m.Header.Get("From") != "me@example.test" {
		t.Fatalf("headers %v", m.Header)
	}
	if m.Header.Get("X-Mailer") != "" || m.Header.Get("Disposition-Notification-To") != "" {
		t.Fatal("fingerprint or receipt header set")
	}
	mt, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/mixed" {
		t.Fatalf("%s %v", mt, err)
	}
	mr := multipart.NewReader(m.Body, params["boundary"])
	p1, _ := mr.NextPart()
	b1, _ := io.ReadAll(p1)
	if !strings.Contains(p1.Header.Get("Content-Type"), "text/plain") || !strings.Contains(string(b1), "fixed text") && !strings.Contains(string(b1), "=C3=A9") {
		t.Fatalf("body part %q %q", p1.Header, b1)
	}
	p2, _ := mr.NextPart()
	enc, _ := io.ReadAll(p2)
	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(enc)), ""))
	if err != nil || string(data) != "%PDF-1.4 synthetic\x00\xff" {
		t.Fatalf("attachment corrupted: %v", err)
	}
	if !strings.HasPrefix(p2.FileName(), "cv.pdf") {
		t.Fatalf("filename %q", p2.FileName())
	}
}

func TestBuildRefusesHeaderInjection(t *testing.T) {
	for name, mod := range map[string]func(*Message){
		"to crlf":        func(m *Message) { m.To = "a@example.test\r\nBcc: x@y.test" },
		"from lf":        func(m *Message) { m.From = "a@example.test\nBcc: x@y.test" },
		"subject crlf":   func(m *Message) { m.Subject = "hi\r\nBcc: x@y.test" },
		"filename crlf":  func(m *Message) { m.Attachments[0].Name = "cv.pdf\r\nX: y" },
		"filename path":  func(m *Message) { m.Attachments[0].Name = "../cv.pdf" },
		"messageid crlf": func(m *Message) { m.MessageID = "<a@b>\r\nBcc: x" },
		"two recipients": func(m *Message) { m.To = "a@example.test, b@example.test" },
		"display name":   func(m *Message) { m.To = "Bob <a@example.test>" },
		"empty to":       func(m *Message) { m.To = "" },
		"mime header":    func(m *Message) { m.Attachments[0].MIME = "application/pdf\r\nX: y" },
		"huge":           func(m *Message) { m.Attachments[0].Data = make([]byte, MaxMessageBytes) },
	} {
		m := msg()
		m.Attachments = append([]Attachment(nil), m.Attachments...)
		mod(&m)
		if _, err := Build(m); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestMailerCreatesExactlyOneDraftAndNothingElse(t *testing.T) {
	r := newRig(t, nil)
	ref, err := r.m.CreateDraft(context.Background(), msg())
	if err != nil {
		t.Fatal(err)
	}
	if ref.DraftID != "r-1234567890" || ref.MessageID != "18c0ffee" {
		t.Fatalf("%+v", ref)
	}
	if len(*r.paths) != 1 || (*r.paths)[0] != "POST /gmail/v1/users/me/drafts" {
		t.Fatalf("requests: %v (only the draft endpoint may be called, never a send endpoint)", *r.paths)
	}
	if (*r.auth)[0] != "Bearer "+synthToken {
		t.Fatal("bearer token not sent from the named env var")
	}
	raw, err := base64.RawURLEncoding.DecodeString((*r.raws)[0])
	if err != nil || !strings.Contains(string(raw), "Subject:") {
		t.Fatalf("raw not the encoded RFC 5322 message: %v", err)
	}
}

func TestMailerErrorsNeverLeakTokenOrBody(t *testing.T) {
	r := newRig(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"bad token ` + synthToken + ` for jobs@example.test"}`))
	})
	_, err := r.m.CreateDraft(context.Background(), msg())
	if err == nil {
		t.Fatal("401 accepted")
	}
	for _, leak := range []string{synthToken, "jobs@example.test", "bad token"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("error leaks %q: %v", leak, err)
		}
	}
	for name, body := range map[string]string{"not json": "<html>", "no id": `{"message":{}}`, "bad id chars": `{"id":"a b\r\n","message":{"id":"x"}}`, "empty": ``} {
		r2 := newRig(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
		if _, err := r2.m.CreateDraft(context.Background(), msg()); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestMissingTokenMakesNoRequest(t *testing.T) {
	r := newRig(t, nil)
	r.m.Lookup = func(string) (string, bool) { return "", false }
	if _, err := r.m.CreateDraft(context.Background(), msg()); err == nil || atomic.LoadInt32(r.hits) != 0 {
		t.Fatalf("err=%v hits=%d", err, *r.hits)
	}
	if err := errors.Unwrap(errors.New("x")); err != nil {
		t.Fatal(err)
	}
}

func TestMailerHasNoSendMethod(t *testing.T) {
	mt := reflect.TypeOf((*Mailer)(nil)).Elem()
	if mt.NumMethod() != 1 || mt.Method(0).Name != "CreateDraft" {
		t.Fatalf("Mailer must expose CreateDraft only, has %d methods", mt.NumMethod())
	}
	gt := reflect.TypeOf(&GmailMailer{})
	for i := 0; i < gt.NumMethod(); i++ {
		if n := strings.ToLower(gt.Method(i).Name); strings.Contains(n, "send") {
			t.Fatalf("GmailMailer has a send-like method %s", gt.Method(i).Name)
		}
	}
}

func channel(t *testing.T, r *rig, deliv func(context.Context) ext.Decision) *Channel {
	t.Helper()
	return &Channel{Mailer: r.m, From: "me@example.test", Deliverability: deliv, Now: func() time.Time { return t0 },
		Body: "Dear hiring team,\r\nplease find my application attached.\r\n",
		Attach: func(context.Context, ext.Approved) ([]Attachment, error) {
			return []Attachment{{Name: "cv.pdf", MIME: "application/pdf", Data: []byte("%PDF-1.4")}}, nil
		}}
}

func yes(context.Context) ext.Decision { return ext.Decision{Verdict: ext.Yes} }

func TestChannelRefusesWithoutApprovalWithoutTouchingTheNetwork(t *testing.T) {
	r := newRig(t, nil)
	c := channel(t, r, yes)
	for name, a := range map[string]ext.Approved{"zero": {}, "forged": {AppID: 1, DraftHash: "h", URL: "mailto:jobs@example.test", Subject: "Application: x"}} {
		if _, err := c.Prepare(context.Background(), a); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if atomic.LoadInt32(r.hits) != 0 {
		t.Fatal("network touched without an approval")
	}
}

func TestChannelNeedsDeliverabilityYes(t *testing.T) {
	a, _ := ext.NewApproved(1, "operator", "h", "h", "mailto:jobs@example.test")
	a, _ = a.WithSubject("Application: Backend Engineer")
	for name, d := range map[string]func(context.Context) ext.Decision{
		"nil":     nil,
		"unknown": func(context.Context) ext.Decision { return ext.Decision{Verdict: ext.Unknown} },
		"no":      func(context.Context) ext.Decision { return ext.Decision{Verdict: ext.No} },
	} {
		r := newRig(t, nil)
		if _, err := channel(t, r, d).Prepare(context.Background(), a); err == nil {
			t.Errorf("%s: draft created without a Yes deliverability check", name)
		}
		if atomic.LoadInt32(r.hits) != 0 {
			t.Errorf("%s: network touched", name)
		}
	}
}

func TestChannelCreatesDraftForApprovedMailtoAndHumanSends(t *testing.T) {
	r := newRig(t, nil)
	a, _ := ext.NewApproved(1, "operator", "h", "h", "mailto:jobs@example.test?subject=Ref%204521")
	a, _ = a.WithSubject("Application: ignored")
	h, err := channel(t, r, yes).Prepare(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if h.Kind != "open_url" || !strings.HasPrefix(h.Link, "https://mail.google.com/") || !strings.Contains(strings.Join(h.Notes, " "), "press Send") {
		t.Fatalf("%+v", h)
	}
	if len(*r.paths) != 1 {
		t.Fatalf("requests %v", *r.paths)
	}
	raw, _ := base64.RawURLEncoding.DecodeString((*r.raws)[0])
	dec := new(mime.WordDecoder)
	m, _ := mail.ReadMessage(strings.NewReader(string(raw)))
	if s, _ := dec.DecodeHeader(m.Header.Get("Subject")); s != "Ref 4521" || m.Header.Get("To") != "jobs@example.test" {
		t.Fatalf("subject/to %q %q", s, m.Header.Get("To"))
	}
	// non-mailto and injected URLs are refused before any request
	for _, u := range []string{"https://x.example/apply", "mailto:a@b.test?bcc=x@y.test"} {
		bad, _ := ext.NewApproved(1, "operator", "h", "h", u)
		before := atomic.LoadInt32(r.hits)
		if _, err := channel(t, r, yes).Prepare(context.Background(), bad); err == nil || atomic.LoadInt32(r.hits) != before {
			t.Errorf("%s: err=%v", u, err)
		}
	}
}

func TestAdmissionAndCaps(t *testing.T) {
	i := (&Channel{}).Info()
	if err := ext.ChannelVet(i); err != nil {
		t.Fatal(err)
	}
	if i.Caps["direct_send"] || !i.Caps["creates_draft"] || !i.Caps["needs_human"] {
		t.Fatalf("%v", i.Caps)
	}
}

func TestTransportErrorsDoNotCarryURLs(t *testing.T) {
	r := newRig(t, nil)
	r.m.Endpoint = "https://not-allowed.example.org/secret/path?token=abc"
	_, err := r.m.CreateDraft(context.Background(), msg())
	if err == nil {
		t.Fatal("a host outside the egress allow-list was contacted")
	}
	for _, leak := range []string{"not-allowed.example.org", "secret/path", "token=abc"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("error leaks %q: %v", leak, err)
		}
	}
	if atomic.LoadInt32(r.hits) != 0 {
		t.Fatal("request reached the server")
	}
}

// JSK-T231 residual: the bearer token must never travel over a non-https endpoint, whatever the caller sets
// and whatever HTTP client it injected (a plain client has no egress policy to fall back on).
func TestEndpointMustBeHTTPS(t *testing.T) {
	var hits int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"id":"r-1234567890","message":{"id":"18c0ffee"}}`))
	}))
	t.Cleanup(plain.Close)
	for _, ep := range []string{plain.URL + "/gmail/v1/users/me/drafts", "HTTP://127.0.0.1:1/x", "ftp://example.com/x",
		"//example.com/x", "example.com/x", "https:///nohost", "https://user:pw@example.com/x", "file:///etc/passwd"} {
		m := &GmailMailer{Endpoint: ep, HTTP: plain.Client(), TokenEnv: "JSK_MAIL_TOKEN",
			Lookup: func(string) (string, bool) { return synthToken, true }}
		_, err := m.CreateDraft(context.Background(), msg())
		if err == nil {
			t.Errorf("endpoint %q was accepted", ep)
			continue
		}
		if strings.Contains(err.Error(), synthToken) || strings.Contains(err.Error(), "pw@") {
			t.Errorf("error leaks for %q: %v", ep, err)
		}
	}
	// userinfo on an otherwise valid https endpoint: a real TLS server would answer, so only the refusal keeps it silent
	var tlsHits int32
	tl := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tlsHits, 1)
		_, _ = w.Write([]byte(`{"id":"r-1234567890","message":{"id":"18c0ffee"}}`))
	}))
	t.Cleanup(tl.Close)
	um := &GmailMailer{Endpoint: strings.Replace(tl.URL, "https://", "https://user:pw@", 1) + "/x", HTTP: tl.Client(), TokenEnv: "JSK_MAIL_TOKEN",
		Lookup: func(string) (string, bool) { return synthToken, true }}
	if _, err := um.CreateDraft(context.Background(), msg()); err == nil || atomic.LoadInt32(&tlsHits) != 0 {
		t.Errorf("an endpoint carrying credentials was used (err=%v hits=%d)", err, tlsHits)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Errorf("the bearer token reached a plain-http server %d time(s)", hits)
	}
}

// review round 1: a redirect from an https endpoint to plain http must not carry the bearer token.
func TestRedirectDoesNotCarryTheToken(t *testing.T) {
	var hits int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&hits, 1) }))
	t.Cleanup(plain.Close)
	tl := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/x", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(tl.Close)
	m := &GmailMailer{Endpoint: tl.URL + "/x", HTTP: tl.Client(), TokenEnv: "JSK_MAIL_TOKEN", Lookup: func(string) (string, bool) { return synthToken, true }}
	if _, err := m.CreateDraft(context.Background(), msg()); err == nil || atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("redirect followed: err=%v hits=%d", err, hits)
	}
}
