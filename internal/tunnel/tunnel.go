// Package tunnel bridges a single exported application socket to a QUIC stream.
package tunnel

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/wire"
)

// Bridge runs both directions until EOF, propagating each TCP half-close independently.
func Bridge(s *quic.Stream, c net.Conn, network string) error {
	stop := context.AfterFunc(s.Context(), func() {
		if !errors.Is(context.Cause(s.Context()), quic.ErrStreamClosed) {
			c.Close()
			s.CancelRead(1)
		}
	})
	defer stop()
	received := make(chan error, 1)
	go func() {
		err := receive(s, c)
		if err == nil && network == "tcp" {
			err = c.(*net.TCPConn).CloseWrite()
		} else {
			c.Close()
		}
		received <- err
	}()
	buf := make([]byte, 65535)
	for {
		n, err := c.Read(buf)
		if n > 0 || err == nil {
			if e := wire.Write(s, wire.Message{Op: "data", Data: buf[:n]}); e != nil {
				wire.Abort(s)
				c.Close()
				<-received
				return e
			}
		}
		if err != nil {
			if err != io.EOF && !errors.Is(err, net.ErrClosed) {
				wire.Abort(s)
				c.Close()
				<-received
				return err
			}
			break
		}
	}
	s.Close()
	return <-received
}
func receive(s *quic.Stream, c net.Conn) error {
	for {
		m, e := wire.Read(s)
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
		if m.Op != "data" {
			return errors.New("expected tunnel data")
		}
		if _, e = c.Write(m.Data); e != nil {
			return e
		}
	}
}
