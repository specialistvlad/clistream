// Package client is shared by the command-line and Apple frontends.
package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/wire"
)

func Connect(dir string) (*quic.Conn, error) {
	p, e := identity.Load(dir)
	if e != nil {
		return nil, e
	}
	return ConnectTo(dir, p)
}

func ConnectTo(dir string, p identity.Profile) (*quic.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return identity.Dial(ctx, dir, p)
}
func List(c *quic.Conn) ([]wire.Session, error) {
	s, r, e := wire.Open(c, wire.Message{Op: "list"})
	if e != nil {
		return nil, e
	}
	s.Close()
	return r.Sessions, nil
}

type Attachment struct {
	input   *quic.Stream
	mu      sync.Mutex
	outputs map[string]*quic.Stream
}

func Attach(c *quic.Conn, id string) (*Attachment, error) {
	items, e := List(c)
	if e != nil {
		return nil, e
	}
	mode := ""
	for _, i := range items {
		if i.ID == id {
			mode = i.Mode
		}
	}
	if mode == "" {
		return nil, errors.New("session unavailable")
	}
	channels := []string{"stdout", "stderr"}
	if mode == "pty" {
		channels = []string{"pty"}
	}
	a := &Attachment{outputs: map[string]*quic.Stream{}}
	for _, ch := range append(channels, "stdin") {
		s, _, e := wire.Open(c, wire.Message{Op: "open", ID: id, Channel: ch})
		if e != nil {
			a.Close()
			return nil, e
		}
		if ch == "stdin" {
			a.input = s
		} else {
			a.outputs[ch] = s
		}
	}
	return a, nil
}
func (a *Attachment) Close() {
	if a.input != nil {
		wire.Abort(a.input)
	}
	for _, s := range a.outputs {
		wire.Abort(s)
	}
}
func (a *Attachment) Send(m wire.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.input.SetDeadline(time.Now().Add(10 * time.Second))
	if e := wire.Write(a.input, m); e != nil {
		return e
	}
	r, e := wire.Read(a.input)
	if e != nil {
		return e
	}
	if r.Op != "ok" {
		return fmt.Errorf("input rejected: %s", r.Error)
	}
	return nil
}
func (a *Attachment) Receive(fn func(string, []byte) error) (int, error) {
	type result struct {
		code int
		e    error
	}
	results := make(chan result, len(a.outputs))
	for ch, s := range a.outputs {
		go func() {
			for {
				m, e := wire.Read(s)
				if e == nil {
					switch m.Op {
					case "data":
						e = fn(ch, m.Data)
					case "exit":
						wire.Write(s, wire.Message{Op: "ack"})
						s.Close()
						results <- result{m.Code, nil}
						return
					default:
						e = fmt.Errorf("output ended: %s", m.Error)
					}
				}
				if e != nil {
					results <- result{1, e}
					return
				}
			}
		}()
	}
	code := 0
	for range a.outputs {
		r := <-results
		if r.e != nil {
			return 1, r.e
		}
		code = r.code
	}
	return code, nil
}
func CopyInput(r io.Reader, send func([]byte) error) error {
	buf := make([]byte, 32768)
	for {
		n, e := r.Read(buf)
		if n > 0 {
			if err := send(buf[:n]); err != nil {
				return err
			}
		}
		if e != nil {
			return e
		}
	}
}
