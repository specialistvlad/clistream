package identity

import (
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"time"
)

// Authenticator configures handshake requirements and identifies a proven peer.
// Authorization and invitation issuance belong to the broker.
type Authenticator interface {
	Configure(*tls.Config)
	Principal(tls.ConnectionState) (string, error)
}

type DeviceKeys struct{}

func (d DeviceKeys) Configure(cfg *tls.Config) {
	cfg.ClientAuth = tls.RequireAnyClientCert
	cfg.VerifyConnection = func(cs tls.ConnectionState) error { _, e := d.Principal(cs); return e }
}
func (DeviceKeys) Principal(cs tls.ConnectionState) (string, error) {
	if len(cs.PeerCertificates) != 1 {
		return "", errors.New("expected one device certificate")
	}
	cert := cs.PeerCertificates[0]
	now := time.Now()
	if _, ok := cert.PublicKey.(ed25519.PublicKey); !ok || now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return "", errors.New("invalid device key or validity period")
	}
	return Pin(cert), nil
}
