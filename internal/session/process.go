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
	managed  bool
	closing  bool
	readers  sync.WaitGroup
}

func Start(path string, args []string, mode string, ports map[string]string, root *files.Root, managed bool) (*Process, error) {
	p := &Process{managed: managed, Root: root, Cmd: exec.Command(path, args...), ports: ports, out: map[string]*history{}}
	if mode == "pty" {
		h := newHistory()
		p.out["pty"] = h
		t, e := pty.StartWithSize(p.Cmd, &pty.Winsize{Rows: 24, Cols: 80})
		if e != nil {
			return nil, e
		}
		p.in = t
		p.terminal = t
		p.ptyDone = make(chan struct{})
		go func() { defer close(p.ptyDone); io.Copy(io.MultiWriter(h, os.Stdout), t) }()
		return p, nil
	}
	p.out["stdout"] = newHistory()
	p.out["stderr"] = newHistory()
	if managed {
		p.Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		p.Cmd.WaitDelay = time.Second
	}
	p.Cmd.Stdout = io.MultiWriter(p.out["stdout"], os.Stdout)
	p.Cmd.Stderr = io.MultiWriter(p.out["stderr"], os.Stderr)
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
	p.closing = true
	p.mu.Unlock()
	for _, h := range p.out {
		h.finish(code)
	}
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
func (p *Process) Handle(s *quic.Stream, m wire.Message) error {
	var e error
	if m.Op != "open" {
		return errors.New("expected open")
	}
	if strings.HasPrefix(m.Channel, "fs/") {
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
