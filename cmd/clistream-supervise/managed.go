package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/files"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/session"
	"github.com/specialistvlad/clistream/internal/termio"
	"github.com/specialistvlad/clistream/internal/wire"
)

func uniqueID() string { var b [12]byte; rand.Read(b[:]); return hex.EncodeToString(b[:]) }

type childRun struct {
	process  *session.Process
	ended    chan struct{}
	code     int
	streams  sync.Map
	handlers sync.WaitGroup
}

func (r *childRun) close() {
	<-r.ended
	r.streams.Range(func(k, v any) bool { wire.Abort(k.(*quic.Stream)); return true })
	r.handlers.Wait()
}

type incoming struct {
	conn   *quic.Conn
	stream *quic.Stream
	msg    wire.Message
}

func listenManaged(c *quic.Conn, requests chan<- incoming) {
	for {
		st, err := c.AcceptStream(c.Context())
		if err != nil {
			return
		}
		go func() {
			st.SetReadDeadline(time.Now().Add(10 * time.Second))
			m, err := wire.Read(st)
			if err != nil {
				wire.Abort(st)
				return
			}
			st.SetReadDeadline(time.Time{})
			select {
			case requests <- incoming{c, st, m}:
			case <-c.Context().Done():
				wire.Abort(st)
			}
		}()
	}
}

func supervise(ctx context.Context, reg registration, conn *quic.Conn, path string, args []string, ports map[string]string, root *files.Root, local bool) (int, error) {
	state := wire.Supervisor{ID: uniqueID(), Info: reg.info}
	var current atomic.Pointer[session.Process]
	var inputEnded atomic.Bool
	var dimensions atomic.Uint32
	var run *childRun
	var ended <-chan struct{}
	start := func() {
		state.Revision++
		state.Exit = nil
		p, err := session.Start(path, args, state.Info.Mode, ports, root, true)
		state.Failed = err != nil
		if err != nil {
			fmt.Fprintln(os.Stderr, "child launch failed")
			return
		}
		state.Info.ID = uniqueID()
		r := &childRun{process: p, ended: make(chan struct{})}
		run = r
		ended = r.ended
		current.Store(p)
		if size := dimensions.Load(); size != 0 {
			p.Resize(uint16(size>>16), uint16(size))
		}
		if inputEnded.Load() {
			p.EndInput()
		}
		go func() { r.code = p.Wait(); close(r.ended) }()
	}
	finish := func() {
		current.Store(nil)
		run.close()
		state.Exit = &run.code
		state.Revision++
		state.Info.ID = ""
		run = nil
		ended = nil
	}
	start()
	defer func() {
		if run != nil {
			run.process.KillGroup()
			finish()
		}
	}()
	if local {
		if state.Info.Mode == "pty" {
			defer termio.Setup(func(w, h uint16) {
				dimensions.Store(uint32(w)<<16 | uint32(h))
				if p := current.Load(); p != nil {
					p.Resize(w, h)
				}
			})()
		}
		go func() {
			client.CopyInput(os.Stdin, func(data []byte) error {
				if p := current.Load(); p != nil {
					p.Input(data)
				}
				return nil
			})
			inputEnded.Store(true)
			if p := current.Load(); p != nil {
				p.EndInput()
			}
		}()
	}
	requests := make(chan incoming)
	var lost <-chan struct{}
	var reconnect <-chan time.Time
	publish := func() {
		if conn == nil {
			return
		}
		_, err := wire.Request(conn, wire.Message{Op: "publish", Supervisor: &state})
		if err != nil {
			conn.CloseWithError(1, "publication failed")
			return
		}
		if state.Info.ID != "" {
			fmt.Fprintln(os.Stderr, "session", state.Info.ID)
		}
	}
	connected := func() { lost = conn.Context().Done(); go listenManaged(conn, requests); publish() }
	connected()
	defer func() {
		if conn != nil {
			conn.CloseWithError(0, "finished")
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return 0, nil
		case <-lost:
			conn = nil
			lost = nil
			reconnect = time.After(time.Second)
		case <-reconnect:
			var err error
			conn, err = identity.Dial(ctx, reg.dir, reg.server)
			if err != nil {
				reconnect = time.After(time.Second)
				continue
			}
			reconnect = nil
			connected()
		case <-ended:
			finish()
			publish()
		case req := <-requests:
			// A natural exit can race an action on the previous revision.
			select {
			case <-ended:
				finish()
				publish()
			default:
			}
			m, st := req.msg, req.stream
			if req.conn != conn {
				wire.Abort(st)
				continue
			}
			if m.Op == "open" && run != nil && m.ID == state.Info.ID {
				r := run
				r.handlers.Add(1)
				r.streams.Store(st, true)
				go func() {
					defer r.handlers.Done()
					defer r.streams.Delete(st)
					defer st.Close()
					defer st.CancelRead(0)
					if err := r.process.Handle(st, m); err != nil {
						wire.Fail(st, err)
					}
				}()
				continue
			}
			response := wire.Message{Op: "ok"}
			if m.ID != state.ID || m.Run != state.Info.ID || m.Revision != state.Revision {
				wire.Fail(st, errors.New("stale supervisor"))
			} else if m.Op == "arguments" {
				response.Args = args
				wire.Write(st, response)
			} else if m.Op == "apply" || (run == nil && m.Op == "start") || (run != nil && (m.Op == "restart" || m.Op == "stop")) {
				if run != nil {
					run.process.KillGroup()
					finish()
				}
				if m.Op == "apply" {
					args = m.Args
				}
				if m.Op != "stop" {
					start()
				}
				publish()
				if state.Failed {
					wire.Fail(st, errors.New("child launch failed"))
				} else {
					wire.Write(st, response)
				}
			} else {
				wire.Fail(st, errors.New("invalid action"))
			}
			st.Close()
			st.CancelRead(0)
		}
	}
}
