// Package identity manages device keys and the pinned TLS transport.
package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/wire"
)

type Profile struct{ Address, Pin string }

func Default(part string) string {
	d, e := os.UserConfigDir()
	if e != nil {
		panic(e)
	}
	return filepath.Join(d, "clistream", part)
}
func Save(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".pending-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), path)
}
func Key(dir string) (tls.Certificate, error) {
	var zero tls.Certificate
	if e := os.MkdirAll(dir, 0700); e != nil {
		return zero, e
	}
	info, e := os.Lstat(dir)
	if e != nil {
		return zero, e
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return zero, errors.New("identity directory must be private (0700)")
	}
	path := filepath.Join(dir, "key.pem")
	b, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		pub, priv, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return zero, e
		}
		now := time.Now()
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(now.UnixNano()), NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature}
		der, e := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
		if e != nil {
			return zero, e
		}
		pk, e := x509.MarshalPKCS8PrivateKey(priv)
		if e != nil {
			return zero, e
		}
		b = append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk})...)
		f, e := os.CreateTemp(dir, ".key-")
		if e != nil {
			return zero, e
		}
		defer os.Remove(f.Name())
		_, e = f.Write(b)
		ce := f.Close()
		if e != nil {
			return zero, e
		}
		if ce != nil {
			return zero, ce
		}
		if e = os.Link(f.Name(), path); errors.Is(e, os.ErrExist) {
			return Key(dir)
		} else if e != nil {
			return zero, e
		}
	} else if e != nil {
		return zero, e
	}
	return tls.X509KeyPair(b, b)
}
func Pin(cert *x509.Certificate) string {
	h := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(h[:])
}
func Base(cert tls.Certificate) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, NextProtos: []string{wire.ALPN}, SessionTicketsDisabled: true}
}
func Dial(ctx context.Context, dir string, p Profile) (*quic.Conn, error) {
	key, e := Key(dir)
	if e != nil {
		return nil, e
	}
	cfg := Base(key)
	cfg.InsecureSkipVerify = true // Exact SPKI pin from the local pairing invitation replaces public CA verification.
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) != 1 || Pin(cs.PeerCertificates[0]) != p.Pin {
			return errors.New("server key mismatch")
		}
		return nil
	}
	conn, err := quic.DialAddr(ctx, p.Address, cfg, wire.Config())
	var timeout net.Error
	if ctx.Err() == nil && errors.As(err, &timeout) && timeout.Timeout() {
		// A fresh UDP socket can restore reachability after a transient path loss.
		// Only retry the handshake, before any enrollment or application request.
		return quic.DialAddr(ctx, p.Address, cfg, wire.Config())
	}
	return conn, err
}
func Load(dir string) (p Profile, e error) {
	b, e := os.ReadFile(filepath.Join(dir, "server.json"))
	if e == nil {
		e = json.Unmarshal(b, &p)
	}
	return
}
func Pair(ctx context.Context, dir, link, role string) error {
	invite, e := ParseInvitation(link)
	if e != nil {
		return e
	}
	if invite.Role != "" && invite.Role != role {
		return errors.New("invitation is for a different device role")
	}
	p := invite.Profile
	token := invite.ID
	if strings.HasPrefix(link, InvitePrefix) {
		token = link
	}
	c, e := Dial(ctx, dir, p)
	if e != nil {
		return e
	}
	defer c.CloseWithError(0, "")
	s, _, e := wire.Open(c, wire.Message{Op: "pair", Token: token})
	if e != nil {
		return e
	}
	s.Close()
	return Save(filepath.Join(dir, "server.json"), p)
}
