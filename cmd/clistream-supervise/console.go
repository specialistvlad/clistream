package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/files"
	"github.com/specialistvlad/clistream/internal/session"
	"github.com/specialistvlad/clistream/internal/terminalview"
	"golang.org/x/term"
)

type connectionStatus struct {
	label   string
	retryAt time.Time
}
type childDisplay struct {
	name, mode string
	ready      chan struct{}
	process    *session.Process
}
type attachOnly struct{}
type childStarted struct{}
type childStopped struct{ label string }
type childOutput struct {
	child *childDisplay
	data  []byte
}
type console struct {
	program    *tea.Program
	dimensions atomic.Uint32
}
type consoleWriter struct {
	c     *console
	child *childDisplay
}

func (w consoleWriter) Write(data []byte) (int, error) {
	b := bytes.Clone(data)
	if w.child.mode == "pipe" {
		b = bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
	}
	w.c.program.Send(childOutput{w.child, b})
	return len(data), nil
}

func (c *console) status(label string, delay time.Duration) {
	s := connectionStatus{label: label}
	if delay > 0 {
		s.retryAt = time.Now().Add(delay)
	}
	if c.program != nil {
		c.program.Send(s)
	} else if delay > 0 {
		fmt.Fprintf(os.Stderr, "%s in %s\n", label, delay)
	} else if label != "Connected" && label != "Connecting" {
		fmt.Fprintln(os.Stderr, label)
	}
}
func (c *console) session(id string) {
	if c.program == nil || !term.IsTerminal(int(os.Stderr.Fd())) {
		fmt.Fprintln(os.Stderr, "session", id)
	}
}
func (c *console) stopped(label string) {
	if c.program != nil {
		c.program.Send(childStopped{label})
	}
}
func (c *console) start(path string, args []string, mode string, ports map[string]string, root *files.Root, managed bool, name string) (*session.Process, error) {
	output := session.LocalOutput{Stdout: os.Stdout, Stderr: os.Stderr, Cols: 80, Rows: 24}
	var child *childDisplay
	if c.program != nil {
		size := c.dimensions.Load()
		output.Cols = uint16(size >> 16)
		output.Rows = uint16(size)
		child = &childDisplay{name: name, mode: mode, ready: make(chan struct{})}
		output.Stdout = consoleWriter{c, child}
		if term.IsTerminal(int(os.Stderr.Fd())) {
			output.Stderr = output.Stdout
		}
		c.program.Send(child)
	}
	p, err := session.Start(path, args, mode, ports, root, managed, output)
	if child != nil {
		child.process = p
		close(child.ready)
		if err == nil {
			c.program.Send(childStarted{})
		} else {
			c.stopped("Launch failed")
		}
	}
	return p, err
}

type supervisorModel struct {
	console              *console
	cancel               context.CancelFunc
	address, name, state string
	local, singleRun     bool
	width, height        int
	conn                 connectionStatus
	child                *childDisplay
	running              bool
	emulator             *vt.Emulator
	inputDone            chan struct{}
}
type clockTick struct{}

