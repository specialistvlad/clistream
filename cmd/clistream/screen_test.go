package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/specialistvlad/clistream/internal/wire"
)

// Decode the actual renderer output. Inspecting the application's cell buffer
// alone misses stale cells and screen switches sent in the wrong order.
func TestFrameSurvivesRemoteScreenChanges(t *testing.T) {
	const w, h = 80, 12
	var output bytes.Buffer
	screen := uv.NewTerminalScreen(&output, []string{"TERM=xterm-256color"})
	screen.Resize(w, h)
	screen.EnterAltScreen()
	screen.Flush()
	outer := vt.NewEmulator(w, h)
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, outer); close(done) }()
	defer func() { outer.InputPipe().(io.Closer).Close(); <-done; outer.Close() }()
	remote := vt.NewEmulator(w, h-2)
	defer remote.Close()
	d := dashboardUI{screen: screen, page: "attached", target: wire.Supervisor{Revision: 1, Info: wire.Session{Name: "sh", Host: "devbox"}}, attachment: &viewport{term: remote}}
	for _, sequence := range []string{"hello", "\x1b[?1049h\x1b[2J\x1b[Hfull screen", strings.Repeat("scroll\r\n", 1000), "\x1b[?1049l\x1b[2J\x1b[999;1Hlast line"} {
		remote.WriteString(sequence)
		if err := d.draw(); err != nil {
			t.Fatal(err)
		}
		outer.Write(output.Bytes())
		output.Reset()
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
	if err := d.draw(); err != nil {
		t.Fatal(err)
	}
	outer.Write(output.Bytes())
	if strings.Contains(outer.String(), "Detach") || strings.Contains(outer.String(), "last line") {
		t.Fatalf("stale attachment after detach:\n%s", outer.String())
	}
}

func TestCompactSupervisorRows(t *testing.T) {
	const width, height = 32, 10
	screen := uv.NewTerminalScreen(io.Discard, nil)
	screen.Resize(width, height)
	d := dashboardUI{screen: screen, page: "table", rows: []wire.Supervisor{
		{ID: "abcdef01", Info: wire.Session{Name: "run\naway", Host: "alpha", ID: "1"}, Revision: 1},
		{ID: "abcdef02", Info: wire.Session{Name: "worker", Host: "beta"}, Revision: 1},
	}}
	if err := d.draw(); err != nil {
		t.Fatal(err)
	}
	var contents strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			contents.WriteString(screen.CellAt(x, y).Content)
		}
		contents.WriteByte('\n')
	}
	for _, label := range []string{"CLIStream Client", "run�away", "abcdef01", "abcdef02", "Running", "Stopped"} {
		if !strings.Contains(contents.String(), label) {
			t.Fatalf("missing %q in:\n%s", label, contents.String())
		}
	}
}

func TestCommandScrollAndResize(t *testing.T) {
	screen := uv.NewTerminalScreen(io.Discard, nil)
	d := dashboardUI{screen: screen, page: "command"}
	d.line.insert("worker " + strings.Repeat("界", 30) + " last")
	for _, width := range []int{24, 80} {
		screen.Resize(width, 10)
		if err := d.draw(); err != nil {
			t.Fatal(err)
		}
		var row strings.Builder
		for x := 0; x < width; x++ {
			row.WriteString(screen.CellAt(x, 4).Content)
		}
		if !strings.Contains(row.String(), "last") || strings.Contains(row.String(), "worker") != (width == 80) {
			t.Errorf("width %d: %q", width, row.String())
		}
	}
}
