package smtpcompose

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// MaxMessageBytes bounds the built message (receiving servers commonly refuse more than ~10 MiB).
const MaxMessageBytes = 10 << 20

// Attachment is one file the human's draft carries (the approved CV and cover letter).
type Attachment struct {
	Name, MIME string
	Data       []byte
}

// Message is the data of one draft. Everything that reaches a header is validated by Build.
type Message struct {
	From, To, Subject, Body string
	MessageID               string // "<local@domain>", set by the caller so the draft is deterministic
	Date                    time.Time
	Attachments             []Attachment
}

var (
	errMsg   = errors.New("smtpcompose: message refused")
	reMIME   = regexp.MustCompile(`^[a-z]+/[a-z0-9][a-z0-9.+-]*$`)
	reMsgID  = regexp.MustCompile(`^<[A-Za-z0-9._+-]{1,64}@[A-Za-z0-9.-]{1,255}>$`)
	reAttach = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,99}$`)
)

func bare(s string) bool { return !strings.ContainsAny(s, "\r\n\x00") }

func oneAddr(s string) bool {
	if s == "" || !bare(s) || strings.ContainsAny(s, ",;<> ") {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Name == "" && a.Address == s
}

// Build renders the RFC 5322 message. It refuses CR/LF/NUL in any header value, multiple recipients, display
// names, path-like attachment names and oversize messages. It sets no X-Mailer and no read-receipt header.
func Build(m Message) ([]byte, error) {
	if !oneAddr(m.To) || !oneAddr(m.From) || !bare(m.Subject) || len(m.Subject) > 200 || !reMsgID.MatchString(m.MessageID) || m.Date.IsZero() {
		return nil, errMsg
	}
	total := len(m.Body)
	for _, a := range m.Attachments {
		if !reAttach.MatchString(a.Name) || strings.Contains(a.Name, "..") || !reMIME.MatchString(a.MIME) {
			return nil, errMsg
		}
		total += len(a.Data)
	}
	if total >= MaxMessageBytes {
		return nil, errMsg
	}
	h := sha256.New()
	h.Write([]byte(m.MessageID))
	boundary := "jsk-" + hex.EncodeToString(h.Sum(nil))[:24]
	var b bytes.Buffer
	w := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	w("From", m.From)
	w("To", m.To)
	w("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	w("Date", m.Date.UTC().Format(time.RFC1123Z))
	w("Message-ID", m.MessageID)
	w("MIME-Version", "1.0")
	w("Content-Type", `multipart/mixed; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary)
	qw := quotedprintable.NewWriter(&b)
	_, _ = qw.Write([]byte(m.Body))
	_ = qw.Close()
	b.WriteString("\r\n")
	for _, a := range m.Attachments {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: %s\r\n\r\n", boundary, a.MIME,
			mime.FormatMediaType("attachment", map[string]string{"filename": a.Name}))
		enc := base64.StdEncoding.EncodeToString(a.Data)
		for len(enc) > 76 {
			b.WriteString(enc[:76] + "\r\n")
			enc = enc[76:]
		}
		b.WriteString(enc + "\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	if b.Len() >= MaxMessageBytes {
		return nil, errMsg
	}
	return b.Bytes(), nil
}
