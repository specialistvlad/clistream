//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestSupervisorConsoleReconnect(t *testing.T) {
	f := setup(t)
	f.server.stop()
	file := filepath.Join(f.work, "launches")
	script := `echo $$ >> launches; printf '\033[2J\033[H'; echo LOCAL-READY; while read v; do [ "$v" = end ] && exit 23; echo "reply:$v"; done`
	ui := f.ui("sup", supervisor, "--interactive", "sh", "-c", script)
	ui.see("CLIStream Supervisor")
	ui.see("Reconnecting")
	_, err := os.Stat(file)
	equal(t, os.IsNotExist(err), true)
	f.startServer()
	ui.see("LOCAL-READY")
	id := f.sessions("a")[0].ID
	for attempt := range 2 {
		f.server.stop()
		ui.see("Reconnecting")
		word := fmt.Sprintf("offline-%d", attempt)
		ui.send(word + "\r")
		ui.see("reply:" + word)
		if attempt == 1 {
			ui.send("end\r")
			ui.see("exit 23")
		}
		offset := len(read(t, f.journal()))
		f.startServer()
		eventually(t, func() bool { return strings.Contains(read(t, f.journal())[offset:], `"event":"supervisor_publish"`) })
		items := f.sessions("a")
		if attempt == 0 {
			equal(t, len(items), 1)
			equal(t, items[0].ID, id)
			v := f.start("b", client, "attach", id)
			v.send("remote\n")
			ui.see("reply:remote")
			v.stop()
		} else {
			equal(t, len(items), 0)
		}
	}
	logs := f.logs()
	equal(t, len(logs), 1)
	data := read(t, logs[0])
	equal(t, strings.Count(data, `"event":"child_started"`), 1)
	equal(t, strings.Count(data, `"event":"registered"`), 3)
	equal(t, len(strings.Fields(read(t, file))), 1)
	ui.send("\x03")
	ui.expect(0)
}

func TestSupervisorConsoleReadOnlyRestart(t *testing.T) {
	f := setup(t)
	local := f.ui("sup", supervisor, "--interactive", "--local-input=false", "sh", "-c", `echo WATCHING; while read word; do echo "$word" >> seen; done`)
	local.see("WATCHING")
	local.send("ignored\r")
	id := f.sessions("a")[0].ID
	v := f.start("a", client, "attach", id)
	v.send("remote-input\n")
	eventually(t, func() bool { b, _ := os.ReadFile(filepath.Join(f.work, "seen")); return string(b) == "remote-input\n" })
	ui := f.ui("b", client)
	ui.see("R Restart")
	ui.send("r")
	ui.see("Enter Restart & attach")
	ui.send("\x15/bin/cat\r")
	ui.see("Ctrl-G Home")
	ui.send("replacement-visible\r")
	local.see("replacement-visible")
	v.expect(128)
	local.send("\x03")
	local.expect(0)
	logs := f.logs()
	equal(t, len(logs), 1)
	data := read(t, logs[0])
	excludes(t, data, "replacement-visible")
	runs := map[string]bool{}
	completed := false
	for _, e := range records(t, data) {
		if e.Event == "child_started" {
			runs[e.Session] = true
		}
		if e.Event == "remote_request" && e.Operation == "run-command" && e.Outcome == "completed" {
			completed = true
		}
	}
	equal(t, len(runs), 2)
	equal(t, runs[id] && completed, true)
}

func TestSupervisorRefusesWrongPin(t *testing.T) {
	f := setup(t)
	f.profile("sup", "Pin", strings.Repeat("0", 64))
	for _, args := range [][]string{{"true"}, {"--once", "true"}} {
		s := f.reject("sup", supervisor, args...)
		contains(t, s, "mismatch")
		excludes(t, s, "Reconnecting")
	}
}

func TestSupervisorConsoleAttachOnly(t *testing.T) {
	f := setup(t)
	ui := f.ui("sup", supervisor, "--interactive", "--once", "sh", "-c", "stty size; read x; stty size; read x; exit 17")
	ui.see("22 100")
	ui.resize(73, 18)
	time.Sleep(150 * time.Millisecond)
	ui.send("hello\r")
	ui.see("16 73")
	f.server.stop()
	ui.see("Reconnecting")
	ui.send("\x04")
	ui.expect(17)
}

func TestPty(t *testing.T) {
	f := setup(t)
	_, id := f.session([]string{"--io-mode", "pty"}, "sh", "-c", `stty -echo; echo ready; read v; printf '\033[32m%s\033[0m\n' "$v"`)
	v := f.start("a", client, "attach", id)
	v.see("ready\r\n")
	v.send("hello\n")
	v.see("\x1b[32mhello\x1b[0m\r\n")
	v.expect(0)
}

func (f *fixture) terminalInput(id string) (*process, *os.File) {
	master, slave, err := pty.Open()
	check(f.t, err)
	f.t.Cleanup(func() { master.Close(); slave.Close() })
	check(f.t, pty.Setsize(slave, &pty.Winsize{Cols: 109, Rows: 43}))
	cmd := f.command("a", client, "attach", id)
	cmd.Stdin = slave
	return f.spawn(cmd, false), master
}

func TestPtyResize(t *testing.T) {
	f := setup(t)
	_, id := f.session([]string{"--io-mode=pty"}, "sh", "-c", "stty -echo; echo ready; read trigger; stty size")
	v, master := f.terminalInput(id)
	v.see("ready\r\n")
	_, err := master.WriteString("go\n")
	check(t, err)
	v.see("43 109\r\n")
	v.expect(0)
}

func TestDetachAndReplay(t *testing.T) {
	f := setup(t)
	_, id := f.session([]string{"--io-mode=pty"}, "sh", "-c", "stty -echo; echo ready; read v; echo result-$v")
	v, master := f.terminalInput(id)
	v.see("ready\r\n")
	_, err := master.Write([]byte{0x1d})
	check(t, err)
	v.expect(0)
	next := f.start("b", client, "attach", id)
	next.see("ready\r\n")
	next.send("joined\n")
	next.see("result-joined\r\n")
	next.expect(0)
}
