//go:build e2e

package e2e

import (
	"fmt"
	"golang.org/x/term"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test executable doubles as a raw terminal child on the supervisor host.
func TestMain(m *testing.M) {
	if len(os.Args) != 2 || os.Args[1] != "clistream-pty-child" {
		os.Exit(m.Run())
	}
	if _, err := term.MakeRaw(0); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print("\x1b[?1049h" + strings.Repeat("fast output\r\n", 400) + "\x1b[2J\x1b[H\x1b[?1000h\x1b[?1006hVIEWPORT")
	var b [1]byte
	for {
		if _, err := io.ReadFull(os.Stdin, b[:]); err != nil {
			os.Exit(1)
		}
		switch b[0] {
		case '!':
			cols, rows, err := term.GetSize(0)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Printf("\r\nSIZE=%d,%d\r\n", cols, rows)
		case 'x':
			os.Exit(19)
		default:
			fmt.Printf("[%02x]", b[0])
		}
	}
}

func probe(t *testing.T) []string {
	path, err := os.Executable()
	check(t, err)
	return []string{path, "clistream-pty-child"}
}

const probeKeys = "s\x03\x1b[200~q\x1ds\x1b[201~\x1b[<0;5;8M!"

func checkProbe(t *testing.T, output string) {
	contains(t, output, "[73][03][71][1d][73]")
	contains(t, output, "[1b][5b][3c][30][3b][35][3b][36][4d]")
}

func TestFramedTerminal(t *testing.T) {
	f := setup(t)
	p := f.managed(append([]string{"--io-mode", "pty", "--"}, probe(t)...)...)
	id := p.sessionID()
	ui := f.ui("a", client)
	ui.see("R Restart")
	ui.send("S")
	ui.see("[Cancel]")
	ui.send("\r")
	ui.see("Enter Attach")
	ui.send("\r")
	ui.see("VIEWPORT")
	ui.send(probeKeys)
	ui.see("SIZE=100,22")
	ui.resize(73, 18)
	time.Sleep(200 * time.Millisecond)
	ui.send("!")
	ui.see("SIZE=73,16")
	ui.send("\x07")
	ui.see("Enter Attach")
	ui.send("q")
	ui.expect(0)
	v := f.ui("b", client, "attach", id)
	v.see("SIZE=73,16")
	checkProbe(t, v.out.contents())
	contains(t, v.out.contents(), "CLIStream Client")
	v.send("\x07")
	v.see("Enter Attach")
	v.send("q")
	v.expect(0)
	direct := f.ui("b", client, "attach", id)
	direct.see("Ctrl-G Home")
	direct.send("x")
	direct.expect(19)
}

func TestSupervisorConsoleInput(t *testing.T) {
	f := setup(t)
	ui := f.ui("sup", supervisor, append([]string{"--interactive", "--once"}, probe(t)...)...)
	ui.see("VIEWPORT")
	ui.send(probeKeys)
	ui.see("SIZE=100,22")
	v := f.start("a", client, "attach", f.sessions("a")[0].ID)
	checkProbe(t, v.see("SIZE=100,22"))
	ui.send("x")
	ui.expect(19)
	v.expect(19)
}

func TestNoRemoteRestart(t *testing.T) {
	f := setup(t)
	p := f.start("sup", supervisor, "--interactive", "--once", "--io-mode", "pty", "--local-input=false", "sh", "-c", `while read -r word; do echo "received-$word"; done; exit 23`)
	id := p.sessionID()
	first := f.ui("a", client)
	first.see("Attach only")
	first.send("rse\r")
	first.see("Ctrl-G Disconnect")
	second := f.ui("b", client, "attach", id)
	second.see("Ctrl-G Disconnect")
	first.send("\x07")
	first.see("Attach only")
	second.send("still-here\r")
	second.see("received-still-here")
	first.send("\r")
	first.see("received-still-here")
	first.send("\x04")
	first.see("exit 23")
	second.expect(23)
	p.expect(23)
	equal(t, len(f.sessions("a")), 0)
}

func TestCycleRunningApps(t *testing.T) {
	f := setup(t)
	var apps []*process
	var ids []string
	for range 3 {
		p := f.managed("/bin/cat")
		ids = append(ids, p.sessionID())
		apps = append(apps, p)
	}
	f.managed("/bin/true")
	v := f.ui("a", client, "attach", ids[0])
	v.see("Ctrl-G Home")
	identify := func(keys, word string) int {
		t.Helper()
		v.send(keys + word + "\r")
		index := -1
		eventually(t, func() bool {
			for i, p := range apps {
				if strings.Contains(p.out.contents(), word+"\n") {
					index = i
					return true
				}
			}
			return false
		})
		return index
	}
	second := identify("\x1d", "second")
	third := identify("\x1d", "third")
	equal(t, second != 0 && third != 0 && second != third, true)
	equal(t, identify("\x1d", "wrap"), 0)
	equal(t, identify("\x1b[91;5u", "previous"), third)
	v.send("\x1einput\r")
	apps[third].see("\x1einput\n")
	v.send("\x1b")
	time.Sleep(150 * time.Millisecond)
	v.send("escape\r")
	apps[third].see("\x1bescape\n")
	v.send("\x07")
	v.see("Enter Attach")
	v.send("q")
	v.expect(0)
	equal(t, len(f.sessions("a")), 3)
}

func TestRestartCommandWidget(t *testing.T) {
	f := setup(t)
	path := filepath.Join(f.work, "worker")
	write(t, path, "#!/bin/sh\nprintf 'argc=%s arg=%s\\n' \"$#\" \"$1\"\nread value\n")
	check(t, os.Chmod(path, 0700))
	p := f.managed(path)
	old := p.sessionID()
	ui := f.ui("a", client)
	ui.see("R Restart")
	ui.send("r")
	ui.see("Enter Restart & attach")
	ui.send(" --hlp\x1b[D\x1b[De")
	ui.resize(28, 12)
	ui.send("\r")
	equal(t, p.sessionID() == old, false)
	ui.see("argc=1 arg=--help")
}

func TestManagedSupervisors(t *testing.T) {
	f := setup(t)
	exe := filepath.Join(f.work, "worker")
	write(t, exe, "#!/bin/sh\nprintf '<%s>\\n' \"$@\"\nread value\nexit 23\n")
	check(t, os.Chmod(exe, 0700))
	p := f.managed(exe, "private argument", "")
	old := p.sessionID()
	first := f.ui("a", client)
	other := f.ui("b", client)
	for _, ui := range []*process{first, other} {
		ui.see("R Restart")
		ui.send("r")
		ui.see("private argument")
	}
	first.send("\r")
	replacement := p.sessionID()
	equal(t, old == replacement, false)
	first.see("Ctrl-G Home")
	other.send("\r")
	other.see("Check the current run")
	equal(t, f.sessions("a")[0].ID, replacement)
	f.reject("a", client, "attach", old)
	first.send("\x07")
	first.see("R Restart")
	first.send("e")
	first.see("private argument")
	first.send("\x1b")
	first.see("R Restart")
	first.send("s")
	first.see("[Cancel]")
	first.send("\t\r")
	first.see("R Start")
	equal(t, len(f.sessions("a")), 0)
	first.send("r")
	first.see("private argument")
	alternate := filepath.Join(f.work, "new worker")
	check(t, os.Symlink(exe, alternate))
	first.send("\x15\x1b[200~" + strconv.Quote(alternate) + " \"private argument\" \"\"\n\"a b;$HOME\"\x1b[201~\r")
	attach := func() *process {
		id := p.sessionID()
		first.see("Ctrl-G Home")
		v := f.start("a", client, "attach", id)
		v.see("<private argument>\r\n<>\r\n<a b;$HOME>\r\n")
		return v
	}
	v := attach()
	first.send("done\r")
	v.expect(23)
	first.see("Press Enter")
	first.send("\r")
	first.see("Stopped")
	f.server.stop()
	first.see("Disconnected")
	f.startServer()
	first.send("r")
	first.see("R Start")
	first.send("r")
	first.see("a b;$HOME")
	first.send("\r")
	v = attach()
	first.send("\x07")
	first.see("R Restart")
	first.send("r")
	first.see("a b;$HOME")
	first.send(" \"\r")
	first.see("unfinished quote")
	first.send("\x7f")
	check(t, os.Remove(alternate))
	first.send("\r")
	first.see("Check the executable")
	write(t, alternate, "#!/missing-interpreter\n")
	check(t, os.Chmod(alternate, 0700))
	first.send("\r")
	v.expect(128)
	first.see("Launch failed")
	first.send("\x15/bin/cat\r")
	p.sessionID()
	first.see("Ctrl-G Home")
	p.stop()
	log := read(t, f.journal())
	excludes(t, log, "private argument")
	excludes(t, log, "a b;$HOME")
	var failed bool
	var owner string
	for _, e := range records(t, log) {
		if e.Event == "supervisor_request" {
			if owner == "" {
				owner = e.Supervisor
			}
			equal(t, e.Supervisor, owner)
			equal(t, e.Device != "", true)
			if e.Session == old && e.Operation == "run-command" && e.Outcome == "failed" {
				failed = true
			}
		}
	}
	equal(t, failed, true)
}

func TestLostRestartAcknowledgement(t *testing.T) {
	f := setup(t)
	p := f.managed("sh", "-c", "echo $$ >> runs; echo ready; exec cat")
	old := p.sessionID()
	v := f.start("b", client, "attach", old)
	v.see("ready\n")
	check(t, v.cmd.Process.Signal(syscall.SIGSTOP))
	ui := f.ui("a", client)
	ui.see("R Restart")
	ui.restart()
	path := filepath.Join(f.work, "runs")
	pid, err := strconv.Atoi(strings.TrimSpace(read(t, path)))
	check(t, err)
	eventually(t, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH })
	check(t, ui.cmd.Process.Kill())
	ui.wait()
	check(t, v.cmd.Process.Signal(syscall.SIGCONT))
	next := p.sessionID()
	equal(t, next == old, false)
	v.expect(128)
	ui = f.ui("a", client)
	ui.see("R Restart")
	ui.send("\r")
	ui.see("Ctrl-G Home")
	f.server.stop()
	ui.see("Disconnected")
	ui.send("s")
	f.startServer()
	ui.send("r")
	ui.see("R Restart")
	equal(t, f.sessions("a")[0].ID, next)
	ids := strings.Fields(read(t, path))
	equal(t, len(ids), 2)
	pid, err = strconv.Atoi(ids[1])
	check(t, err)
	p.stop()
	equal(t, syscall.Kill(pid, 0), error(syscall.ESRCH))
}

