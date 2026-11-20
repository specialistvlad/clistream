// Package broker owns enrollment, device access, and the live session registry.
package broker

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/activity"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/wire"
)

type entry struct {
	info wire.Session
	conn *quic.Conn
}
type managedEntry struct {
	state wire.Supervisor
	conn  *quic.Conn
}
type Server struct {
	controllers map[string]managedEntry
	log         *activity.Log
	workers     sync.WaitGroup
	auth        identity.Authenticator
	mu          sync.Mutex
	dir, pin    string
	addresses   []string
	signingKey  ed25519.PrivateKey
	slots       chan struct{}
	now         func() time.Time
	devices     map[string]string
	invites     map[string]time.Time
	sessions    map[string]entry
	peers       map[*quic.Conn]string
}

func New(dir string, addresses []string, auth identity.Authenticator, log *activity.Log) (*Server, *tls.Config, error) {
	cert, e := identity.Key(dir)
	if e != nil {
		return nil, nil, e
	}
	s := &Server{
		controllers: map[string]managedEntry{}, log: log, auth: auth, dir: dir, addresses: addresses, pin: identity.Pin(cert.Leaf),
		signingKey: cert.PrivateKey.(ed25519.PrivateKey), now: time.Now,
		slots: make(chan struct{}, 128), devices: map[string]string{},
		invites: map[string]time.Time{}, sessions: map[string]entry{}, peers: map[*quic.Conn]string{},
	}
	b, e := os.ReadFile(filepath.Join(dir, "devices.json"))
	if e == nil {
		e = json.Unmarshal(b, &s.devices)
	}
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, nil, e
	}
	for pin, role := range s.devices {
		switch role {
		case "client", "supervisor", "device":
			s.devices[pin] = "device"
		default:
			return nil, nil, errors.New("unknown saved device role")
		}
	}
	cfg := identity.Base(cert)
	auth.Configure(cfg)
	return s, cfg, nil
}
func randomID(n int) string { b := make([]byte, n); rand.Read(b); return hex.EncodeToString(b) }
func (s *Server) Admin(action, arg string) (string, error) {
	event := activity.Event{Name: "admin_request", Outcome: "ok", Operation: "unknown"}
	switch action {
	case "display", "invite", "devices", "revoke":
		event.Operation = action
	}
	result, err := s.admin(action, arg)
	if err != nil {
		event.Outcome = "failed"
	} else if action == "revoke" {
		event.Device = arg
	}
	s.log.Record(event)
	return result, err
}
func (s *Server) admin(action, arg string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch action {
	case "invite", "display":
		if arg != "" && arg != "client" && arg != "supervisor" {
			return "", errors.New("expected client or supervisor")
		}
		now := s.now()
		for id, v := range s.invites {
			if !now.Before(v) {
				delete(s.invites, id)
			}
		}
		id := make([]byte, 32)
		rand.Read(id)
		token := hex.EncodeToString(id)
		until := now.Add(3 * time.Minute).Truncate(time.Second)
		if len(s.addresses) == 0 {
			return "", errors.New("no listening endpoints")
		}
		addresses := s.addresses
		if action == "invite" {
			// Scripts expect one line. Prefer a reachable network address for
			// remote devices when loopback and network listeners coexist.
			address := addresses[0]
			for _, candidate := range addresses {
				host, _, _ := net.SplitHostPort(candidate)
				if host != "localhost" && !net.ParseIP(host).IsLoopback() {
					address = candidate
					break
				}
			}
			addresses = []string{address}
		}
		var links []string
		for _, addr := range addresses {
			link, e := identity.SignInvitation(s.signingKey, addr, id, until)
			if e != nil {
				return "", e
			}
			links = append(links, link)
		}
		if action == "display" {
			clear(s.invites)
		}
		s.invites[token] = until
		return strings.Join(links, "\n"), nil
	case "devices":
		var lines []string
		for k, v := range s.devices {
			lines = append(lines, k+" "+v)
		}
		sort.Strings(lines)
		return strings.Join(lines, "\n"), nil
	case "revoke":
		old, ok := s.devices[arg]
		if !ok {
			return "", errors.New("unknown device")
		}
		delete(s.devices, arg)
		if e := s.save(); e != nil {
			s.devices[arg] = old
			return "", e
		}
		for c, p := range s.peers {
			if p == arg {
				c.CloseWithError(3, "revoked")
			}
		}
		return "revoked", nil
	}
	return "", errors.New("unknown action")
}
func (s *Server) save() error { return identity.Save(filepath.Join(s.dir, "devices.json"), s.devices) }
func (s *Server) Serve(ctx context.Context, listeners ...*quic.Listener) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	results := make(chan error, len(listeners))
	for _, l := range listeners {
		go func() { results <- s.accept(ctx, l) }()
	}
	var err error
	for range listeners {
		err = <-results
	}

	stop()
	s.workers.Wait()
	return err
}
func (s *Server) accept(ctx context.Context, l *quic.Listener) error {
	for {
		c, e := l.Accept(ctx)
		if e != nil {
			return e
		}
		start := time.Now()
		pin, authErr := s.auth.Principal(c.ConnectionState().TLS)
		peer := activity.Event{Name: "connection_open", Connection: randomID(12), Address: c.RemoteAddr().String(), Device: pin, Role: "unknown"}
		s.log.Record(peer)
		reason := "busy"
		if authErr == nil {
			select {
			case s.slots <- struct{}{}:
				s.workers.Add(1)
				go func() {
					defer s.workers.Done()
					defer func() { <-s.slots }()
					stop := context.AfterFunc(ctx, func() { c.CloseWithError(0, "daemon stopping") })
					defer stop()
					s.connection(c, peer, start)
				}()
				continue
			default:
				c.CloseWithError(2, "busy")
			}
		} else {
			reason = "invalid_identity"
			c.CloseWithError(3, "invalid identity")
		}
		peer.Name, peer.Outcome, peer.Duration = "connection_close", reason, activity.Since(start)
		s.log.Record(peer)
	}
}

