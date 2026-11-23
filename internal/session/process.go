package session

import (
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/specialistvlad/clistream/internal/tunnel"

	"github.com/creack/pty"
	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/files"
	"github.com/specialistvlad/clistream/internal/wire"
)

type Process struct {
	Root     *files.Root
	Cmd      *exec.Cmd
	in       io.WriteCloser
	terminal *os.File
	out      map[string]*history
	ptyDone  chan struct{}
	ports    map[string]string
	mu       sync.Mutex
	observe  func(string, string)
	managed  bool
	closing  bool
	readers  sync.WaitGroup
}

type LocalOutput struct {
	Stdout, Stderr io.Writer
	Cols, Rows     uint16
	DetachTerminal bool
}

func Start(path string, args []string, mode string, ports map[string]string, root *files.Root, managed bool, output LocalOutput, observe func(string, string)) (*Process, error) {
	p := &Process{observe: observe, managed: managed, Root: root, Cmd: exec.Command(path, args...), ports: ports, out: map[string]*history{}}
	if mode == "pty" {
		h := newTerminalHistory()
		p.out["pty"] = h
		t, e := pty.StartWithSize(p.Cmd, &pty.Winsize{Rows: output.Rows, Cols: output.Cols})
		if e != nil {
			return nil, e
		}
		p.in = t
		p.terminal = t
		p.ptyDone = make(chan struct{})
		go func() { defer close(p.ptyDone); io.Copy(io.MultiWriter(h, output.Stdout), t) }()
		return p, nil
	}
	p.out["stdout"] = newHistory()
	p.out["stderr"] = newHistory()
	if output.DetachTerminal {
		// A pipe child must not reopen the supervisor's controlling terminal.
		p.Cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	} else if managed {
		p.Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if managed {
		p.Cmd.WaitDelay = time.Second
	}
	p.Cmd.Stdout = io.MultiWriter(p.out["stdout"], output.Stdout)
	p.Cmd.Stderr = io.MultiWriter(p.out["stderr"], output.Stderr)
	in, e := p.Cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	p.in = in
	e = p.Cmd.Start()
	if e != nil {
		in.Close()
		return nil, e
	}
	return p, nil
}
func (p *Process) Input(b []byte) error { _, e := p.in.Write(b); return e }
func (p *Process) EndInput() error {
	if p.terminal != nil {
		return errors.New("pipe mode required")
	}
	return p.in.Close()
}
func (p *Process) Resize(cols, rows uint16) error {
	if p.terminal == nil || cols == 0 || rows == 0 {
		return errors.New("invalid PTY size")
	}
	return pty.Setsize(p.terminal, &pty.Winsize{Cols: cols, Rows: rows})
}
func (p *Process) Wait() int {
	p.Cmd.Wait()
	if p.managed {
		p.KillGroup()
	}
	code := p.Cmd.ProcessState.ExitCode()
	if code < 0 {
		code = 128
	}
	if p.terminal != nil {
		select {
		case <-p.ptyDone:
		case <-time.After(time.Second):
			p.terminal.Close()
			<-p.ptyDone
		}
		p.terminal.Close()
	}
	p.mu.Lock()
	for _, h := range p.out {
		h.finish(code)
	}
	p.closing = true
	p.mu.Unlock()
	done := make(chan struct{})
	go func() { p.readers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	return code
}
func (p *Process) Serve(c *quic.Conn) {
	var handlers sync.WaitGroup
	defer handlers.Wait()
	for {
		s, e := c.AcceptStream(c.Context())
		if e != nil {
			return
		}
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			defer s.CancelRead(0)
			defer s.Close()
			if e := p.handle(s); e != nil {
				wire.Fail(s, e)
			}
		}()
	}
}
func (p *Process) handle(s *quic.Stream) error {
	m, e := wire.Read(s)
	if e != nil {
		return e
	}
	return p.Handle(s, m)
}
func (p *Process) KillGroup() { _ = syscall.Kill(-p.Cmd.Process.Pid, syscall.SIGKILL) }
func (p *Process) Handle(s *quic.Stream, m wire.Message) (e error) {
	if m.Op == "workspace" && wire.WorkspaceChannel(m.Channel) {
		err := p.Root.ServeWorkspace(s, m)
		if p.observe != nil {
			outcome := "completed"
			if err != nil {
				outcome = "failed"
			}
			p.observe(m.Channel, outcome)
		}
		return nil // ServeWorkspace already reported the outcome on this stream.
	}
	if m.Op != "open" {
		return errors.New("expected open")
	}
	if p.observe != nil {
		op := "unknown"
		terminal := false
		switch m.Channel {
		case "stdin", "stdout", "stderr", "pty":
			op = m.Channel
			terminal = true
		case "fs/get", "fs/put", "fs/list", "fs/upload", "fs/cp", "fs/mv", "fs/rm", "fs/mkdir", "fs/rmdir":
			op = m.Channel
		default:
			if dest, ok := p.ports[m.Channel]; ok {
				network, _, _ := strings.Cut(dest, ":")
				if network == "tcp" || network == "udp" {
					op = "port/" + network
				}
			}
		}
		defer func() {
			outcome := "completed"
			if terminal {
				outcome = "closed"
			}
			if terminal && wire.IsDetach(e) {
				// The viewer deliberately closed this attachment.
				e = nil
			}
			if e != nil {
				outcome = "failed"
			}
			p.observe(op, outcome)
		}()
	}
	if strings.HasPrefix(m.Channel, "fs/") {
		if p.Root == nil {
			return errors.New("file sharing is disabled")
		}
		return p.Root.Handle(s, m)
	}
	ok := wire.Message{Op: "ok"}
	if h := p.out[m.Channel]; h != nil {
		p.mu.Lock()
		if p.closing {
			p.mu.Unlock()
			return errors.New("process exited")
		}
		p.readers.Add(1)
		p.mu.Unlock()
		defer p.readers.Done()
		if e = wire.Write(s, ok); e != nil {
			return e
		}
		if p.observe != nil {
			p.observe(m.Channel, "opened")
		}
		if e = h.stream(s.Context(), func(m wire.Message) error { return wire.Write(s, m) }); e != nil {
			return e
		}
		s.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, e = wire.Read(s)
		return e
	}
	if export, okPort := p.ports[m.Channel]; okPort {
		network, addr, _ := strings.Cut(export, ":")
		dst, e := net.DialTimeout(network, addr, 5*time.Second)
		if e != nil {
			return e
		}
		defer dst.Close()
		if e = wire.Write(s, ok); e != nil {
			return e
		}
		return tunnel.Bridge(s, dst, network)
	}
	if m.Channel != "stdin" {
		return errors.New("unknown channel")
	}
	if e = wire.Write(s, ok); e != nil {
		return e
	}
	if p.observe != nil {
		p.observe("stdin", "opened")
	}
	for {
		m, e = wire.Read(s)
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
		switch m.Op {
		case "data":
			e = p.Input(m.Data)
		case "resize":
			e = p.Resize(m.Cols, m.Rows)
		case "eof":
			e = p.EndInput()
		default:
			return errors.New("invalid input operation")
		}
		if e != nil {
			return e
		}
		if e = wire.Write(s, ok); e != nil {
			return e
		}
	}
}

// Result streams bounded output for the selected run without opening its input.
func (p *Process) Result(s *quic.Stream) error {
	p.mu.Lock()
	finished := p.closing
	p.mu.Unlock()
	if !finished {
		return errors.New("run is still active")
	}
	if err := wire.Write(s, wire.Message{Op: "ok"}); err != nil {
		return err
	}
	var exit wire.Message
	for _, channel := range []string{"pty", "stdout", "stderr"} {
		h := p.out[channel]
		if h == nil {
			continue
		}
		h.mu.Lock()
		alternate, truncated := h.alternate, h.start > 0
		h.mu.Unlock()
		if alternate {
			return wire.Write(s, wire.Message{Op: "exit", Channel: "alternate", Code: h.code})
		}
		if truncated {
			if err := wire.Write(s, wire.Message{Op: "truncated", Channel: channel}); err != nil {
				return err
			}
		}
		err := h.stream(s.Context(), func(m wire.Message) error {
			if m.Op == "exit" {
				exit = m
				return nil
			}
			m.Channel = channel
			return wire.Write(s, m)
		})
		if err != nil {
			return err
		}
	}
	return wire.Write(s, exit)
}
