package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

func TestSupervisorHeaderSurvivesChildScreenChanges(t *testing.T) {
	m := supervisorModel{name: "tool", address: "broker:7443", state: "Running", conn: connectionStatus{label: "Connected"}, width: 80, height: 12, running: true, local: true}
	m.emulator = vt.NewEmulator(m.width, m.height-2)
	defer m.emulator.Close()
	for _, seq := range []string{"\x1b[2J\x1b[Hready", "\x1b[?1049h\x1b[2J\x1b[Hready", "\x1b[?1049l\x1b[Hready"} {
		m.emulator.WriteString(seq)
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		if !strings.Contains(lines[0], "CLIStream Supervisor │ tool on broker:7443") || !strings.Contains(lines[1], "Running · Connected") || !strings.Contains(lines[2], "ready") {
			t.Fatalf("unexpected screen: %q", lines)
		}
	}
}
