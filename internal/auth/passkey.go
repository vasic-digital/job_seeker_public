package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// Errors. Handlers map every one of them to a closed API code; none of them carries a value.
var (
	ErrBootstrapDisabled = errors.New("auth: no bootstrap token is configured")
	ErrBootstrapToken    = errors.New("auth: bootstrap token mismatch")
	ErrBootstrapClosed   = errors.New("auth: a passkey already exists; bootstrap registration is closed")
	ErrNoPasskeys        = errors.New("auth: no passkey is registered")
	ErrCeremony          = errors.New("auth: unknown, expired or mismatched ceremony")
	ErrBusy              = errors.New("auth: too many pending ceremonies")
	ErrCloneWarning      = errors.New("auth: signature counter did not advance; authenticator may be cloned")
	ErrAssertion         = errors.New("auth: assertion rejected")
	ErrAttestation       = errors.New("auth: registration rejected")
	ErrPasskeyLimit      = errors.New("auth: passkey limit reached")
)

const (
	ceremonyTTL = 5 * time.Minute
	maxCeremony = 256
	// maxCeremonyPerClient keeps one client from using the whole pending table: past it that client alone gets ErrBusy.
	maxCeremonyPerClient = 16
	maxPasskeys          = 8
	maxBody              = 64 << 10
)

// Config configures the Service.
type Config struct {
	RPID           string   // effective domain, no scheme or port
	RPName         string   // display name
	Origins        []string // exact permitted origins, e.g. https://js.lan:8443
	SessionKey     []byte   // derives the per-session CSRF token
	BootstrapToken string   // value of JOBSEEKER_BOOTSTRAP_TOKEN; empty disables first registration
	Now            func() time.Time
}

type kind int

const (
	kBootstrap kind = iota + 1
	kAdd
	kLogin
	kStepUp
)

type ceremony struct {
	kind    kind
	sd      webauthn.SessionData
	expires time.Time
	bound   string // session cookie value for step-up
	client  string // caller identity from WithClient ("" = unattributed, only the global cap applies)
}

type clientKey struct{}

// WithClient attributes ceremonies started under ctx to one caller (the HTTP layer passes the peer address).
func WithClient(ctx context.Context, client string) context.Context {
	return context.WithValue(ctx, clientKey{}, client)
}

// Service owns passkeys, sessions and WebAuthn ceremonies. It has no HTTP surface of its own.
type Service struct {
	cfg Config
	db  *store.DB
	wa  *webauthn.WebAuthn
	mu  sync.Mutex
	cer map[string]*ceremony
}

// New builds the Service. The library's own validation rejects an empty RP ID or origin list.
func New(db *store.DB, cfg Config) (*Service, error) {
	if len(cfg.SessionKey) < 16 {
		return nil, errors.New("auth: session key too short")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID: cfg.RPID, RPDisplayName: cfg.RPName, RPOrigins: cfg.Origins,
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey: protocol.ResidentKeyRequirementPreferred, UserVerification: protocol.VerificationRequired},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTTL, TimeoutUVD: ceremonyTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTTL, TimeoutUVD: ceremonyTTL}},
	})
	if err != nil {
		return nil, fmt.Errorf("auth: webauthn configuration: %w", err)
	}
	return &Service{cfg: cfg, db: db, wa: wa, cer: map[string]*ceremony{}}, nil
}

func (s *Service) now() time.Time { return s.cfg.Now() }

// Now is the service clock (injected in tests).
func (s *Service) Now() time.Time { return s.cfg.Now() }

// operator is the single account; it implements webauthn.User.
type operator struct {
	id    []byte
	creds []webauthn.Credential
}

func (o *operator) WebAuthnID() []byte                         { return o.id }
func (o *operator) WebAuthnName() string                       { return "operator" }
func (o *operator) WebAuthnDisplayName() string                { return "Operator" }
func (o *operator) WebAuthnCredentials() []webauthn.Credential { return o.creds }

type stored struct {
	T []protocol.AuthenticatorTransport `json:"t,omitempty"`
	F byte                              `json:"f"`
}