func disconnectReason(err error) string {
	var app *quic.ApplicationError
	if errors.As(err, &app) {
		if app.Remote {
			if app.ErrorCode == 0 {
				return "peer_closed"
			}
			return "peer_error"
		}
		switch app.ErrorMessage {
		case "daemon stopping":
			return "shutdown"
		case "revoked":
			return "revoked"
		case "enrollment required":
			return "enrollment_required"
		}
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	return "transport_closed"
}

func (s *Server) connection(c *quic.Conn, peer activity.Event, start time.Time) {
	pin := peer.Device
	var closeErr error
	var clientRole atomic.Bool
	var streams sync.WaitGroup
	s.mu.Lock()
	if s.devices[pin] == "" {
		timer := time.AfterFunc(8*time.Second, func() {
			s.mu.Lock()
			unknown := s.devices[pin] == ""
			s.mu.Unlock()
			if unknown {
				c.CloseWithError(3, "enrollment required")
			}
		})
		defer timer.Stop()
	}
	s.peers[c] = pin
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.peers, c)
		for id, v := range s.controllers {
			if v.conn == c {
				delete(s.controllers, id)
				peer.Supervisor = id
				peer.Role = "supervisor"
			}
		}
		for id, v := range s.sessions {
			if v.conn == c {
				delete(s.sessions, id)
				peer.Session = id
			}
		}
		s.mu.Unlock()
		if peer.Session != "" {
			peer.Role = "supervisor"
			peer.Name = "session_removed"
			s.log.Record(peer)
		} else if clientRole.Load() {
			peer.Role = "client"
		}
		peer.Name, peer.Outcome, peer.Duration = "connection_close", disconnectReason(closeErr), activity.Since(start)
		s.log.Record(peer)
	}()
	defer streams.Wait()
	for {
		st, e := c.AcceptStream(context.Background())
		if e != nil {
			closeErr = e
			return
		}
		streams.Add(1)
		go func() {
			defer streams.Done()
			defer st.Close()
			defer st.CancelRead(0)
			st.SetReadDeadline(time.Now().Add(5 * time.Second))
			m, e := wire.Read(st)
			st.SetReadDeadline(time.Time{})
			if e == nil {
				e = s.handle(c, st, peer, m, &clientRole)
			} else {
				ev := peer
				ev.Name = "request_rejected"
				ev.Outcome = "invalid_record"
				s.log.Record(ev)
			}
			if e != nil {
				wire.Fail(st, e)
			}
		}()
	}
}
func (s *Server) handle(c *quic.Conn, st *quic.Stream, ev activity.Event, m wire.Message, clientRole *atomic.Bool) (err error) {
	pin := ev.Device
	start := time.Now()
	id := int64(st.StreamID())
	ev.Stream = &id
	ev.Name = "request_rejected"
	defer func() {
		if err != nil && ev.Outcome == "" {
			ev.Outcome = "failed"
		} else if ev.Outcome == "" {
			ev.Outcome = "ok"
		}
		ev.Duration = activity.Since(start)
		s.log.Record(ev)
	}()
	s.mu.Lock()
	_, enrolled := s.devices[pin]
	switch m.Op {
	case "pair":
		ev.Name = "pair"
		defer s.mu.Unlock()
		i, e := identity.ParseInvitation(m.Token)
		if e != nil || i.Pin != s.pin || i.Version != 2 {
			return errors.New("invalid invitation")
		}
		v, ok := s.invites[i.ID]
		if !ok || !s.now().Before(v) || !i.Expires.Equal(v) {
			return errors.New("invalid invitation; press p in the server terminal for a new link")
		}
		if !enrolled {
			s.devices[pin] = "device"
			if e := s.save(); e != nil {
				delete(s.devices, pin)
				return e
			}
		}
		return wire.Write(st, wire.Message{Op: "ok"})
	case "register":
		ev.Name = "session_register"
		defer s.mu.Unlock()
		if !enrolled {
			return errors.New("device enrollment required")
		}
		for _, v := range s.controllers {
			if v.conn == c {
				return errors.New("already registered")
			}
		}
		if m.Info == nil || (m.Info.Mode != "pipe" && m.Info.Mode != "pty") {
			return errors.New("registration denied")
		}
		for _, v := range s.sessions {
			if v.conn == c {
				return errors.New("already registered")
			}
		}
		id := randomID(12)
		info := *m.Info
		info.ID = id
		s.sessions[id] = entry{info, c}
		ev.Session, ev.Role = id, "supervisor"
		return wire.Write(st, wire.Message{Op: "ok", ID: id})
	}
	if m.Op == "list" || m.Op == "open" {
		clientRole.Store(true)
		ev.Role = "client"
		if m.Op == "list" {
			ev.Name = "session_list"
		} else {
			ev.Name = "channel_rejected"
			ev.Operation = channelOperation(m.Channel, nil)
		}
	}
	switch m.Op {
	case "command", "run-command", "arguments", "start", "stop", "restart", "apply":
		ev.Name = "supervisor_request"
		ev.Operation = m.Op
		ev.Role = "client"
		clientRole.Store(true)
		if validID(m.ID) {
			ev.Supervisor = m.ID
		}
		if validID(m.Run) {
			ev.Session = m.Run
		}
	}
	if !enrolled {
		s.mu.Unlock()
		return errors.New("client authorization required")
	}
	switch m.Op {
	case "capabilities":
		ev.Name = "capabilities"
		s.mu.Unlock()
		return wire.Write(st, wire.Message{Op: "ok", Capabilities: []string{wire.ManagedSupervisors, wire.EditCommand}})
	case "publish":
		ev.Name = "supervisor_publish"
		ev.Role = "supervisor"
		if m.Supervisor != nil {
			m.Supervisor.EditableCommand = slices.Contains(m.Capabilities, wire.EditCommand)
		}
		err := s.publish(c, m.Supervisor)
		s.mu.Unlock()
		if err != nil {
			return err
		}
		ev.Supervisor = m.Supervisor.ID
		ev.Session = m.Supervisor.Info.ID
		return wire.Write(st, wire.Message{Op: "ok"})
	case "command-supervisors", "supervisors":
		ev.Name = "supervisor_list"
		ev.Role = "client"
		clientRole.Store(true)
		rows := s.supervisors()
		if m.Op == "supervisors" {
			for i := range rows {
				rows[i].EditableCommand = false
			}
		}
		s.mu.Unlock()
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
		return wire.Write(st, wire.Message{Op: "ok", Supervisors: rows})
	case "command", "run-command", "arguments", "start", "stop", "restart", "apply":
		target, ok := s.controllers[m.ID]
		if !ok || s.devices[s.peers[target.conn]] == "" {
			s.mu.Unlock()
			return errors.New("supervisor unavailable")
		}
		if (m.Op == "command" || m.Op == "run-command") && !target.state.EditableCommand {
			s.mu.Unlock()
			return errors.New("command editing unavailable")
		}
		if m.Revision != target.state.Revision || m.Run != target.state.Info.ID {
			s.mu.Unlock()
			return errors.New("stale supervisor")
		}
		s.mu.Unlock()
		response, e := wire.Request(target.conn, m)
		if e != nil {
			if response.Op == "" {
				ev.Outcome = "unconfirmed"
			}
			if m.Op == "run-command" && (response.Error == "invalid command" || response.Error == "child launch failed") {
				ev.Outcome = "failed"
				return wire.Write(st, wire.Message{Op: "error", Error: response.Error, Revision: response.Revision})
			}
			if response.Error == "stale supervisor" {
				return errors.New("stale supervisor")
			}
			return errors.New("supervisor request failed")
		}
		return wire.Write(st, response)
	}
	if m.Op == "list" {
		items := []wire.Session{}
		for _, v := range s.sessions {
			items = append(items, v.info)
		}
		s.mu.Unlock()
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		return wire.Write(st, wire.Message{Op: "ok", Sessions: items})
	}
	v, ok := s.sessions[m.ID]
	s.mu.Unlock()
	if !ok || m.Op != "open" {
		return errors.New("unknown session or operation")
	}
	ev.Session = v.info.ID
	ev.Operation = channelOperation(m.Channel, v.info.Ports)
	up, response, e := wire.Open(v.conn, m)
	if e != nil {
		return e
	}
	defer up.CancelRead(0)
	if e = wire.Write(st, response); e != nil {
		wire.Abort(up)
		return e
	}
	ev.Name = "channel_open"
	s.log.Record(ev)
	ev.Name = "channel_close"
	relay(st, up, &ev)
	return nil
}
func channelOperation(channel string, ports map[string]string) string {
	switch channel {
	case "stdin", "stdout", "stderr", "pty", "fs/list", "fs/get", "fs/put":
		return channel
	}
	if name, ok := strings.CutPrefix(channel, "port/"); ok {
		switch ports[name] {
		case "tcp":
			return "port/tcp"
		case "udp":
			return "port/udp"
		}
		return "port"
	}
	return "unknown"
}

