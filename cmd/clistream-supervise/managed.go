package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/activity"
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

func validateCommand(cmd [][]byte) ([]string, error) {
	var argv []string
	for _, arg := range cmd {
		if strings.IndexByte(string(arg), 0) >= 0 {
			return nil, errors.New("invalid command")
		}
		argv = append(argv, string(arg))
	}
	if len(argv) == 0 || argv[0] == "" {
		return nil, errors.New("invalid command")
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return nil, err
	}
	return argv, nil
}

func supervise(ctx context.Context, reg registration, conn *quic.Conn, state wire.Supervisor, path string, args []string, ports map[string]string, root *files.Root, local bool) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	var current atomic.Pointer[session.Process]
	var inputEnded atomic.Bool

	var restore func()
	var run *childRun
	var ended <-chan struct{}
	start := func() {
		state.Revision++
		state.Exit = nil
		runID := uniqueID()
		p, err := reg.console.start(runID, path, args, state.Info.Mode, ports, root, true, state.Info.Name)
		state.Failed = err != nil
		if err != nil {
			if reg.console.program == nil && !reg.console.logOnly {
				fmt.Fprintln(os.Stderr, "child launch failed")
			}
			return
		}
		state.Info.ID = runID
		r := &childRun{process: p, ended: make(chan struct{})}
		run = r
		ended = r.ended
		current.Store(p)
		if local && reg.console.program == nil && state.Info.Mode == "pty" {
			restore = termio.Setup(func(w, h uint16) { p.Resize(w, h) })
		}
		if inputEnded.Load() {
			p.EndInput()
		}
		go func() { r.code = p.Wait(); close(r.ended) }()
	}
	finish := func() {
		if restore != nil {
			restore()
			restore = nil
		}
		current.Store(nil)
		run.close()
		reg.console.exit(state.Info.ID, run.code)
		reg.console.stopped(fmt.Sprintf("Stopped · exit %d", run.code))
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
	if local && reg.console.program == nil {
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
	type registrationResult struct {
		conn     *quic.Conn
		revision uint64
		err      error
	}
	var latest atomic.Pointer[wire.Supervisor]
	var recovered <-chan registrationResult
	reconnect := func() {
		ready := make(chan registrationResult)
		recovered = ready
		workers.Add(1)
		go func() {
			defer workers.Done()
			var revision uint64
			c, err := retry(ctx, reg.console, time.Second, func(ctx context.Context) (*quic.Conn, error) {
				c, err := identity.Dial(ctx, reg.dir, reg.server)
				if err != nil {
					return nil, err
				}
				stop := abortOnCancel(ctx, c)
				defer stop()
				snapshot := latest.Load()
				revision = snapshot.Revision
				if err = publishState(c, *snapshot); err != nil {
					c.CloseWithError(1, "publication failed")
					return nil, err
				}
				reg.console.record(activity.Event{Name: "registered", Session: snapshot.Info.ID})
				return c, nil
			})
			select {
			case ready <- registrationResult{c, revision, err}:
			case <-ctx.Done():
				if c != nil {
					c.CloseWithError(0, "finished")
				}
			}
		}()
	}
	publish := func() {
		snapshot := state
		latest.Store(&snapshot)
		if conn == nil {
			return
		}
		err := publishState(conn, state)
		if err != nil {
			conn.CloseWithError(1, "publication failed")
			return
		}
		if state.Info.ID != "" {
			reg.console.session(state.Info.ID)
		}
	}
	listen := func() { lost = conn.Context().Done(); go listenManaged(conn, requests) }
	listen()
	publish()
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
			reg.console.record(activity.Event{Name: "connection_lost"})
			cause := context.Cause(conn.Context())
			conn = nil
			lost = nil
			if permanent(cause) {
				reg.console.status(connectionFailure(cause), 0)
			} else {
				reconnect()
			}
		case res := <-recovered:
			recovered = nil
			if res.err != nil {
				reg.console.status(connectionFailure(res.err), 0)
				continue
			}
			conn = res.conn
			listen()
			if state.Revision != res.revision {
				publish()
			} else if state.Info.ID != "" {
				reg.console.session(state.Info.ID)
			}
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
			outcome := "completed"
			response := wire.Message{Op: "ok"}
			if m.ID != state.ID || m.Run != state.Info.ID || m.Revision != state.Revision {
				outcome = "failed"
				wire.Fail(st, errors.New("stale supervisor"))
			} else if m.Op == "command" {
				response.Command = [][]byte{[]byte(path)}
				for _, arg := range args {
					response.Command = append(response.Command, []byte(arg))
				}
				wire.Write(st, response)
			} else if m.Op == "run-command" {
				argv, err := validateCommand(m.Command)
				select {
				case <-ended:
					finish()
					publish()
				default:
				}
				if m.Revision != state.Revision {
					outcome = "failed"
					wire.Fail(st, errors.New("stale supervisor"))
				} else if err != nil {
					outcome = "failed"
					wire.Fail(st, errors.New("invalid command"))
				} else {
					if run != nil {
						run.process.KillGroup()
						finish()
					}
					oldPath, oldArgs, oldInfo := path, args, state.Info
					path, args = argv[0], argv[1:]
					state.Info.Name = filepath.Base(path)
					start()
					if state.Failed {
						path, args, state.Info = oldPath, oldArgs, oldInfo
						outcome = "failed"
						response = wire.Message{Op: "error", Error: "child launch failed", Revision: state.Revision}
					} else {
						response.Run = state.Info.ID
					}
					publish()
					wire.Write(st, response)
				}
			} else if m.Op == "arguments" {
				response.Args = args
				wire.Write(st, response)
			} else if m.Op == "apply" || (run == nil && m.Op == "start") || (run != nil && (m.Op == "restart" || m.Op == "stop")) {
				if run != nil {
					run.process.KillGroup()
					finish()
				}
				previousArgs := args
				if m.Op == "apply" {
					args = m.Args
				}
				if m.Op != "stop" {
					start()
				}
				publish()
				if state.Failed {
					args = previousArgs
					outcome = "failed"
					wire.Fail(st, errors.New("child launch failed"))
				} else {
					wire.Write(st, response)
				}
			} else {
				outcome = "failed"
				wire.Fail(st, errors.New("invalid action"))
			}
			switch m.Op {
			case "command", "arguments", "start", "stop", "restart", "apply", "run-command":
				reg.console.record(activity.Event{Name: "remote_request", Session: m.Run, Operation: m.Op, Outcome: outcome})
			}
			st.Close()
			st.CancelRead(0)
		}
	}
}

func selectMode(mode string, terminal bool) (string, error) {
	switch mode {
	case "pipe", "pty":
		return mode, nil
	case "auto":
		if terminal {
			return "pty", nil
		}
		return "pipe", nil
	default:
		return "", errors.New("--mode requires auto, pipe or pty")
	}
}
