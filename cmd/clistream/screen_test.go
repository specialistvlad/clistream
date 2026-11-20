package main

import (
	"bytes"
	"github.com/creack/pty"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/specialistvlad/clistream/internal/wire"
)

// Decode the actual renderer output. Inspecting the application's cell buffer
// alone misses stale cells and screen switches sent in the wrong order.
func TestFrameSurvivesRemoteScreenChanges(t *testing.T) {
	const w, h = 80, 12
	outer := vt.NewEmulator(w, h)
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, outer); close(done) }()
	defer func() { outer.InputPipe().(io.Closer).Close(); <-done; outer.Close() }()
	remote := vt.NewEmulator(w, h-2)
	defer remote.Close()
	d := dashboardUI{width: w, height: h, page: "attached", target: wire.Supervisor{Revision: 1, Info: wire.Session{Name: "sh", Host: "devbox"}}, attachment: &viewport{term: remote}}
	for _, sequence := range []string{"hello", "\x1b[?1049h\x1b[2J\x1b[Hfull screen", strings.Repeat("scroll\r\n", 1000), "\x1b[?1049l\x1b[2J\x1b[999;1Hlast line"} {
		remote.WriteString(sequence)
		renderTerminalView(t, outer, d.View(), w, h)
		lines := strings.Split(outer.String(), "\n")
		if len(lines) != h || strings.TrimSpace(lines[0]) != "CLIStream Client │ Attached to sh on devbox" || !strings.Contains(lines[1], "Ctrl-G Home") {
			t.Fatalf("frame after remote update:\n%s", outer.String())
		}
	}
	if !strings.Contains(strings.Split(outer.String(), "\n")[h-1], "last line") {
		t.Fatal("bottom viewport row missing")
	}
	d.page = "table"
	d.host = "devbox"
	d.attachment = nil
	renderTerminalView(t, outer, d.View(), w, h)
	if strings.Contains(outer.String(), "Detach") || strings.Contains(outer.String(), "last line") {
		t.Fatalf("stale attachment after detach:\n%s", outer.String())
	}
}

// Run the real Bubble Tea renderer with a fixed view. Replay the frame up to
// terminal restoration so the test can inspect its visible cells.
type fixedView struct{ view tea.View }

func (m fixedView) Init() tea.Cmd                       { return nil }
func (m fixedView) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m fixedView) View() tea.View                      { return m.view }
func renderTerminalView(t *testing.T, outer *vt.Emulator, view tea.View, w, h int) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(master, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}); err != nil {
		t.Fatal(err)
	}
	out := frameCapture{ready: make(chan []byte, 1)}
	program := tea.NewProgram(fixedView{view}, tea.WithOutput(&out), tea.WithInput(slave), tea.WithWindowSize(w, h), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	defer func() {
		program.Quit()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	select {
	case frame := <-out.ready:
		outer.Write(frame)
	case <-time.After(3 * time.Second):
		t.Fatal("no rendered frame")
	}
}

type frameCapture struct {
	buf   bytes.Buffer
	ready chan []byte
}

func (c *frameCapture) Write(data []byte) (int, error) {
	if c.ready != nil {
		c.buf.Write(data)
		if bytes.Contains(c.buf.Bytes(), []byte("CLIStream Client")) {
			select {
			case c.ready <- bytes.Clone(c.buf.Bytes()):
			default:
			}
		}
	}
	return len(data), nil
}

func TestCompactSupervisorRows(t *testing.T) {
	const width, height = 32, 10
	d := dashboardUI{width: width, height: height, page: "table", rows: []wire.Supervisor{
		{ID: "abcdef01", Info: wire.Session{Name: "run\naway", Host: "alpha", ID: "1"}, Revision: 1},
		{ID: "abcdef02", Info: wire.Session{Name: "worker", Host: "beta"}, Revision: 1},
	}}
	contents := ansi.Strip(d.View().Content)
	for _, label := range []string{"CLIStream Client", "run�away", "abcdef01", "abcdef02", "Running", "Stopped"} {
		if !strings.Contains(contents, label) {
			t.Fatalf("missing %q in:\n%s", label, contents)
		}
	}
}

func TestCommandScrollAndResize(t *testing.T) {
	d := dashboardUI{height: 10, page: "command", line: newCommandInput("worker " + strings.Repeat("界", 30) + " last")}
	for _, width := range []int{24, 80} {
		d.width = width
		view := d.View()
		row := strings.Split(ansi.Strip(view.Content), "\n")[4]
		if !strings.Contains(row, "last") || strings.Contains(row, "worker") != (width == 80) || view.Cursor.X >= width {
			t.Errorf("width %d: %q, cursor %v", width, row, view.Cursor)
		}
	}
}

// Use the same live program for every keystroke so the test exercises
// incremental redraws, including the terminal's previous cursor position.
type editorTestModel struct{ *dashboardUI }

func (editorTestModel) Init() tea.Cmd { return nil }

type terminalWrites chan []byte

func (w terminalWrites) Write(data []byte) (int, error) {
	w <- bytes.Clone(data)
	return len(data), nil
}
func TestRestartWidgetTerminalCells(t *testing.T) {
	const w, h = 34, 10
	terminal := vt.NewEmulator(w, h)
	drained := make(chan struct{})
	go func() { io.Copy(io.Discard, terminal); close(drained) }()
	defer func() { terminal.InputPipe().(io.Closer).Close(); <-drained; terminal.Close() }()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(master, &pty.Winsize{Rows: h, Cols: w}); err != nil {
		t.Fatal(err)
	}
	out := make(terminalWrites, 64)
	d := &dashboardUI{width: w, height: h, page: "command", line: newCommandInput("codex")}
	program := tea.NewProgram(editorTestModel{d}, tea.WithInput(slave), tea.WithOutput(out), tea.WithWindowSize(w, h), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	defer func() {
		program.Quit()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	want := "codex"
	check := func() {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case data := <-out:
				terminal.Write(data)
				row := strings.Split(terminal.String(), "\n")[4]
				cursor := terminal.CursorPosition()
				if strings.Contains(row, "│› "+want+" ") && cursor.X == 3+len(want) && cursor.Y == 4 {
					return
				}
			case <-deadline:
				t.Fatalf("expected %q with cursor at %d, got %v\n%s", want, 3+len(want), terminal.CursorPosition(), terminal.String())
			}
		}
	}
	check()
	for _, r := range " --help" {
		if _, err := master.WriteString(string(r)); err != nil {
			t.Fatal(err)
		}
		want += string(r)
		check()
	}
}
