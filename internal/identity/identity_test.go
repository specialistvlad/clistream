package identity

import (
	"context"
	"crypto/tls"
	"path/filepath"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/wire"
)

func TestServerRequiresCertificateAndCompressionALPN(t *testing.T) {
	key, e := Key(filepath.Join(t.TempDir(), "device"))
	if e != nil {
		t.Fatal(e)
	}
	config := Base(key)
	DeviceKeys{}.Configure(config)
	listener, e := quic.ListenAddr("127.0.0.1:0", config, wire.Config())
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	for _, test := range []struct {
		name, alpn string
		withCert   bool
	}{
		{"no private key", wire.ALPN, false},
		{"no compression", "clistream/1", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cfg := Base(key)
			cfg.NextProtos = []string{test.alpn}
			cfg.InsecureSkipVerify = true
			cfg.VerifyConnection = func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) != 1 || Pin(cs.PeerCertificates[0]) != Pin(key.Leaf) {
					t.Error("unexpected test server")
				}
				return nil
			}
			if !test.withCert {
				cfg.Certificates = nil
			}
			c, e := quic.DialAddr(ctx, listener.Addr().String(), cfg, wire.Config())
			if e == nil {
				defer c.CloseWithError(0, "test complete")
				select {
				case <-c.Context().Done():
				case <-ctx.Done():
					t.Fatal("server accepted an incompatible client")
				}
			}
		})
	}
}
