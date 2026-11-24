# 04 - Email and transport protocols

Index: [00-README-index.md](00-README-index.md). Legal context for unsolicited mail and GDPR is in [05](05-identity-trust-privacy-and-legal.md). Retrieval date **2026-11-24**. Tags: `[S]` secondary (vendor blogs/search summaries; official pages for Google/Yahoo/Microsoft were NOT fetched), `[K]` RFC/spec cited from background knowledge and not re-fetched (edition/status UNCONFIRMED), `[P]` primary fetched.

## Contents

1. [Scope and the human-approval boundary](#1-scope-and-the-human-approval-boundary)
2. [Standards table](#2-standards-table)
3. [Deliverability: SPF, DKIM, DMARC, ARC, bulk-sender rules](#3-deliverability)
4. [Message construction: mailto, MIME, subject, attachments](#4-message-construction)
5. [Sending paths: SMTP, Gmail API, Microsoft Graph, OAuth2](#5-sending-paths)
6. [Reply tracking: IMAP, threading](#6-reply-tracking)
7. [Encryption and signing: S/MIME, PGP](#7-encryption-and-signing)
8. [Unsolicited-mail rules](#8-unsolicited-mail-rules)
9. [Integration for job_seeker](#9-integration-for-job_seeker)
10. [Priority table](#10-priority-table)

## 1. Scope and the human-approval boundary

Some postings say "send your CV to jobs@company". That is a legitimate application channel. job_seeker **prepares** the message (draft state) and a human **approves** the send (existing FSM, `internal/fsm`; spec: the system never approves or sends on its own). Everything below is about making an approved message arrive, be read, and be attributable.

## 2. Standards table

| Standard | Spec | Role | Priority |
|---|---|---|---|
| Internet Message Format | RFC 5322 `[K]` | headers, `Date`, `Message-ID`, `In-Reply-To`, `References` | MUST |
| MIME | RFC 2045-2049, 2231 (filenames), 6532 (internationalised headers) `[K]` | multipart/mixed + alternative, UTF-8, attachments | MUST |
| SMTP / Submission | RFC 5321, RFC 6409 (port 587 STARTTLS), RFC 8314 (implicit TLS 465) `[K]` | sending | MUST |
| SPF | RFC 7208 `[K]` | sender IP authorisation | MUST (domain owner) |
| DKIM | RFC 6376 `[K]` | message signature | MUST (domain owner) |
| DMARC | RFC 7489 `[K]` (successor work in IETF: status UNCONFIRMED) | policy + alignment | MUST (domain owner) |
| ARC | RFC 8617 `[K]` | forwarding chain | MAY (only matters to intermediaries) |
| mailto URI | RFC 6068 `[K]` | parse `mailto:` apply links incl. `subject`, `body`, `cc` | MUST |
| List-Unsubscribe / one-click | RFC 2369, RFC 8058 `[K]` | bulk mail only | N/A for individual applications |
| IMAP | RFC 9051 (IMAP4rev2), RFC 3501 `[K]` | read replies | SHOULD |
| OAuth 2.0 + PKCE | RFC 6749, 7636, 9700 (security BCP) `[K]` | mailbox access without passwords | MUST for Gmail/Outlook |
| SASL XOAUTH2 | vendor spec (Google/Microsoft) `[K]` | IMAP/SMTP with OAuth tokens | SHOULD |
| S/MIME | RFC 8551 `[K]` | signing/encryption | MAY |
| OpenPGP | RFC 9580 `[K]` | signing/encryption | MAY |
| DSN / MDN | RFC 3461-3464, 8098 `[K]` | delivery/read receipts | MAY (see risks) |
| MTA-STS / TLS-RPT / DANE | RFC 8461, 8460, 7672 `[K]` | transport security | MAY |

## 3. Deliverability

Facts from secondary sources (vendor blogs summarising Google/Yahoo/Microsoft policy) `[S]`; **primary pages not fetched: UNCONFIRMED as to current thresholds**:
- Google and Yahoo began enforcing bulk-sender rules in Feb 2024; Microsoft for consumer domains (Outlook.com, Hotmail.com, Live.com) from 2025-05-05; enforcement has since moved from throttling to outright rejection with 5xx codes (Gmail 5.7.x, Outlook `550 5.7.515`).
- "Bulk" is generally reported as ~5,000 messages/day to the provider. SPF **and** DKIM required; DMARC required (Microsoft: sources disagree whether policy `p=none` suffices or alignment is the rule); one-click unsubscribe required for bulk marketing mail; spam-complaint rate threshold 0.3% (target < 0.1%).
- **Our volume is a handful of individually written, human-approved messages, which is far below any bulk threshold.** The rules still matter indirectly: a mailbox on a domain with no SPF/DKIM/DMARC is treated with suspicion by recruiters' filters, and an unauthenticated message to a company mailbox is likely to land in spam. So: **send from the user's own mailbox through that provider's authenticated submission path (Gmail/Outlook), or from a domain the user controls with SPF+DKIM+DMARC aligned.** Do not send from a generic relay with an unaligned From.
- No `List-Unsubscribe` on application emails (it is a bulk-mail signal and would look odd); but **include a one-line opt-out courtesy** only in follow-ups to unknown recruiters if a regulation requires it ([05](05-identity-trust-privacy-and-legal.md)).
- Verification tool for the user's own domain: `dig TXT` for SPF/DMARC, DKIM selector lookup, plus a mail-tester round trip in [08](08-observability-and-conformance-testing.md).

## 4. Message construction

- **Parsing `mailto:` from postings:** RFC 6068; percent-decode; accept `subject`, `cc`, `body`; **reject** unknown headers (header-injection defence) and any CR/LF in values. Prefer the posting's requested subject verbatim when it states one (e.g. "Subject: Go Engineer - Ref 4521"); some employers filter on it.
- **Subject convention (our default when none is given):** `Application: <Job title> [<Reference>] - <Full name>` - short, no clickbait, no ALL CAPS; reference ids from `referencenumber`/requisition fields ([01](01-job-posting-data-standards.md)).
- **Body:** plain-text part first, optional HTML alternative; UTF-8 (`Content-Type: text/plain; charset=utf-8`, `Content-Transfer-Encoding: quoted-printable` or base64); 70-78 char soft wrap per RFC 5322 line limit 998 hard; language of the posting; signature with name, link, phone in E.164.
- **Attachments:** CV (+ cover letter if requested) as PDF/DOCX per [03](03-resume-cv-format-and-parsing.md); total message size well under 10 MB (a common receiving limit; **UNCONFIRMED** per provider - Gmail's documented total is 25 MB `[K]`); RFC 2231 `filename*=UTF-8''...` only if non-ASCII, otherwise ASCII names.
- **Headers we set:** `Message-ID` (own domain, random), `Date`, `User-Agent`/`X-Mailer` omitted (avoid tool fingerprints), `Reply-To` only if different from From. Never set `Disposition-Notification-To` (read receipts look intrusive and are often blocked).

## 5. Sending paths

| Path | State in 2026 | Notes |
|---|---|---|
| Gmail API `users.messages.send` | `gmail.send` is classed **sensitive** (verification, but not the restricted-scope security assessment); `gmail.readonly`/`modify`/`compose`/`mail.google.com` are **restricted** (verification + third-party assessment for apps whose data touches a server) `[S: nylas, unipile guides]`; Google is the authority: UNCONFIRMED | single-user private deployment: use an OAuth client in *testing/internal* mode or `gmail.send` only; avoid restricted scopes by never reading the mailbox through Gmail API |
| Microsoft Graph `POST /me/sendMail` (delegated `Mail.Send`) | Graph coverage not obtained in this session: UNCONFIRMED | preferred over SMTP AUTH for Microsoft 365 |
| Exchange Online SMTP AUTH with **Basic auth** | Microsoft revised the timeline: Basic auth for client submission to be **disabled by default starting December 2026**, final removal date to be announced in H2 2027; OAuth (modern auth) SMTP AUTH is **not** deprecated `[S: techcommunity.microsoft.com "Updated Exchange Online SMTP AUTH Basic Authentication deprecation timeline", MC786329]` | never use Basic; use OAuth2 XOAUTH2 or Graph |
| Generic SMTP (user's own provider) | submission port 587 + STARTTLS or 465 implicit TLS; credentials in the secrets store only | TLS required; reject plaintext |
| `mailto:` hand-off | open the user's mail client with prefilled message | zero credentials, zero deliverability risk; attachments cannot be set via `mailto:` (RFC 6068 has no attachment field) so this path only prefills subject/body |

Secrets: tokens/refresh tokens live in the encrypted store (`internal/crypto`), never in config, logs, or git (CONST-042). Token scopes least-privilege; PKCE for installed apps; rotate on revoke.

## 6. Reply tracking

- Best-effort and **user-consented**. Options: (a) user forwards/labels replies to a dedicated mailbox read over IMAP (`INBOX` + search by `In-Reply-To`/`References` matching our sent `Message-ID`); (b) paste-in; (c) manual status. IMAP XOAUTH2 requires a restricted Gmail scope (`mail.google.com`) per `[S]` - avoid; prefer (a) with a plain mailbox the user controls or a Gmail filter forwarding to it.
- Match replies by `References`/`In-Reply-To` first, then subject token `[Ref]`, then sender domain = employer domain. Classify (acknowledgement / interview request / rejection / auto-reply) with deterministic rules before any LLM; auto-replies (`Auto-Submitted: auto-replied`, RFC 3834 `[K]`) must not change application state to "responded".
- Tracker update uses the existing `internal/tracker`; all inbound content is untrusted text: strip HTML, never render remote images (tracking pixels), do not auto-follow links.

## 7. Encryption and signing

- S/MIME (RFC 8551) and OpenPGP (RFC 9580) are rarely used by recruiters; signing adds an attachment (`smime.p7s`) some filters distrust. **Do not sign or encrypt by default.** Offer opt-in encryption only when the employer publishes a key (e.g. `Autocrypt`/WKD discovery, UNCONFIRMED adoption). Priority MAY.

## 8. Unsolicited-mail rules

An application to a *posted* address is invited correspondence. **Cold outreach to people who did not post a role** is different: GDPR/ePrivacy (EU), PECR (UK), CAN-SPAM (US) apply differently (B2B vs B2C, consent, identification, opt-out). **Policy: job_seeker sends only (i) applications to channels named in a posting, and (ii) replies in an existing thread. No bulk or sequence outreach, no scraped addresses.** Detail and citations in [05](05-identity-trust-privacy-and-legal.md).

## 9. Integration for job_seeker

Packages: `internal/mailx` (message building), `internal/mailx/send` (transports), `internal/mailx/inbox` (reply matching).

```go
// internal/mailx
type Outgoing struct {
    From, To, Cc []mail.Address
    Subject      string
    TextBody     string
    HTMLBody     string // optional
    Attachments  []Attachment // from cvrender artefacts
    InReplyTo    string
    ApplicationID string // internal, never put in headers
}
func Build(o Outgoing, now func() time.Time) ([]byte, string /*Message-ID*/, error) // RFC 5322/MIME; rejects CR/LF in any header value
func ParseMailto(uri string) (To []string, Subject, Body string, err error)        // RFC 6068, header allow-list

// internal/mailx/send
type Sender interface {
    Name() string
    Send(ctx context.Context, raw []byte, from string, to []string) (providerMsgID string, err error)
}
// impls: GmailAPI (oauth2, gmail.send), GraphMail (Mail.Send), SMTPSubmit (587/465, XOAUTH2|PLAIN over TLS only), MailtoHandoff (no-op returning URL)
```

Hard rules coded as tests: `Send` is only reachable from the FSM transition `approved -> sending` (compile-time via an unexported token type); the transition requires a human approval row; a gate `gates/nosend` greps for `Send(` calls outside `internal/app` wiring. Header injection fuzz test; MIME round-trip with `net/mail` and `mime/multipart` from the standard library (no new dependency needed for Build); golden messages parsed by a second implementation as oracle.

Live evidence plan: send to an operator-owned test mailbox on each transport, capture the received raw source, verify `Authentication-Results` shows `spf=pass dkim=pass dmarc=pass` (this is the positive sink-side evidence; metadata alone is not a pass).

**Risks:** account suspension from automation patterns (one message per approval, jitter, daily cap in `internal/limiter`); wrong recipient (confirm address came from the posting, show it in the approval screen); leaking private data in attachments metadata; provider policy drift.

## 10. Priority table

| Item | Priority |
|---|---|
| RFC 5322/MIME builder, `mailto` parser, header-injection defence | MUST |
| Human-approval-gated sender; Gmail API `gmail.send` or Graph or SMTP over TLS with OAuth | MUST (at least one transport) |
| SPF/DKIM/DMARC alignment check for the sending domain (pre-flight) | MUST |
| Reply matching via forwarded mailbox / IMAP | SHOULD |
| `mailto` hand-off transport | SHOULD |
| S/MIME / PGP, MTA-STS, ARC | MAY |
| Bulk/sequence outreach, List-Unsubscribe machinery | WON'T |
