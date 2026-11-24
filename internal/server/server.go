// Package server runs the TCP TLS (h2, h1.1) and QUIC (h3) listener pair on ONE port number (FR-040).
// Alt-Svc is advertised only when both binds succeeded; shutdown is h3.Shutdown, h3.Close, then the
// TCP server; 0-RTT is disabled. Certificates come from the operator CA (see JSK-T030); nothing here
// generates a certificate.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// Config configures the pair.
type Config struct {
	Addr         string      // host:port; port 0 picks one port number free on both TCP and UDP
	TLS          *tls.Config // Certificates (or GetCertificate) required
	Handler      http.Handler
	AltSvcMaxAge int  // seconds, default 86400
	RequireH3    bool // production: fail Start if QUIC cannot bind
	Log          *slog.Logger
}

type closer interface {
	Shutdown(context.Context) error
	Close() error
}

type h3Handle struct {
	closer    closer
	allow0RTT bool
}

func (h *h3Handle) quicAllows0RTT() bool { return h.allow0RTT }

// Server is the listener pair.
type Server struct {
	cfg  Config
	mu   sync.Mutex
	tcp  *http.Server
	ln   net.Listener
	pc   net.PacketConn
	h3   *h3Handle
	done chan struct{}
}

// New validates the configuration.
func New(cfg Config) (*Server, error) {
	if cfg.TLS == nil || (len(cfg.TLS.Certificates) == 0 && cfg.TLS.GetCertificate == nil) {
		return nil, errors.New("server: a TLS certificate is required")
	}
	if cfg.Handler == nil {
		return nil, errors.New("server: handler required")
	}
	if cfg.AltSvcMaxAge == 0 {
		cfg.AltSvcMaxAge = 86400
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Server{cfg: cfg, done: make(chan struct{})}, nil
}

func bindPair(addr string) (net.Listener, net.PacketConn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, nil, err
	}
	attempts := 1
	if port == "0" || port == "" {
		attempts = 20 // an ephemeral TCP port may be taken on UDP: try again with another
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, nil, err
		}
		_, p, _ := net.SplitHostPort(ln.Addr().String())
		pc, err := net.ListenPacket("udp", net.JoinHostPort(host, p))
		if err == nil {
			return ln, pc, nil
		}
		lastErr = err
		if attempts == 1 {
			return ln, nil, lastErr // caller decides: TCP only, or fail closed
		}
		ln.Close()
	}
	return nil, nil, lastErr
}

// Start binds and serves; it returns once both sockets are bound. Serving continues in goroutines.
func (s *Server) Start() error {
	ln, pc, err := bindPair(s.cfg.Addr)
	if err != nil && ln == nil {
		return fmt.Errorf("server: bind: %w", err)
	}
	if pc == nil && s.cfg.RequireH3 {
		ln.Close()
		return fmt.Errorf("server: QUIC bind failed and HTTP/3 is required: %w", err)
	}
	s.ln, s.pc = ln, pc
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	handler := s.cfg.Handler
	if pc != nil {
		alt := fmt.Sprintf(`h3=":%s"; ma=%d`, port, s.cfg.AltSvcMaxAge)
		inner := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Alt-Svc", alt)
			inner.ServeHTTP(w, r)
		})
	} else {
		s.cfg.Log.Warn("QUIC bind failed; serving TCP only and NOT advertising Alt-Svc")
	}

	tcpTLS := s.cfg.TLS.Clone()
	tcpTLS.MinVersion = tls.VersionTLS12
	tcpTLS.NextProtos = []string{"h2", "http/1.1"}
	tcp := &http.Server{Handler: handler, TLSConfig: tcpTLS, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	s.tcp = tcp
	go func() { _ = tcp.ServeTLS(ln, "", ""); close(s.done) }()

	if pc != nil {
		h3tls := s.cfg.TLS.Clone()
		h3tls.MinVersion = tls.VersionTLS13
		qc := &quic.Config{Allow0RTT: false}
		srv := &http3.Server{Handler: handler, TLSConfig: http3.ConfigureTLSConfig(h3tls), QUICConfig: qc}
		s.h3 = &h3Handle{closer: srv, allow0RTT: qc.Allow0RTT}
		go func() { _ = srv.Serve(pc) }() // srv and pc are captured by value
	}
	return nil
}

// Addr is the bound TCP address.
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Addrs returns the bound TCP and UDP addresses (UDP is empty when QUIC is not serving).
func (s *Server) Addrs() (tcp, udp string) {
	if s.pc != nil {
		udp = s.pc.LocalAddr().String()
	}
	return s.ln.Addr().String(), udp
}

// H3Enabled reports whether QUIC is serving (and Alt-Svc advertised).
func (s *Server) H3Enabled() bool { return s.h3 != nil }

// Shutdown stops accepting, then h3.Shutdown, h3.Close, and finally the TCP server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	if s.h3 != nil {
		errs = append(errs, s.h3.closer.Shutdown(ctx), s.h3.closer.Close())
	}
	if s.pc != nil {
		_ = s.pc.Close()
	}
	if s.tcp != nil {
		errs = append(errs, s.tcp.Shutdown(ctx))
		// http.Server.Shutdown only closes listeners the serve goroutine has already registered. If
		// Shutdown wins the race against that goroutine's first scheduling, the TCP socket would stay
		// bound until the goroutine runs, so close it explicitly (a second Close is harmless) and wait
		// for the serve goroutine to finish.
		if s.ln != nil {
			_ = s.ln.Close()
		}
		select {
		case <-s.done:
		case <-ctx.Done():
			errs = append(errs, ctx.Err())
		}
	}
	return errors.Join(errs...)
}
