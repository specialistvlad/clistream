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
	server, tlsConfig, err := New(filepath.Join(t.TempDir(), "server"), "", identity.DeviceKeys{})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := quic.ListenAddr("127.0.0.1:0", tlsConfig, wire.Config())
	if err != nil {
		t.Fatal(err)
	}
	server.address = listener.Addr().String()
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
	for token, entry := range server.invites {
		entry.until = time.Now().Add(-time.Second)
		server.invites[token] = entry
	}
	server.mu.Unlock()
	if err := identity.Pair(ctx, device, expired); err == nil || !strings.Contains(err.Error(), "invalid invitation") {
		t.Fatal("expected expiration at enrollment", err)
	}
	if err := identity.Pair(ctx, device, invite()); err != nil {
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
