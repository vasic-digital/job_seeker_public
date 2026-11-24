package app

import "crypto/tls"

func tlsConfig(c tls.Certificate) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{c}, MinVersion: tls.VersionTLS12}
}
