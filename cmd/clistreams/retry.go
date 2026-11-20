package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/wire"
)

type denied struct{ reason string }

func (e denied) Error() string { return e.reason + ". Check the server and pair this device again." }
func permanent(err error) bool {
	var rejection denied
	var addr *net.AddrError
	var app *quic.ApplicationError
	return errors.Is(err, identity.ErrServerKeyMismatch) || errors.Is(err, identity.ErrDeviceKey) || errors.As(err, &addr) || errors.As(err, &rejection) || errors.Is(err, errLegacyRole) || (errors.As(err, &app) && app.ErrorMessage == "revoked")
}

// Close a candidate connection when cancellation interrupts discovery or registration.
func abortOnCancel(ctx context.Context, c *quic.Conn) func() bool {
	return context.AfterFunc(ctx, func() { c.CloseWithError(0, "registration canceled") })
}

func connectionFailure(err error) string {
	var app *quic.ApplicationError
	if errors.As(err, &app) && app.ErrorMessage == "revoked" {
		return "Disconnected · enrollment revoked. Pair this device again."
	}
	return "Disconnected · " + err.Error()
}

type dialFunc func(context.Context) (*quic.Conn, error)

func retry(ctx context.Context, c *console, delay time.Duration, attempt dialFunc) (*quic.Conn, error) {
	for {
		if delay > 0 {
			c.status("Reconnecting", delay)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		c.status("Connecting", 0)
		attemptCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		conn, err := attempt(attemptCtx)
		cancel()
		if err == nil {
			c.status("Connected", 0)
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if permanent(err) {
			return nil, fmt.Errorf("supervisor connection: %w", err)
		}
		delay = min(max(time.Second, 2*delay), 30*time.Second)
	}
}

func publishState(c *quic.Conn, state wire.Supervisor) error {
	res, err := wire.Request(c, wire.Message{Op: "publish", Supervisor: &state, Capabilities: []string{wire.EditCommand}})
	if res.Error == "client authorization required" {
		return denied{res.Error}
	}
	return err
}
