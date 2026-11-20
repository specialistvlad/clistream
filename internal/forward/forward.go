// Package forward publishes an explicitly exported session port on local loopback.
package forward

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/specialistvlad/clistream/internal/tunnel"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/wire"
)

type Listener struct {
	Address string
	cancel  context.CancelFunc
}

func (l *Listener) Close() { l.cancel() }
func Start(c *quic.Conn, id, name, network, addr string) (*Listener, error) {
	host, _, e := net.SplitHostPort(addr)
	if e != nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("listener must be a numeric loopback address")
	}
	ctx, cancel := context.WithCancel(c.Context())
	open := func() (*quic.Stream, error) {
		s, _, e := wire.Open(c, wire.Message{Op: "open", ID: id, Channel: "port/" + name})
		return s, e
	}
	if network == "tcp" {
		l, e := net.Listen("tcp", addr)
		if e != nil {
			cancel()
			return nil, e
		}
		context.AfterFunc(ctx, func() { l.Close() })
		go func() {
			for {
				conn, e := l.Accept()
				if e != nil {
					return
				}
				go func() {
					defer conn.Close()
					s, e := open()
					if e != nil {
						return
					}
					stop := context.AfterFunc(ctx, func() { wire.Abort(s); conn.Close() })
					defer stop()
					tunnel.Bridge(s, conn, "tcp")
				}()
			}
		}()
		return &Listener{l.Addr().String(), cancel}, nil
	}
	if network != "udp" {
		cancel()
		return nil, errors.New("unknown protocol")
	}
	l, e := net.ListenPacket("udp", addr)
	if e != nil {
		cancel()
		return nil, e
	}
	context.AfterFunc(ctx, func() { l.Close() })
	go func() {
		var mu sync.Mutex
		peers := map[string]*quic.Stream{}
		defer func() {
			mu.Lock()
			defer mu.Unlock()
			for _, s := range peers {
				wire.Abort(s)
			}
		}()
		buf := make([]byte, 65535)
		for {
			n, a, e := l.ReadFrom(buf)
			if e != nil {
				return
			}
			k := a.String()
			mu.Lock()
			s := peers[k]
			if s == nil && len(peers) < 64 {
				s, e = open()
				if e == nil {
					peers[k] = s
					go func(s *quic.Stream) {
						defer func() { mu.Lock(); delete(peers, k); mu.Unlock(); wire.Abort(s) }()
						for {
							m, e := wire.Read(s)
							if e != nil || m.Op != "data" {
								return
							}
							if _, e = l.WriteTo(m.Data, a); e != nil {
								return
							}
						}
					}(s)
				}
			}
			mu.Unlock()
			if s != nil {
				s.SetDeadline(time.Now().Add(time.Minute))
				if e = wire.Write(s, wire.Message{Op: "data", Data: buf[:n]}); e != nil {
					wire.Abort(s)
				}
			}
		}
	}()
	return &Listener{l.LocalAddr().String(), cancel}, nil
}