func (s *Service) userID(ctx context.Context) ([]byte, error) {
	var v string
	err := s.db.Read().QueryRowContext(ctx, `SELECT value FROM setting WHERE key='webauthn_user_id'`).Scan(&v)
	if err == nil {
		return hex.DecodeString(v)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO setting(key,value) VALUES('webauthn_user_id',?)`, hex.EncodeToString(b))
		return e
	})
	if err != nil {
		return nil, err
	}
	if err := s.db.Read().QueryRowContext(ctx, `SELECT value FROM setting WHERE key='webauthn_user_id'`).Scan(&v); err != nil {
		return nil, err
	}
	return hex.DecodeString(v)
}

func (s *Service) operator(ctx context.Context) (*operator, error) {
	id, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Read().QueryContext(ctx, `SELECT credential_id, public_key, sign_count, COALESCE(aaguid,''), COALESCE(transports,'') FROM passkey ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	o := &operator{id: id}
	for rows.Next() {
		var cid, pk []byte
		var sc int64
		var aaguid, meta string
		if err := rows.Scan(&cid, &pk, &sc, &aaguid, &meta); err != nil {
			return nil, err
		}
		var st stored
		_ = json.Unmarshal([]byte(meta), &st)
		ag, _ := hex.DecodeString(aaguid)
		o.creds = append(o.creds, webauthn.Credential{ID: cid, PublicKey: pk, Transport: st.T,
			Flags:         webauthn.NewCredentialFlags(protocol.AuthenticatorFlags(st.F)),
			Authenticator: webauthn.Authenticator{AAGUID: ag, SignCount: uint32(sc)}})
	}
	return o, rows.Err()
}

// PasskeyCount is the number of registered passkeys.
func (s *Service) PasskeyCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.Read().QueryRowContext(ctx, `SELECT COUNT(*) FROM passkey`).Scan(&n)
	return n, err
}

func (s *Service) putCeremony(ctx context.Context, c *ceremony) (string, error) {
	c.client, _ = ctx.Value(clientKey{}).(string)
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(b[:])
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.client != "" {
		mine := 0
		for k, v := range s.cer {
			if v.client != c.client {
				continue
			}
			if now.After(v.expires) {
				delete(s.cer, k)
				continue
			}
			mine++
		}
		if mine >= maxCeremonyPerClient {
			return "", ErrBusy
		}
	}
	if len(s.cer) >= maxCeremony {
		for k, v := range s.cer {
			if now.After(v.expires) {
				delete(s.cer, k)
			}
		}
		if len(s.cer) >= maxCeremony { // never evict a live ceremony: flooding must not break a real login
			return "", ErrBusy
		}
	}
	c.expires = now.Add(ceremonyTTL)
	s.cer[id] = c
	return id, nil
}

// takeCeremony removes and returns the ceremony: it is single-use whether the response verifies or not.
func (s *Service) takeCeremony(id string, want ...kind) (*ceremony, error) {
	s.mu.Lock()
	c := s.cer[id]
	delete(s.cer, id)
	s.mu.Unlock()
	if c == nil || s.now().After(c.expires) {
		return nil, ErrCeremony
	}
	for _, k := range want {
		if c.kind == k {
			return c, nil
		}
	}
	return nil, ErrCeremony
}

