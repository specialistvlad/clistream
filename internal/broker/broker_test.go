package broker

import (
	"context"
	"encoding/binary"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/wire"
)

// Exercise the two enrollment outcomes and mandatory compression over a real
// listener. Rejecting a raw list request must leave other streams usable.
func TestEnrollmentAndCompressedStreams(t *testing.T) {
	server, tlsConfig, err := New(filepath.Join(t.TempDir(), "server"), nil, identity.DeviceKeys{})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := quic.ListenAddr("127.0.0.1:0", tlsConfig, wire.Config())
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
	defer func() {
		cancel()
		listener.Close()
		<-done
	}()

	invite := func() string {
		t.Helper()
		link, err := server.Admin("invite", "client")
		if err != nil {
			t.Fatal(err)
		}
		return link
	}
	device := filepath.Join(t.TempDir(), "device")
	expired := invite()
	server.mu.Lock()
	server.now = func() time.Time { return time.Now().Add(3 * time.Minute) }
	server.mu.Unlock()
	if err := identity.Pair(ctx, device, expired, "client"); err == nil || !strings.Contains(err.Error(), "invalid invitation") {
		t.Fatal("expected expiration at enrollment", err)
	}
	server.mu.Lock()
	server.now = time.Now
	server.mu.Unlock()
	if err := identity.Pair(ctx, device, invite(), "client"); err != nil {
		t.Fatal(err)
	}
	profile, err := identity.Load(device)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := identity.Dial(ctx, device, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "test complete")
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer wire.Abort(stream)
	stream.SetDeadline(time.Now().Add(3 * time.Second))
	plain := []byte(`{"Op":"list"}`)
	record := binary.BigEndian.AppendUint32(nil, uint32(len(plain)))
	if _, err = stream.Write(append(record, plain...)); err != nil {
		t.Fatal(err)
	}
	rejected, err := wire.Read(stream)
	if err != nil || rejected.Op != "error" {
		t.Fatal("expected stream rejection for an uncompressed request", err, rejected.Op)
	}
	valid, _, err := wire.Open(conn, wire.Message{Op: "list"})
	if err != nil {
		t.Fatal(err)
	}
	valid.Close()
}

func TestDisplayedInvitesShareRedemptionAcrossListeners(t *testing.T) {
	server, config, err := New(filepath.Join(t.TempDir(), "server"), nil, identity.DeviceKeys{})
	if err != nil {
		t.Fatal(err)
	}
	var listeners []*quic.Listener
	for range 2 {
		l, err := quic.ListenAddr("127.0.0.1:0", config, wire.Config())
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		listeners = append(listeners, l)
		server.addresses = append(server.addresses, l.Addr().String())
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); server.Serve(ctx, listeners...) }()
	defer func() { cancel(); <-done }()
	display := func() []string {
		links, e := server.Admin("display", "client")
		if e != nil {
			t.Fatal(e)
		}
		return strings.Split(links, "\n")
	}
	first := display()
	links := display()
	if len(links) != 2 {
		t.Fatal("expected a link per listener")
	}
	dir := filepath.Join(t.TempDir(), "client")
	for _, link := range first {
		if identity.Pair(ctx, dir, link, "client") == nil {
			t.Fatal("replaced link was accepted")
		}
	}
	results := make(chan error, 2)
	for _, link := range links {
		go func() { results <- identity.Pair(ctx, dir, link, "client") }()
	}
	a, b := <-results, <-results
	if (a == nil) == (b == nil) {
		t.Fatal("expected exactly one accepted enrollment", a, b)
	}
	profile, e := identity.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	profile.Address = listeners[1].Addr().String()
	conn, e := identity.Dial(ctx, dir, profile)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.CloseWithError(0, "finished")
	display()
	listeners[0].Close()
	st, _, e := wire.Open(conn, wire.Message{Op: "list"})
	if e != nil {
		t.Fatal(e)
	}
	st.Close()
}
