package main

import (
	"context"
	"errors"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/activity"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/session"
	"github.com/specialistvlad/clistream/internal/wire"
)

var errLegacyRole = errors.New("registration denied. Pair this device with the server again")

type registration struct {
	dir     string
	server  identity.Profile
	info    wire.Session
	console *console
}

func (r registration) connect(ctx context.Context) (*quic.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, e := identity.Dial(ctx, r.dir, r.server)
	if e != nil {
		return nil, e
	}
	stop := abortOnCancel(ctx, c)
	defer stop()
	st, res, e := wire.Open(c, wire.Message{Op: "register", Info: &r.info})
	if e != nil {
		c.CloseWithError(1, "registration failed")
		if res.Error == "registration denied" {
			return nil, errLegacyRole
		}
		return nil, e
	}
	st.Close()
	r.console.record(activity.Event{Name: "registered", Session: res.ID})
	r.console.session(res.ID)
	return c, nil
}
func (r registration) serve(ctx context.Context, p *session.Process, c *quic.Conn) {
	for {
		active := c
		stop := context.AfterFunc(ctx, func() { active.CloseWithError(0, "finished") })
		p.Serve(active)
		stop()
		if ctx.Err() != nil {
			return
		}
		r.console.record(activity.Event{Name: "connection_lost"})
		err := context.Cause(active.Context())
		if !permanent(err) {
			c, err = retry(ctx, r.console, time.Second, r.connect)
		}
		if err != nil {
			if ctx.Err() == nil {
				r.console.status(connectionFailure(err), 0)
			}
			return
		}
	}
}