func tick() tea.Cmd                      { return tea.Tick(time.Second, func(time.Time) tea.Msg { return clockTick{} }) }
func (m *supervisorModel) Init() tea.Cmd { return tick() }
func (m *supervisorModel) close() {
	if m.emulator != nil {
		m.emulator.InputPipe().(io.Closer).Close()
		<-m.inputDone
		m.emulator.Close()
		m.emulator = nil
	}
}
func (m *supervisorModel) resize() {
	cols, rows := max(1, m.width), max(1, m.height-2)
	m.console.dimensions.Store(uint32(cols)<<16 | uint32(rows))
	if m.emulator != nil {
		m.emulator.Resize(cols, rows)
	}
	if m.running && m.child.mode == "pty" {
		m.child.process.Resize(uint16(cols), uint16(rows))
	}
}
func (m *supervisorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch ev := msg.(type) {
	case clockTick:
		return m, tick()
	case tea.WindowSizeMsg:
		if ev.Width <= 0 || ev.Height <= 0 {
			break
		}
		m.width, m.height = ev.Width, ev.Height
		m.resize()
	case attachOnly:
		m.singleRun = true
	case connectionStatus:
		m.conn = ev
	case *childDisplay:
		m.close()
		m.child = ev
		m.name = ev.name
		m.state = "Starting"
		m.running = false
		v := vt.NewEmulator(max(1, m.width), max(1, m.height-2))
		m.emulator = v
		done := make(chan struct{})
		m.inputDone = done
		go func() {
			defer close(done)
			<-ev.ready
			client.CopyInput(v, func(b []byte) error {
				if ev.process != nil && m.local {
					_ = ev.process.Input(b)
				}
				return nil
			})
		}()
	case childStarted:
		m.running = true
		m.state = "Running"
		m.resize()
	case childStopped:
		m.running = false
		m.state = ev.label
	case childOutput:
		if ev.child == m.child {
			m.emulator.Write(ev.data)
		}
	case tea.KeyPressMsg:
		if !m.running || !m.local {
			if ev.String() == "ctrl+c" {
				m.cancel()
			}
			break
		}
		if m.child.mode == "pipe" && ev.Code == tea.KeyEnter {
			m.emulator.SendText("\n")
		} else if m.child.mode == "pipe" && ev.String() == "ctrl+d" {
			m.child.process.EndInput()
		} else {
			m.emulator.SendKey(uv.KeyPressEvent(ev))
		}
	case tea.PasteMsg:
		if m.running && m.local {
			m.emulator.Paste(ev.Content)
		}
	case tea.MouseClickMsg:
		m.mouse(uv.MouseClickEvent(ev))
	case tea.MouseReleaseMsg:
		m.mouse(uv.MouseReleaseEvent(ev))
	case tea.MouseMotionMsg:
		m.mouse(uv.MouseMotionEvent(ev))
	case tea.MouseWheelMsg:
		m.mouse(uv.MouseWheelEvent(ev))
	}
	return m, nil
}
func (m *supervisorModel) mouse(e uv.MouseEvent) {
	if m.running && m.local {
		terminalview.Mouse(m.emulator, e)
	}
}
func (m *supervisorModel) View() tea.View {
	s := uv.NewScreenBuffer(max(1, m.width), max(1, m.height))
	v := tea.View{AltScreen: true}
	if m.emulator != nil {
		terminalview.Draw(&s, m.emulator, &v)
	}
	if m.running && m.local {
		v.MouseMode = tea.MouseModeAllMotion
	} else {
		v.Cursor = nil
	}
	conn := m.conn.label
	if !m.conn.retryAt.IsZero() {
		conn = fmt.Sprintf("%s in %ds", conn, max(0, int(math.Ceil(time.Until(m.conn.retryAt).Seconds()))))
	}
	status := m.state + " · " + conn
	if m.singleRun {
		status += " · Attach only"
	}
	if !m.running {
		status += "   Ctrl-C Exit"
	} else if !m.local {
		status += "   Read only · Ctrl-C Exit"
	}
	for i, label := range []string{"CLIStream Supervisor │ " + m.name + " on " + m.address, status} {
		if i >= s.Height() {
			break
		}
		uv.NewStyledString(terminalview.Printable(label)).Draw(&s, image.Rect(0, i, s.Width(), i+1))
		for x := 0; x < s.Width(); {
			cell := s.CellAt(x, i).Clone()
			cell.Style.Attrs |= uv.AttrReverse
			s.SetCell(x, i, cell)
			x += max(1, cell.Width)
		}
	}
	v.Content = s.Render()
	return v
}
func startConsole(ctx context.Context, cancel context.CancelFunc, terminal, local bool, name, address string) (*console, func()) {
	c := &console{}
	if !terminal {
		return c, func() {}
	}
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 1 || h < 1 {
		w, h = 80, 24
	}
	m := &supervisorModel{console: c, cancel: cancel, address: address, name: name, state: "Waiting to start", local: local, width: w, height: h, conn: connectionStatus{label: "Connecting"}}
	m.resize()
	c.program = tea.NewProgram(m, tea.WithContext(ctx), tea.WithoutSignalHandler(), tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := c.program.Run()
		if err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, err)
		}
		cancel()
	}()
	return c, func() { c.program.Quit(); <-done; m.close() }
}