func bootstrapOK(want, got string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// BeginBootstrap starts the first registration. It requires the bootstrap token and zero passkeys.
func (s *Service) BeginBootstrap(ctx context.Context, token string) (*protocol.CredentialCreation, string, error) {
	n, err := s.PasskeyCount(ctx)
	if err != nil {
		return nil, "", err
	}
	if n > 0 {
		return nil, "", ErrBootstrapClosed
	}
	if s.cfg.BootstrapToken == "" {
		return nil, "", ErrBootstrapDisabled
	}
	if !bootstrapOK(s.cfg.BootstrapToken, token) {
		return nil, "", ErrBootstrapToken
	}
	return s.beginRegistration(ctx, kBootstrap)
}

// BeginAdd starts registration of a further authenticator. The caller MUST already have required a
// session and a fresh step-up.
func (s *Service) BeginAdd(ctx context.Context) (*protocol.CredentialCreation, string, error) {
	n, err := s.PasskeyCount(ctx)
	if err != nil {
		return nil, "", err
	}
	if n == 0 {
		return nil, "", ErrNoPasskeys
	}
	if n >= maxPasskeys {
		return nil, "", ErrPasskeyLimit
	}
	return s.beginRegistration(ctx, kAdd)
}

func (s *Service) beginRegistration(ctx context.Context, k kind) (*protocol.CredentialCreation, string, error) {
	o, err := s.operator(ctx)
	if err != nil {
		return nil, "", err
	}
	creation, sd, err := s.wa.BeginRegistration(o, webauthn.WithExclusions(webauthn.Credentials(o.creds).CredentialDescriptors()))
	if err != nil {
		return nil, "", err
	}
	id, err := s.putCeremony(ctx, &ceremony{kind: k, sd: *sd})
	return creation, id, err
}

// FinishRegistration verifies the attestation (none; user verification required; origin and RP ID checked)
// and stores the credential. For a bootstrap ceremony the insert is conditional on zero passkeys inside
// the write transaction, so two racing bootstraps cannot both succeed.
func (s *Service) FinishRegistration(ctx context.Context, cer string, r *http.Request, label string) (int64, error) {
	c, err := s.takeCeremony(cer, kBootstrap, kAdd)
	if err != nil {
		return 0, err
	}
	o, err := s.operator(ctx)
	if err != nil {
		return 0, err
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBody)
	cred, err := s.wa.FinishRegistration(o, c.sd, r)
	if err != nil {
		return 0, ErrAttestation
	}
	meta, _ := json.Marshal(stored{T: cred.Transport, F: byte(cred.Flags.ProtocolValue())})
	if len(label) > 64 {
		label = label[:64]
	}
	var id int64
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		cond := `1=1`
		if c.kind == kBootstrap {
			cond = `(SELECT COUNT(*) FROM passkey)=0`
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO passkey(credential_id,public_key,sign_count,aaguid,transports,label,created_at)
			SELECT ?,?,?,?,?,?,? WHERE `+cond, cred.ID, cred.PublicKey, int64(cred.Authenticator.SignCount),
			hex.EncodeToString(cred.Authenticator.AAGUID), string(meta), label, ts(s.now()))
		if e != nil {
			return e
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrBootstrapClosed
		}
		id, _ = res.LastInsertId()
		return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "passkey_registered", Evidence: map[string]any{"passkey_id": id}})
	})
	return id, err
}

// BeginLogin starts an assertion ceremony.
func (s *Service) BeginLogin(ctx context.Context) (*protocol.CredentialAssertion, string, error) {
	return s.beginAssertion(ctx, kLogin, "")
}

// BeginStepUp starts an assertion bound to an existing session.
func (s *Service) BeginStepUp(ctx context.Context, sessionCookie string) (*protocol.CredentialAssertion, string, error) {
	return s.beginAssertion(ctx, kStepUp, sessionCookie)
}

func (s *Service) beginAssertion(ctx context.Context, k kind, bound string) (*protocol.CredentialAssertion, string, error) {
	o, err := s.operator(ctx)
	if err != nil {
		return nil, "", err
	}
	if len(o.creds) == 0 {
		return nil, "", ErrNoPasskeys
	}
	as, sd, err := s.wa.BeginLogin(o, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}
	id, err := s.putCeremony(ctx, &ceremony{kind: k, sd: *sd, bound: bound})
	return as, id, err
}

// verifyAssertion validates the response and persists the advanced counter in tx. A counter that does
// not advance (when either side is non-zero) is refused: ErrCloneWarning.
func (s *Service) verifyAssertion(ctx context.Context, c *ceremony, r *http.Request) (credID int64, commit func(*sql.Tx) error, err error) {
	o, err := s.operator(ctx)
	if err != nil {
		return 0, nil, err
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBody)
	cred, err := s.wa.FinishLogin(o, c.sd, r)
	if err != nil {
		return 0, nil, ErrAssertion
	}
	if cred.Authenticator.CloneWarning {
		return 0, nil, ErrCloneWarning
	}
	newCount := int64(cred.Authenticator.SignCount)
	return 0, func(tx *sql.Tx) error {
		var res sql.Result
		var e error
		if newCount > 0 { // compare-and-swap: a replayed or racing assertion cannot reuse a count
			res, e = tx.ExecContext(ctx, `UPDATE passkey SET sign_count=?, last_used_at=? WHERE credential_id=? AND sign_count<?`, newCount, ts(s.now()), cred.ID, newCount)
		} else {
			res, e = tx.ExecContext(ctx, `UPDATE passkey SET last_used_at=? WHERE credential_id=? AND sign_count=0`, ts(s.now()), cred.ID)
		}
		if e != nil {
			return e
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrCloneWarning
		}
		return nil
	}, nil
}

// FinishLogin verifies the assertion and, in ONE transaction, advances the counter, creates a new
// session (any presented session is destroyed: rotation) and appends the audit row. A fresh login
// counts as step-up: the assertion just performed required user verification.
func (s *Service) FinishLogin(ctx context.Context, cer string, r *http.Request, oldCookie string) (*Session, error) {
	c, err := s.takeCeremony(cer, kLogin)
	if err != nil {
		return nil, err
	}
	_, commit, err := s.verifyAssertion(ctx, c, r)
	if err != nil {
		return nil, err
	}
	var sess *Session
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		if e := commit(tx); e != nil {
			return e
		}
		if reSessionID.MatchString(oldCookie) {
			if _, e := tx.ExecContext(ctx, `DELETE FROM session WHERE id=?`, sessionKey(oldCookie)); e != nil {
				return e
			}
		}
		var e error
		if sess, e = s.newSession(ctx, tx, true); e != nil {
			return e
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "login_ok"})
	})
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// FinishStepUp verifies an assertion bound to the caller's own session and stamps step_up_at.
func (s *Service) FinishStepUp(ctx context.Context, cer string, r *http.Request, sessionCookie string) error {
	c, err := s.takeCeremony(cer, kStepUp)
	if err != nil {
		return err
	}
	if c.bound == "" || subtle.ConstantTimeCompare([]byte(c.bound), []byte(sessionCookie)) != 1 {
		return ErrCeremony
	}
	_, commit, err := s.verifyAssertion(ctx, c, r)
	if err != nil {
		return err
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if e := commit(tx); e != nil {
			return e
		}
		return s.markStepUp(ctx, tx, sessionCookie)
	})
}
