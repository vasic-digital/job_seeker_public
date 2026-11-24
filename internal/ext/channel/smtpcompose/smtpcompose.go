// Package smtpcompose is the compose-only mail ApplicationChannel (JSK-T231, Q-ARCH-3): with the operator's own
// OAuth access token it CREATES A DRAFT in the operator's mailbox through the mail provider's HTTPS draft endpoint
// and hands the human a link to it. The human presses Send. Nothing in this package sends: the Mailer port has one
// method, CreateDraft, and a reflection test pins that.
//
// Transport: the draft endpoint is HTTPS on 443, so it goes through the egress client and its allow-list like every
// other outbound call (SMTP/IMAP ports cannot pass the egress policy). Credentials are read from the environment BY
// NAME at call time and never stored, logged or returned in an error.
package smtpcompose

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/channel/mailto"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// DraftRef identifies the created draft at the provider.
type DraftRef struct{ DraftID, MessageID string }

// Mailer creates drafts. It deliberately has no send method.
type Mailer interface {
	CreateDraft(ctx context.Context, m Message) (DraftRef, error)
}

// GmailEndpoint is the draft-creation endpoint (Gmail API users.drafts.create). The host must be on the egress
// allow-list for this to work; it is not enabled by default.
const GmailEndpoint = "https://gmail.googleapis.com/gmail/v1/users/me/drafts"

var reID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// GmailMailer implements Mailer over the Gmail API with an OAuth bearer token.
type GmailMailer struct {
	HTTP     *http.Client                // the egress client's HTTP()
	TokenEnv string                      // NAME of the environment variable holding the access token
	Lookup   func(string) (string, bool) // environment lookup (os.LookupEnv in production)
	Endpoint string                      // empty: GmailEndpoint
}

// CreateDraft builds the message and creates exactly one draft.
func (g *GmailMailer) CreateDraft(ctx context.Context, m Message) (DraftRef, error) {
	raw, err := Build(m)
	if err != nil {
		return DraftRef{}, err
	}
	if g.HTTP == nil || g.Lookup == nil || g.TokenEnv == "" {
		return DraftRef{}, errors.New("smtpcompose: mailer not configured")
	}
	tok, ok := g.Lookup(g.TokenEnv)
	if !ok || strings.TrimSpace(tok) == "" || strings.ContainsAny(tok, "\r\n ") {
		return DraftRef{}, errors.New("smtpcompose: access token not available in the named environment variable")
	}
	body, _ := json.Marshal(map[string]any{"message": map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw)}})
	ep := g.Endpoint
	if ep == "" {
		ep = GmailEndpoint
	}
	if u, perr := url.Parse(ep); perr != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		// the bearer token must never leave over anything but https, whatever client was injected (JSK-T231 residual)
		return DraftRef{}, errors.New("smtpcompose: endpoint must be an https URL with a host and no credentials")
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, ep, bytes.NewReader(body))
	if err != nil {
		return DraftRef{}, errors.New("smtpcompose: bad endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	// A redirect must never carry the bearer token (or the body) anywhere the first URL check did not vouch for:
	// refuse every redirect on a copy of the injected client.
	hc := *g.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return DraftRef{}, errors.New("smtpcompose: draft request failed") // never wrap err: it can carry URL details
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return DraftRef{}, fmt.Errorf("smtpcompose: draft endpoint refused (status %d)", resp.StatusCode)
	}
	var out struct {
		ID      string `json:"id"`
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	if json.Unmarshal(rb, &out) != nil || !reID.MatchString(out.ID) || !reID.MatchString(out.Message.ID) {
		return DraftRef{}, errors.New("smtpcompose: unexpected draft response")
	}
	return DraftRef{DraftID: out.ID, MessageID: out.Message.ID}, nil
}

// Channel implements ext.ApplicationChannel. Every dependency is explicit; a missing one refuses.
type Channel struct {
	Mailer Mailer
	From   string // the operator's own address
	// Deliverability must answer Yes (T240: SPF/DKIM/DMARC of the sending domain); nil or anything else refuses.
	Deliverability func(context.Context) ext.Decision
	Now            func() time.Time
	Body           string // fixed template text from the templates, never generated (FR-029)
	// Attach returns the approved, frozen draft files for the approval (the human also attaches nothing else).
	Attach func(context.Context, ext.Approved) ([]Attachment, error)
	// MessageID builds the Message-ID; empty uses a value derived from the approval.
	MessageID func(ext.Approved) string
}

// Info implements ext.Plugin.
func (*Channel) Info() ext.Info {
	return ext.Info{ID: "smtp_compose", Kind: "channel", Version: "1.0.0", Priority: 30,
		Caps: map[string]bool{"direct_send": false, "creates_draft": true, "needs_human": true}, Spec: "FR-098, Q-ARCH-3: compose-only"}
}

// Supports: the posting's apply URL is a safe mailto: link.
func (*Channel) Supports(p providers.Posting) bool {
	_, err := mailto.Parse(p.ApplyURL)
	return err == nil
}

var errRefused = func(why string) error { return &ext.ErrFailClosed{Why: why} }

// Prepare creates the draft and returns the link the human opens to review and send it.
func (c *Channel) Prepare(ctx context.Context, a ext.Approved) (ext.Handoff, error) {
	if !a.Valid() {
		return ext.Handoff{}, errRefused("no approval")
	}
	m, err := mailto.Parse(a.URL)
	if err != nil {
		return ext.Handoff{}, errRefused("apply url is not a safe mailto link")
	}
	if c.Mailer == nil || c.Deliverability == nil || c.Attach == nil || c.Now == nil {
		return ext.Handoff{}, errRefused("channel not configured")
	}
	if d := c.Deliverability(ctx); d.Verdict != ext.Yes {
		return ext.Handoff{}, errRefused("sender deliverability is not Yes")
	}
	subject := m.Subject
	if subject == "" {
		subject = a.Subject
	}
	if subject == "" {
		return ext.Handoff{}, errRefused("no fixed-template subject")
	}
	atts, err := c.Attach(ctx, a)
	if err != nil {
		return ext.Handoff{}, errRefused("approved files unavailable")
	}
	mid := fmt.Sprintf("<jsk.%d.%s@%s>", a.AppID, shortHash(a.DraftHash), domainOf(c.From))
	if c.MessageID != nil {
		mid = c.MessageID(a)
	}
	ref, err := c.Mailer.CreateDraft(ctx, Message{From: c.From, To: m.To, Subject: subject, Body: c.Body, MessageID: mid, Date: c.Now(), Attachments: atts})
	if err != nil {
		return ext.Handoff{}, err
	}
	return ext.Handoff{Kind: "open_url", Link: "https://mail.google.com/mail/u/0/#drafts?compose=" + ref.MessageID,
		Notes: []string{"a draft was created in your mailbox; review it and press Send yourself", "nothing has been sent"}}, nil
}

func shortHash(h string) string {
	var b strings.Builder
	for _, r := range h {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
		if b.Len() == 12 {
			break
		}
	}
	return b.String()
}

func domainOf(addr string) string {
	if _, d, ok := strings.Cut(addr, "@"); ok {
		return d
	}
	return "invalid.example"
}
