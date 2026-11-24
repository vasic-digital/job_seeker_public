package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/testauthn"
)

const (
	origin = "https://js.lan:8443"
	rpid   = "js.lan"
	boot   = "bootstrap-token-0123456789abcdef"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) adv(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func setup(t *testing.T, token string) (*Service, *store.DB, *clock) {
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
	c := &clock{t: time.Now().UTC()}
	s, err := New(db, Config{RPID: rpid, RPName: "job_seeker", Origins: []string{origin}, SessionKey: []byte("0123456789abcdef0123456789abcdef"), BootstrapToken: token, Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	return s, db, c
}

func jreq(body []byte) *http.Request {
	r := httptest.NewRequest("POST", "/x", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func enc(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func register(t *testing.T, s *Service, a *testauthn.Authenticator, token string) error {
	t.Helper()
	cr, id, err := s.BeginBootstrap(context.Background(), token)
	if err != nil {
		return err
	}
	body, err := a.Register(enc(t, cr))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.FinishRegistration(context.Background(), id, jreq(body), "laptop")
	return err
}

func login(t *testing.T, s *Service, a *testauthn.Authenticator, old string) (*Session, error) {
	t.Helper()
	as, id, err := s.BeginLogin(context.Background())
	if err != nil {
		return nil, err
	}
	body, err := a.Assert(enc(t, as))
	if err != nil {
		t.Fatal(err)
	}
	return s.FinishLogin(context.Background(), id, jreq(body), old)
}

func TestBootstrapRequiresTokenAndZeroPasskeys(t *testing.T) {
	s, _, _ := setup(t, boot)
	a := testauthn.New(origin, rpid)
	if err := register(t, s, a, "wrong-token-wrong-token-wrong"); !errors.Is(err, ErrBootstrapToken) {
		t.Fatalf("wrong token: %v", err)
	}
	if err := register(t, s, a, ""); !errors.Is(err, ErrBootstrapToken) {
		t.Fatalf("empty token: %v", err)
	}
	if n, _ := s.PasskeyCount(context.Background()); n != 0 {
		t.Fatal("a refused bootstrap stored a passkey")
	}
	if err := register(t, s, a, boot); err != nil {
		t.Fatalf("valid bootstrap: %v", err)
	}
	b := testauthn.New(origin, rpid)
	if err := register(t, s, b, boot); !errors.Is(err, ErrBootstrapClosed) {
		t.Fatalf("token must be dead once a passkey exists: %v", err)
	}
	// JSK-T107 mutation pair: the BEGIN step must itself refuse (no ceremony minted), not merely the finish step.
	if cr, id, err := s.BeginBootstrap(context.Background(), boot); !errors.Is(err, ErrBootstrapClosed) || cr != nil || id != "" {
		t.Fatalf("begin after bootstrap: cr=%v id=%q err=%v", cr != nil, id, err)
	}
	if n, _ := s.PasskeyCount(context.Background()); n != 1 {
		t.Fatalf("passkeys=%d want 1", n)
	}
}

func TestBootstrapDisabledWithoutToken(t *testing.T) {
	s, _, _ := setup(t, "")
	if err := register(t, s, testauthn.New(origin, rpid), ""); !errors.Is(err, ErrBootstrapDisabled) {
		t.Fatalf("no configured token must disable registration: %v", err)
	}
}

func TestRacingBootstrapsYieldExactlyOnePasskey(t *testing.T) {
	s, _, _ := setup(t, boot)
	ctx := context.Background()
	type pend struct {
		a    *testauthn.Authenticator
		body []byte
		id   string
	}
	var ps []pend
	for i := 0; i < 6; i++ { // all six begin while zero passkeys exist
		a := testauthn.New(origin, rpid)
		cr, id, err := s.BeginBootstrap(ctx, boot)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := a.Register(enc(t, cr))
		ps = append(ps, pend{a, body, id})
	}
	var wg sync.WaitGroup
	var ok, closed int32
	var mu sync.Mutex
	for _, p := range ps {
		wg.Add(1)
		go func(p pend) {
			defer wg.Done()
			_, err := s.FinishRegistration(ctx, p.id, jreq(p.body), "x")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrBootstrapClosed):
				closed++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}(p)
	}
	wg.Wait()
	if n, _ := s.PasskeyCount(ctx); n != 1 || ok != 1 || closed != 5 {
		t.Fatalf("passkeys=%d ok=%d closed=%d", n, ok, closed)
	}
}

func TestLoginSessionLifetimesRotationAndStepUp(t *testing.T) {
	s, db, c := setup(t, boot)
	ctx := context.Background()
	a := testauthn.New(origin, rpid)
	if err := register(t, s, a, boot); err != nil {
		t.Fatal(err)
	}
	s1, err := login(t, s, a, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(s1.ID) != 43 {
		t.Fatalf("session id %d chars, want 43 (256 bits)", len(s1.ID))
	}
	var storedID string
	_ = db.Read().QueryRow(`SELECT id FROM session`).Scan(&storedID)
	if storedID == s1.ID || len(storedID) != 64 {
		t.Fatal("the cookie value must not be what is stored (hash only)")
	}
	if !s1.StepUpFresh(c.now()) {
		t.Fatal("a fresh login performed a user-verified assertion: step-up is fresh")
	}
	c.adv(StepUpTTL + time.Second)
	got, err := s.Lookup(ctx, s1.ID)
	if err != nil {
		t.Fatalf("lookup inside idle window: %v", err)
	}
	if got.StepUpFresh(c.now()) {
		t.Fatal("step-up must lapse after 5 minutes")
	}
	// rotation: logging in again with the old cookie destroys it
	s2, err := login(t, s, a, s1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s2.ID == s1.ID || s2.CSRF == s1.CSRF {
		t.Fatal("rotation must mint a new id and a new CSRF token")
	}
	if _, err := s.Lookup(ctx, s1.ID); !errors.Is(err, ErrNoSession) {
		t.Fatalf("old session survived rotation: %v", err)
	}
	// idle: 29 minutes of inactivity is fine, 31 is not
	c.adv(29 * time.Minute)
	if _, err := s.Lookup(ctx, s2.ID); err != nil {
		t.Fatalf("29m idle: %v", err)
	}
	c.adv(31 * time.Minute)
	if _, err := s.Lookup(ctx, s2.ID); !errors.Is(err, ErrNoSession) {
		t.Fatalf("31m idle must expire: %v", err)
	}
	// absolute: keep it busy every 20 minutes; it still dies at 12 hours
	s3, err := login(t, s, a, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 35; i++ { // 35 * 20m = 11h40m
		c.adv(20 * time.Minute)
		if _, err := s.Lookup(ctx, s3.ID); err != nil {
			t.Fatalf("active session died early at step %d: %v", i, err)
		}
	}
	c.adv(21 * time.Minute) // 12h01m since login
	if _, err := s.Lookup(ctx, s3.ID); !errors.Is(err, ErrNoSession) {
		t.Fatalf("absolute 12h limit not enforced: %v", err)
	}
	var n int
	_ = db.Read().QueryRow(`SELECT COUNT(*) FROM session WHERE id=?`, sessionKey(s3.ID)).Scan(&n)
	if n != 0 {
		t.Fatal("expired session row not deleted")
	}
}

func TestSessionIDGarbageIsRejectedWithoutQuery(t *testing.T) {
	s, _, _ := setup(t, boot)
	for _, id := range []string{"", "short", "x' OR '1'='1", string(bytes.Repeat([]byte("A"), 44)), string(bytes.Repeat([]byte("A"), 43))} {
		if _, err := s.Lookup(context.Background(), id); !errors.Is(err, ErrNoSession) {
			t.Errorf("%q: %v", id, err)
		}
	}
}

func TestStepUpIsBoundToTheCallersSession(t *testing.T) {
	s, _, c := setup(t, boot)
	ctx := context.Background()
	a := testauthn.New(origin, rpid)
	if err := register(t, s, a, boot); err != nil {
		t.Fatal(err)
	}
	mine, _ := login(t, s, a, "")
	other, _ := login(t, s, a, "")
	c.adv(StepUpTTL + time.Minute)
	as, id, err := s.BeginStepUp(ctx, mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := a.Assert(enc(t, as))
	if err := s.FinishStepUp(ctx, id, jreq(body), other.ID); !errors.Is(err, ErrCeremony) {
		t.Fatalf("ceremony begun for one session finished by another: %v", err)
	}
	as, id, _ = s.BeginStepUp(ctx, mine.ID)
	body, _ = a.Assert(enc(t, as))
	if err := s.FinishStepUp(ctx, id, jreq(body), mine.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Lookup(ctx, mine.ID)
	if !got.StepUpFresh(c.now()) {
		t.Fatal("step-up not recorded")
	}
	o, _ := s.Lookup(ctx, other.ID)
	if o.StepUpFresh(c.now()) {
		t.Fatal("step-up leaked to another session")
	}
}

func TestAssertionRefusals(t *testing.T) {
	ctx := context.Background()
	t.Run("counter replay is a clone warning", func(t *testing.T) {
		s, _, _ := setup(t, boot)
		a := testauthn.New(origin, rpid)
		if err := register(t, s, a, boot); err != nil {
			t.Fatal(err)
		}
		if _, err := login(t, s, a, ""); err != nil { // counter 1
			t.Fatal(err)
		}
		if _, err := login(t, s, a, ""); err != nil { // counter 2
			t.Fatal(err)
		}
		one := uint32(1)
		a.FixedCount = &one // a cloned authenticator replays an old counter
		if _, err := login(t, s, a, ""); !errors.Is(err, ErrCloneWarning) {
			t.Fatalf("replayed counter accepted: %v", err)
		}
		two := uint32(2)
		a.FixedCount = &two // equal is also refused
		if _, err := login(t, s, a, ""); !errors.Is(err, ErrCloneWarning) {
			t.Fatalf("equal counter accepted: %v", err)
		}
	})
	t.Run("zero counters are tolerated", func(t *testing.T) {
		s, _, _ := setup(t, boot)
		a := testauthn.New(origin, rpid)
		if err := register(t, s, a, boot); err != nil {
			t.Fatal(err)
		}
		z := uint32(0)
		a.FixedCount = &z // many platform authenticators never count
		for i := 0; i < 2; i++ {
			if _, err := login(t, s, a, ""); err != nil {
				t.Fatalf("zero-counter authenticator refused: %v", err)
			}
		}
	})
	t.Run("wrong origin", func(t *testing.T) {
		s, _, _ := setup(t, boot)
		a := testauthn.New(origin, rpid)
		if err := register(t, s, a, boot); err != nil {
			t.Fatal(err)
		}
		a.Origin = "https://evil.example"
		if _, err := login(t, s, a, ""); !errors.Is(err, ErrAssertion) {
			t.Fatalf("assertion from another origin accepted: %v", err)
		}
	})
	t.Run("wrong rp id", func(t *testing.T) {
		s, _, _ := setup(t, boot)
		a := testauthn.New(origin, rpid)
		if err := register(t, s, a, boot); err != nil {
			t.Fatal(err)
		}
		a.RPID = "evil.example"
		if _, err := login(t, s, a, ""); !errors.Is(err, ErrAssertion) {
			t.Fatalf("assertion for another RP ID accepted: %v", err)
		}
	})
	t.Run("user verification absent", func(t *testing.T) {
		s, _, _ := setup(t, boot)
		a := testauthn.New(origin, rpid)
		if err := register(t, s, a, boot); err != nil {
			t.Fatal(err)
		}
		a.NoUV = true
		if _, err := login(t, s, a, ""); !errors.Is(err, ErrAssertion) {
			t.Fatalf("assertion without user verification accepted: %v", err)
		}
	})
	t.Run("registration without user verification", func(t *testing.T) {
		s, _, _ := setup(t, boot)
		a := testauthn.New(origin, rpid)
		a.NoUV = true
		if err := register(t, s, a, boot); !errors.Is(err, ErrAttestation) {
			t.Fatalf("UV-less registration accepted: %v", err)
		}
	})
	t.Run("ceremony is single use and kind-bound", func(t *testing.T) {
		s, _, _ := setup(t, boot)
		a := testauthn.New(origin, rpid)
		if err := register(t, s, a, boot); err != nil {
			t.Fatal(err)
		}
		as, id, _ := s.BeginLogin(ctx)
		body, _ := a.Assert(enc(t, as))
		if _, err := s.FinishLogin(ctx, id, jreq(body), ""); err != nil {
			t.Fatal(err)
		}
		if _, err := s.FinishLogin(ctx, id, jreq(body), ""); !errors.Is(err, ErrCeremony) {
			t.Fatalf("replayed finish: %v", err)
		}
		if _, err := s.FinishLogin(ctx, "nope", jreq(body), ""); !errors.Is(err, ErrCeremony) {
			t.Fatalf("unknown ceremony: %v", err)
		}
		_, sid, _ := s.BeginStepUp(ctx, "irrelevant")
		if _, err := s.FinishLogin(ctx, sid, jreq(body), ""); !errors.Is(err, ErrCeremony) {
			t.Fatalf("a step-up ceremony must not finish as a login: %v", err)
		}
	})
	t.Run("ceremony expires", func(t *testing.T) {
		s, _, c := setup(t, boot)
		a := testauthn.New(origin, rpid)
		if err := register(t, s, a, boot); err != nil {
			t.Fatal(err)
		}
		as, id, _ := s.BeginLogin(ctx)
		body, _ := a.Assert(enc(t, as))
		c.adv(ceremonyTTL + time.Second)
		if _, err := s.FinishLogin(ctx, id, jreq(body), ""); !errors.Is(err, ErrCeremony) {
			t.Fatalf("expired ceremony accepted: %v", err)
		}
	})
	t.Run("no passkeys, no login", func(t *testing.T) {
		s, _, _ := setup(t, boot)
		if _, _, err := s.BeginLogin(ctx); !errors.Is(err, ErrNoPasskeys) {
			t.Fatalf("%v", err)
		}
	})
}

func TestPendingCeremoniesAreBoundedAndNeverEvictLiveOnes(t *testing.T) {
	s, _, _ := setup(t, boot)
	a := testauthn.New(origin, rpid)
	if err := register(t, s, a, boot); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	as, id, _ := s.BeginLogin(ctx) // the victim's ceremony
	body, _ := a.Assert(enc(t, as))
	var busy int
	for i := 0; i < maxCeremony+50; i++ {
		if _, _, err := s.BeginLogin(ctx); errors.Is(err, ErrBusy) {
			busy++
		}
	}
	if busy == 0 {
		t.Fatal("pending ceremonies are unbounded")
	}
	if _, err := s.FinishLogin(ctx, id, jreq(body), ""); err != nil {
		t.Fatalf("flooding evicted a live ceremony: %v", err)
	}
}

func TestAddSecondPasskeyAndLimit(t *testing.T) {
	s, db, _ := setup(t, boot)
	ctx := context.Background()
	a := testauthn.New(origin, rpid)
	if err := register(t, s, a, boot); err != nil {
		t.Fatal(err)
	}
	b := testauthn.New(origin, rpid)
	cr, id, err := s.BeginAdd(ctx)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := b.Register(enc(t, cr))
	if _, err := s.FinishRegistration(ctx, id, jreq(body), "phone"); err != nil {
		t.Fatal(err)
	}
	if _, err := login(t, s, b, ""); err != nil {
		t.Fatalf("second authenticator cannot log in: %v", err)
	}
	var n int
	_ = db.Read().QueryRow(`SELECT COUNT(*) FROM audit_log WHERE reason_code='passkey_registered'`).Scan(&n)
	if n != 2 {
		t.Fatalf("audit rows for registrations: %d", n)
	}
	_ = db.Read().QueryRow(`SELECT COUNT(*) FROM audit_log WHERE reason_code='login_ok'`).Scan(&n)
	if n != 1 {
		t.Fatalf("audit rows for logins: %d", n)
	}
}

func TestPendingCeremoniesHavePerClientCeiling(t *testing.T) {
	s, _, _ := setup(t, boot)
	a := testauthn.New(origin, rpid)
	if err := register(t, s, a, boot); err != nil {
		t.Fatal(err)
	}
	flood := WithClient(context.Background(), "203.0.113.9")
	var ok, busy int
	for i := 0; i < maxCeremonyPerClient+10; i++ {
		if _, _, err := s.BeginLogin(flood); errors.Is(err, ErrBusy) {
			busy++
		} else if err == nil {
			ok++
		}
	}
	if ok != maxCeremonyPerClient || busy != 10 {
		t.Fatalf("one client got %d ceremonies, %d refused", ok, busy)
	}
	// another client is unaffected by the flood
	if _, _, err := s.BeginLogin(WithClient(context.Background(), "198.51.100.7")); err != nil {
		t.Fatalf("a different client was starved: %v", err)
	}
}
