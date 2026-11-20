package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/session"
	"github.com/specialistvlad/clistream/internal/wire"
)

var errLegacyRole = errors.New("registration denied")

type registration struct {
	dir    string
	server identity.Profile
	info   wire.Session
}

func (r registration) connect(ctx context.Context) (*quic.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, e := identity.Dial(ctx, r.dir, r.server)
	if e != nil {
		return nil, e
	}
	st, res, e := wire.Open(c, wire.Message{Op: "register", Info: &r.info})
	if e != nil {
		c.CloseWithError(1, "registration failed")
		if res.Error == "registration denied" {
			return nil, errLegacyRole
		}
		return nil, e
	}
	st.Close()
	fmt.Fprintln(os.Stderr, "session", res.ID)
	return c, nil
}
func (r registration) serve(ctx context.Context, p *session.Process, c *quic.Conn) {
	delay := time.Second
	for {
		if c != nil {
			active := c
			stop := context.AfterFunc(ctx, func() { active.CloseWithError(0, "finished") })
			p.Serve(c)
			stop()
			delay = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		var e error
		c, e = r.connect(ctx)
		if e != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "reconnect:", e)
			delay = min(delay*2, 30*time.Second)
		}
	}
}
