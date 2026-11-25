package broker

import (
	"context"
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/wire"
)

// serve starts server on a loopback listener (at addr, or any free port for "") and returns a
// context for pairing; the cleanup stops it.
func serve(t *testing.T, server *Server, addr string) context.Context {
	t.Helper()
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	listener, err := quic.ListenAddr(addr, server.tlsConfig(t), wire.Config())
	if err != nil {
		t.Fatal(err)
	}
	server.addresses = []string{listener.Addr().String()}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.Serve(ctx, listener)
	}()
	t.Cleanup(func() {
		cancel()
		listener.Close()
		<-done
	})
	return ctx
}

// newServer is New on dir, with its TLS config kept for serve.
func newServer(t *testing.T, dir string) *Server {
	t.Helper()
	server, cfg, err := New(dir, nil, identity.DeviceKeys{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	configs[server] = cfg
	return server
}

var configs = map[*Server]*tls.Config{}

func (s *Server) tlsConfig(t *testing.T) *tls.Config {
	t.Helper()
	cfg, ok := configs[s]
	if !ok {
		t.Fatal("no TLS config for this server: use newServer")
	}
	return cfg
}

// A week-long link survives a restart of the daemon and pairs more than one device.
func TestLongInvitationOutlivesARestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "server")
	first := newServer(t, dir)
	// The link names the address the second daemon will serve on.
	probe, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.LocalAddr().String()
	probe.Close()
	first.addresses = []string{addr}
	link, err := first.Invite("", 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "invitations.json")); err != nil {
		t.Fatal("a long-lived link is kept on disk:", err)
	}
	// A new daemon on the same directory: the link stays valid, for two devices in turn.
	second := newServer(t, dir)
	ctx := serve(t, second, addr)
	for _, name := range []string{"phone", "laptop"} {
		if err := identity.Pair(ctx, filepath.Join(t.TempDir(), name), link, ""); err != nil {
			t.Fatalf("%s pairs from the kept link after a restart: %v", name, err)
		}
	}
	listed, err := second.Admin("invitations", "")
	if err != nil || !strings.Contains(listed, "T") || len(strings.Fields(listed)) != 2 {
		t.Fatalf("invitations lists the kept link with its expiry: %q %v", listed, err)
	}
}

// The default link is not kept; a lifetime past a week is refused.
func TestInvitationLifetimes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "server")
	server := newServer(t, dir)
	server.addresses = []string{"127.0.0.1:1"}
	if _, err := server.Invite("", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "invitations.json")); err == nil {
		t.Fatal("a default link is not kept on disk")
	}
	if _, err := server.Invite("", MaxInviteLifetime+time.Second); err == nil {
		t.Fatal("a lifetime past MaxInviteLifetime is refused")
	}
	if _, err := server.Invite("", -time.Minute); err == nil {
		t.Fatal("a negative lifetime is refused")
	}
	if listed, _ := server.Admin("invitations", ""); listed != "" {
		t.Fatalf("no kept link yet: %q", listed)
	}
}

// The display replaces its own links and spares a kept one; uninvite cancels the kept ones.
func TestDisplaySparesKeptLinksAndUninviteCancels(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "server")
	server := newServer(t, dir)
	ctx := serve(t, server, "")
	short, err := server.Invite("", 0)
	if err != nil {
		t.Fatal(err)
	}
	long, err := server.Invite("", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.Admin("display", ""); err != nil {
		t.Fatal(err)
	}
	if err := identity.Pair(ctx, filepath.Join(t.TempDir(), "a"), short, ""); err == nil {
		t.Fatal("the display replaced the short link")
	}
	if err := identity.Pair(ctx, filepath.Join(t.TempDir(), "b"), long, ""); err != nil {
		t.Fatal("the display spares the kept link:", err)
	}
	out, err := server.Admin("uninvite", "")
	if err != nil || out != "cancelled 1" {
		t.Fatalf("uninvite: %q %v", out, err)
	}
	if err := identity.Pair(ctx, filepath.Join(t.TempDir(), "c"), long, ""); err == nil {
		t.Fatal("a cancelled link no longer pairs")
	}
	// After a restart too.
	again := newServer(t, dir)
	if listed, _ := again.Admin("invitations", ""); listed != "" {
		t.Fatalf("a cancelled link stays cancelled after a restart: %q", listed)
	}
}

// A kept link that expired while the daemon was down is dropped at start.
func TestExpiredKeptLinkIsDroppedAtStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "server")
	server := newServer(t, dir)
	server.addresses = []string{"127.0.0.1:1"}
	if _, err := server.Invite("", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := identity.Save(filepath.Join(dir, "invitations.json"), map[string]int64{strings.Repeat("ab", 32): time.Now().Add(-time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	later := newServer(t, dir)
	if listed, _ := later.Admin("invitations", ""); listed != "" {
		t.Fatalf("an expired kept link is gone: %q", listed)
	}
}
