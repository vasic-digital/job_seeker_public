// Command jsgate-serve is the real server stack (internal/server TCP+QUIC pair, server.Brotli,
// internal/api router with the FR-039 security middleware) wired minimally so the live HTTP/3 + Brotli gate
// (JSK-T032) has a real binary to run in a container. It exists because cmd/jobseekerd does not wire the
// server yet; once it does, the gate switches to jobseekerd and this command is removed. It serves
// /healthz and /readyz only, loads a certificate from files, and never generates one (JSK-T030).
//
// -mutate exists ONLY for the gate's paired mutation proof (section 1.1): it makes the gate's own claims
// false (no Brotli; the missing-QUIC case is proven with the TCP-only stack) so the probe can be shown to FAIL against a broken server.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vasic-digital/job_seeker/internal/api"
	"github.com/vasic-digital/job_seeker/internal/buildinfo"
	"github.com/vasic-digital/job_seeker/internal/server"
)

func main() {
	addr := flag.String("addr", ":8443", "host:port (one number for TCP and UDP)")
	cert := flag.String("cert", "", "certificate chain PEM file")
	key := flag.String("key", "", "private key PEM file")
	hosts := flag.String("hosts", "localhost,127.0.0.1", "comma-separated allowed Host names")
	mutate := flag.String("mutate", "none", "none | no-brotli (paired-mutation proof only)")
	flag.Parse()

	pair, err := tls.LoadX509KeyPair(*cert, *key)
	if err != nil {
		fmt.Fprintln(os.Stderr, "jsgate-serve: certificate load failed")
		os.Exit(1)
	}
	var h http.Handler = api.New(api.Deps{Build: buildinfo.Get(), AllowedHosts: strings.Split(*hosts, ",")})
	if *mutate != "no-brotli" {
		h = server.Brotli(h)
	}
	srv, err := server.New(server.Config{Addr: *addr, TLS: &tls.Config{Certificates: []tls.Certificate{pair}},
		Handler: h, RequireH3: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "jsgate-serve:", err)
		os.Exit(1)
	}
	if err := srv.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "jsgate-serve:", err)
		os.Exit(1)
	}
	tcp, udp := srv.Addrs()
	fmt.Printf("jsgate-serve: listening tcp=%s udp=%s h3=%v mutate=%s\n", tcp, udp, srv.H3Enabled(), *mutate)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	sc, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sc)
}
