package broker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/activity"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/wire"
)

// Exercise the two enrollment outcomes and mandatory compression over a real
// listener. Rejecting a raw list request must leave other streams usable.
func TestEnrollmentAndCompressedStreams(t *testing.T) {
	server, tlsConfig, err := New(filepath.Join(t.TempDir(), "server"), nil, identity.DeviceKeys{}, nil)
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
	if err := identity.Pair(ctx, device, expired, "supervisor"); err != nil {
		t.Fatal(err)
	}
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
	managed, err := wire.Managed(conn)
	if !managed || err != nil {
		t.Fatal("expected negotiated supervisor controls", err)
	}
	unknown, err := identity.Dial(ctx, filepath.Join(t.TempDir(), "unenrolled"), profile)
	if err != nil {
		t.Fatal(err)
	}
	defer unknown.CloseWithError(0, "finished")
	for _, op := range []string{"supervisors", "arguments", "restart", "publish"} {
		response, err := wire.Request(unknown, wire.Message{Op: op})
		if err == nil || response.Error != "client authorization required" {
			t.Fatalf("%s enrollment check: %v", op, err)
		}
	}
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
	t.Run("attach-only session rejects lifecycle requests", func(t *testing.T) {
		registered, err := wire.Request(conn, wire.Message{Op: "register", Info: &wire.Session{Name: "cat", Mode: "pty"}})
		if err != nil || registered.ID == "" {
			t.Fatal("register one-shot session", err)
		}
		for _, op := range []string{"command", "run-command", "arguments", "stop", "start", "restart", "apply"} {
			res, err := wire.Request(conn, wire.Message{Op: op, ID: registered.ID, Run: registered.ID})
			if err == nil || res.Error != "supervisor unavailable" {
				t.Fatalf("%s returned %+v, %v", op, res, err)
			}
		}
	})
}

func TestDisplayedInvitesShareRedemptionAcrossListeners(t *testing.T) {
	server, config, err := New(filepath.Join(t.TempDir(), "server"), nil, identity.DeviceKeys{}, nil)
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
	results := make(chan error, 3)
	for i, link := range []string{links[0], links[1], links[0]} {
		device := dir
		if i > 0 {
			device = filepath.Join(t.TempDir(), "supervisor")
		}
		go func() { results <- identity.Pair(ctx, device, link, "supervisor") }()
	}
	for range 3 {
		if err := <-results; err != nil {
			t.Fatal("concurrent enrollment", err)
		}
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

func TestConnectionActivity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "server")
	var output bytes.Buffer
	log := activity.Open(dir, true, &output)
	server, config, err := New(dir, nil, identity.DeviceKeys{}, log)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := quic.ListenAddr("127.0.0.1:0", config, wire.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server.slots = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); server.Serve(ctx, listener) }()
	stop := sync.OnceFunc(func() { cancel(); <-done })
	defer stop()
	profile := identity.Profile{Address: listener.Addr().String(), Pin: server.pin}
	device := filepath.Join(t.TempDir(), "device")
	dial := func() *quic.Conn {
		t.Helper()
		c, err := identity.Dial(ctx, device, profile)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first := dial()
	if _, _, err := wire.Open(first, wire.Message{Op: "pair", Token: "private-token-marker"}); err == nil {
		t.Fatal("invalid pairing accepted")
	}
	wait := func(c *quic.Conn) {
		t.Helper()
		select {
		case <-c.Context().Done():
		case <-time.After(12 * time.Second):
			t.Fatal("expected disconnect")
		}
	}
	wait(dial()) // First unpaired connection occupies the only slot.
	wait(first)  // Enrollment deadline.
	// The remote can observe the close before the handler releases its slot.
	deadline := time.Now().Add(5 * time.Second)
	for len(server.slots) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	third := dial()
	if _, _, err := wire.Open(third, wire.Message{Op: "private-operation-marker"}); err == nil {
		t.Fatal("unknown request accepted")
	}
	third.CloseWithError(42, "private-reason-marker")
	// Wait for the remote close to reach the broker before stopping the daemon.
	deadline = time.Now().Add(5 * time.Second)
	for {
		server.mu.Lock()
		n := len(server.peers)
		server.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("connection did not close")
		}
		time.Sleep(time.Millisecond)
	}
	stop()
	if strings.Contains(output.String(), "private-") {
		t.Fatal("private marker leaked")
	}
	opens := map[string]bool{}
	reasons := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var ev activity.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatal(err)
		}
		switch ev.Name {
		case "connection_open":
			opens[ev.Connection] = true
		case "connection_close":
			if !opens[ev.Connection] || ev.Device == "" || ev.Duration == nil {
				t.Fatal("unmatched disconnect")
			}
			delete(opens, ev.Connection)
			reasons[ev.Outcome]++
		}
	}
	if len(opens) != 0 || reasons["busy"] != 1 || reasons["enrollment_required"] != 1 || reasons["peer_error"] != 1 {
		t.Fatalf("disconnects: %v", reasons)
	}
}
