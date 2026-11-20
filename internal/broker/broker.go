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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/wire"
)

type entry struct {
	info wire.Session
	conn *quic.Conn
}
type Server struct {
	auth       identity.Authenticator
	mu         sync.Mutex
	dir, pin   string
	addresses  []string
	signingKey ed25519.PrivateKey
	slots      chan struct{}
	now        func() time.Time
	devices    map[string]string
	invites    map[string]time.Time
	sessions   map[string]entry
	peers      map[*quic.Conn]string
}

func New(dir string, addresses []string, auth identity.Authenticator) (*Server, *tls.Config, error) {
	cert, e := identity.Key(dir)
	if e != nil {
		return nil, nil, e
	}
	s := &Server{
		auth: auth, dir: dir, addresses: addresses, pin: identity.Pin(cert.Leaf),
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
	results := make(chan error, len(listeners))
	for _, l := range listeners {
		go func() { results <- s.accept(ctx, l) }()
	}
	var err error
	for range listeners {
		err = <-results
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for peer := range s.peers {
		peer.CloseWithError(0, "daemon stopping")
	}
	return err
}
func (s *Server) accept(ctx context.Context, l *quic.Listener) error {
	for {
		c, e := l.Accept(ctx)
		if e != nil {
			return e
		}
		select {
		case s.slots <- struct{}{}:
			go func() { defer func() { <-s.slots }(); s.connection(c) }()
		default:
			c.CloseWithError(2, "busy")
		}
	}
}
func (s *Server) connection(c *quic.Conn) {
	pin, e := s.auth.Principal(c.ConnectionState().TLS)
	if e != nil {
		c.CloseWithError(3, "invalid identity")
		return
	}
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
		defer s.mu.Unlock()
		delete(s.peers, c)
		for id, v := range s.sessions {
			if v.conn == c {
				delete(s.sessions, id)
			}
		}
	}()
	for {
		st, e := c.AcceptStream(c.Context())
		if e != nil {
			return
		}
		go func() {
			defer st.Close()
			defer st.CancelRead(0)
			st.SetReadDeadline(time.Now().Add(5 * time.Second))
			m, e := wire.Read(st)
			st.SetReadDeadline(time.Time{})
			if e == nil {
				e = s.handle(c, st, pin, m)
			}
			if e != nil {
				wire.Fail(st, e)
			}
		}()
	}
}
func (s *Server) handle(c *quic.Conn, st *quic.Stream, pin string, m wire.Message) error {
	s.mu.Lock()
	_, enrolled := s.devices[pin]
	switch m.Op {
	case "pair":
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
		defer s.mu.Unlock()
		if !enrolled {
			return errors.New("device enrollment required")
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
		return wire.Write(st, wire.Message{Op: "ok", ID: id})
	}
	if !enrolled {
		s.mu.Unlock()
		return errors.New("client authorization required")
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
	up, response, e := wire.Open(v.conn, m)
	if e != nil {
		return e
	}
	defer up.CancelRead(0)
	if e = wire.Write(st, response); e != nil {
		wire.Abort(up)
		return e
	}
	relay(st, up)
	return nil
}
func relay(a, b *quic.Stream) {
	stopOnReset := func(source, other *quic.Stream) func() bool {
		return context.AfterFunc(source.Context(), func() {
			if !errors.Is(context.Cause(source.Context()), quic.ErrStreamClosed) {
				other.CancelRead(1)
			}
		})
	}
	defer stopOnReset(a, b)()
	defer stopOnReset(b, a)()
	copySide := func(dst, src *quic.Stream) {
		for {
			m, e := wire.Read(src)
			if e == nil {
				e = wire.Write(dst, m)
			}
			if e != nil {
				if e == io.EOF {
					dst.Close()
				} else {
					dst.CancelWrite(1)
					src.CancelRead(1)
				}
				return
			}
		}
	}
	done := make(chan struct{})
	go func() { copySide(b, a); close(done) }()
	copySide(a, b)
	<-done
}