// Only the supervisor-to-client copy observes application results. A clean FIN
// by itself does not confirm a file transfer or establish a process exit code.
func observeResult(ev *activity.Event, m wire.Message) {
	switch m.Op {
	case "error":
		ev.Outcome = "failed"
	case "end":
		if ev.Operation == "fs/list" || ev.Operation == "fs/get" {
			ev.Outcome = "confirmed"
		}
	case "ok":
		if ev.Operation == "fs/put" {
			ev.Outcome = "confirmed"
		}
	case "exit":
		switch ev.Operation {
		case "stdout", "stderr", "pty":
			ev.Outcome = "exited"
			code := m.Code
			ev.ExitCode = &code
		}
	}
}

func relay(a, b *quic.Stream, ev *activity.Event) {
	stopOnReset := func(source, other *quic.Stream) func() bool {
		return context.AfterFunc(source.Context(), func() {
			if !errors.Is(context.Cause(source.Context()), quic.ErrStreamClosed) {
				other.CancelRead(1)
			}
		})
	}
	defer stopOnReset(a, b)()
	defer stopOnReset(b, a)()
	copySide := func(dst, src *quic.Stream) bool {
		for {
			m, e := wire.Read(src)
			if e == nil {
				if src == b {
					observeResult(ev, m)
				}
				e = wire.Write(dst, m)
			}
			if e != nil {
				if e == io.EOF {
					dst.Close()
				} else {
					dst.CancelWrite(1)
					src.CancelRead(1)
				}
				return e == io.EOF
			}
		}
	}
	done := make(chan bool, 1)
	go func() { done <- copySide(b, a) }()
	outputEOF := copySide(a, b)
	inputEOF := <-done
	if ev.Outcome == "" {
		ev.Outcome = "interrupted"
		if inputEOF && outputEOF {
			ev.Outcome = "closed"
		}
	}
}

func validID(id string) bool { _, err := hex.DecodeString(id); return len(id) == 24 && err == nil }