func TestInteractivePairingAndReconnect(t *testing.T) {
	f := setup(t)
	display := func(ui *process) (link, clientLine, supervisorLine string) {
		screen := ui.see("Ctrl-C exits.")
		contains(t, screen, "This computer only")
		link = regexp.MustCompile(`clistream://127\.0\.0\.1/\?token=[A-Za-z0-9_-]+`).FindString(screen)
		if link == "" {
			t.Fatal("missing local pairing link")
		}
		command := func(name string) string {
			path := filepath.Join(f.bin, name)
			if !regexp.MustCompile(`^[a-zA-Z0-9_./-]+$`).MatchString(path) {
				path = "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'"
			}
			return path + " '" + link + "'"
		}
		clientLine, supervisorLine = command(client), command(supervisor)+" PROGRAM"
		contains(t, screen, clientLine)
		contains(t, screen, supervisorLine)
		return
	}
	foreground := f.ui("fresh", daemon, "--listen", "127.0.0.1:0", "--qr=false")
	_, clientLine, supervisorLine := display(foreground)
	f.run("first", "/bin/sh", "-c", "exec "+clientLine)
	shared := f.start("first-supervisor", "/bin/sh", "-c", "exec "+strings.TrimSuffix(supervisorLine, "PROGRAM")+"cat")
	shared.diagnostics.until(t, "Child started")
	equal(t, len(f.sessions("first")), 1)
	shared.stop()
	foreground.see(`"event":"pair"`)
	foreground.stop()
	duplicate := f.run("srv", daemon)
	contains(t, duplicate, "already running")
	excludes(t, duplicate, "clistream://")
	admin := f.ui("srv", daemon, "--qr=false")
	old, _, _ := display(admin)
	admin.send("p")
	fresh, clientLine, _ := display(admin)
	f.reject("guest", client, old)
	shell, err := exec.LookPath("zsh")
	if err != nil {
		shell = "/bin/sh"
	}
	viewer := f.ui("guest", shell, "-c", "exec "+clientLine)
	viewer.see("Waiting for supervisors")
	p := f.start("sup", supervisor, "--interactive", "--once", fresh, "sh", "-c", "echo first-ready; while read x; do echo reply-$x; done")
	p.sessionID()
	viewer.see("Enter Attach")
	viewer.send("\r")
	viewer.see("first-ready")
	viewer.send("shared-link-input\r")
	viewer.see("reply-shared-link-input")
	viewer.stop()
	viewer = f.ui("guest", client)
	viewer.see("Enter Attach")
	viewer.send("\r")
	viewer.see("first-ready")
	viewer.stop()
	f.session(nil, "sh", "-c", "echo second-ready; cat")
	viewer = f.ui("guest", client)
	viewer.see("Enter Attach")
	viewer.send("\x1b[B\r")
	viewer.see("-ready")
	admin.stop()
	equal(t, len(f.sessions("guest")), 2)
}
