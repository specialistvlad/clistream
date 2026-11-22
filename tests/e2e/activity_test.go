//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuietDashboardRefresh(t *testing.T) {
	f := setup(t)
	ui := f.ui("a", client)
	ui.see("Waiting for supervisors")
	p := f.managed("cat")
	p.sessionID()
	ui.see("R Restart")
	p.stop()
	ui.see("Waiting for supervisors")
	ui.send("q")
	ui.expect(0)
	f.server.stop()
	for _, s := range []string{f.server.diagnostics.contents(), read(t, f.journal())} {
		found := false
		for _, e := range records(t, s) {
			if e.Event == "supervisor_list" && e.Outcome == "ok" {
				t.Fatal("logged dashboard poll")
			}
			if e.Event == "supervisor_publish" && e.Outcome == "ok" {
				found = true
			}
		}
		equal(t, found, true)
	}
}

func TestActivityLog(t *testing.T) {
	for _, mode := range []string{"default", "console", "fallback", "custom"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			f.server.stop()
			original := read(t, f.journal())
			flags := []string{}
			if mode == "fallback" {
				check(t, os.Rename(f.journal(), f.journal()+".saved"))
				check(t, os.Mkdir(f.journal(), 0700))
			}
			if mode == "console" {
				flags = append(flags, "--log-file=FALSE")
			}
			if mode == "custom" {
				flags = append(flags, "--log-file="+filepath.Join(f.base, "custom.log"))
			}
			f.startServer(flags...)
			link := strings.TrimSpace(f.run("srv", daemon, "invite"))
			exe := filepath.Join(f.work, "private-program")
			write(t, exe, "#!/bin/sh\nexec /bin/cat\n")
			check(t, os.Chmod(exe, 0700))
			_, id := f.session(nil, exe)
			v := f.start("sup", client, "attach", id)
			f.echo(v, "private-terminal")
			file := filepath.Join(f.base, "upload")
			write(t, file, "private-content")
			f.run("a", client, id, "fs", "cp", "local:"+file, "private-name")
			f.run("b", client, id, "fs", "cp", "private-name", "local:"+file+"2")
			f.run("a", client, id, "fs", "ls")
			f.reject("a", client, id, "fs", "cp", "local:"+file, "missing/private-name")
			f.reject("srv", daemon, "private-admin")
			f.server.stop()
			console := f.server.diagnostics.contents()
			excludes(t, console, "private-")
			excludes(t, console, link)
			equal(t, strings.Contains(console, "writing events to stderr"), mode == "fallback")
			var journal strings.Builder
			for line := range strings.SplitSeq(console, "\n") {
				if strings.HasPrefix(line, "{") {
					journal.WriteString(line + "\n")
				}
			}
			if mode == "default" {
				equal(t, read(t, f.journal()), original+journal.String())
			}
			if mode == "custom" {
				equal(t, read(t, filepath.Join(f.base, "custom.log")), journal.String())
			}
			if mode == "console" {
				equal(t, read(t, f.journal()), original)
			}
			opened := map[string]int{}
			streams := map[[2]any]int{}
			ops := map[string]bool{}
			var supervisorDevice string
			var closed []event
			events := records(t, console)
			equal(t, events[len(events)-1].Event, "daemon_stop")
			for _, e := range events {
				switch e.Event {
				case "connection_open":
					opened[e.Connection]++
				case "connection_close":
					opened[e.Connection]--
					closed = append(closed, e)
					if e.Role == "supervisor" {
						supervisorDevice = e.Device
						equal(t, e.Session, id)
						equal(t, e.Outcome, "shutdown")
					}
				case "channel_open":
					streams[[2]any{e.Connection, e.Stream}]++
				case "channel_rejected":
					if e.Session == id {
						ops[e.Operation+" "+e.Outcome] = true
					}
				case "channel_close":
					streams[[2]any{e.Connection, e.Stream}]--
					if e.Session == id {
						ops[e.Operation+" "+e.Outcome] = true
					}
				}
			}
			for _, n := range opened {
				equal(t, n, 0)
			}
			for _, n := range streams {
				equal(t, n, 0)
			}
			for _, op := range []string{"fs/get confirmed", "fs/list confirmed", "fs/upload confirmed", "fs/upload failed"} {
				equal(t, ops[op], true)
			}
			found := false
			for _, e := range closed {
				if e.Role == "client" && e.Device == supervisorDevice {
					found = true
				}
			}
			equal(t, found, true)
		})
	}
}

func TestClosedConsole(t *testing.T) {
	f := setup(t)
	f.server.stop()
	// Give the daemon a real stderr pipe whose reader disappears after startup.
	cmd := f.command("srv", daemon, "--listen", f.address)
	rd, wr, err := os.Pipe()
	check(t, err)
	defer rd.Close()
	cmd.Stderr = wr
	check(t, cmd.Start())
	wr.Close()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	var line strings.Builder
	var b [1]byte
	rd.SetReadDeadline(time.Now().Add(8 * time.Second))
	for !strings.Contains(line.String(), "serving QUIC") {
		_, err := rd.Read(b[:])
		check(t, err)
		line.Write(b[:])
	}
	rd.Close()
	f.run("a", client, "list")
	check(t, cmd.Process.Signal(os.Interrupt))
	check(t, cmd.Wait())
	events := records(t, read(t, f.journal()))
	equal(t, events[len(events)-1].Event, "daemon_stop")
}

func TestNoLogFile(t *testing.T) {
	f := setup(t)
	f.server.stop()
	original := read(t, f.journal())
	for _, existing := range []bool{true, false} {
		if !existing {
			check(t, os.Remove(f.journal()))
		}
		f.startServer("--log-file=FALSE")
		link := strings.TrimSpace(f.run("srv", daemon, "invite"))
		f.run("a", client, link)
		f.reject("srv", daemon, "private-stderr-marker")
		f.server.stop()
		s := f.server.diagnostics.contents()
		contains(t, s, `"event":"pair"`)
		excludes(t, s, link)
		excludes(t, s, "private-stderr-marker")
		if existing {
			equal(t, read(t, f.journal()), original)
		} else {
			_, err := os.Stat(f.journal())
			equal(t, os.IsNotExist(err), true)
		}
	}
}

func TestSupervisorActivityDefault(t *testing.T) {
	f := setup(t)
	p := f.start("sup", supervisor, "sh", "-c", `if (exec 9<>/dev/tty) 2>/dev/null; then exit 8; fi; echo private-output; echo private-error >&2; read x || exit 9; echo "reply-$x"`)
	p.send("private-local\n")
	p.input.Close()
	p.diagnostics.until(t, "Child started")
	id := f.sessions("a")[0].ID
	write(t, filepath.Join(f.work, "private-file"), "private-content")
	f.run("a", client, id, "fs", "cp", "private-file", "local:"+filepath.Join(f.base, "copy"))
	v := f.start("a", client, "attach", id)
	v.send("private-remote\n")
	v.expect(0)
	contains(t, v.out.contents(), "private-output")
	contains(t, v.out.contents(), "reply-private-remote")
	contains(t, v.diagnostics.contents(), "private-error")
	p.stop()
	equal(t, p.out.contents(), "")
	feed := p.diagnostics.contents()
	contains(t, feed, "File download completed")
	contains(t, feed, "Child exited (status 0)")
	logs := f.logs()
	equal(t, len(logs), 1)
	info, err := os.Stat(logs[0])
	check(t, err)
	equal(t, info.Mode().Perm(), os.FileMode(0600))
	data := read(t, logs[0])
	excludes(t, feed+data, "private-")
	found := false
	for _, e := range records(t, data) {
		equal(t, "supervisor-"+e.Supervisor+".log", info.Name())
		if e.Operation == "fs/get" && e.Outcome == "completed" && e.Session == id {
			found = true
		}
	}
	equal(t, found, true)
}

func TestConcurrentSupervisorJournals(t *testing.T) {
	f := setup(t)
	a := f.start("sup", supervisor, "--once", "cat")
	b := f.start("sup", supervisor, "--once", "cat")
	for _, p := range []*process{a, b} {
		p.diagnostics.until(t, "Child started")
	}
	equal(t, len(f.sessions("a")), 2)
	a.stop()
	b.stop()
	logs := f.logs()
	equal(t, len(logs), 2)
	for _, path := range logs {
		exits := 0
		for _, e := range records(t, read(t, path)) {
			equal(t, filepath.Base(path), "supervisor-"+e.Supervisor+".log")
			if e.Event == "child_exited" {
				exits++
			}
		}
		equal(t, exits, 1)
	}
}

func TestSupervisorActivityTerminal(t *testing.T) {
	f := setup(t)
	ui := f.ui("sup", supervisor, "--once", "sh", "-c", `test -t 0 || exit 9; echo private-output; read x; echo "reply-$x"`)
	ui.see("Child started")
	ui.send("private-local-input\r")
	v := f.start("a", client, "attach", f.sessions("a")[0].ID)
	v.see("private-output")
	v.send("remote\n")
	v.see("reply-remote")
	v.expect(0)
	ui.see("Child exited")
	ui.expect(0)
	excludes(t, v.out.contents(), "private-local-input")
	excludes(t, ui.out.contents(), "private-")
	excludes(t, ui.out.contents(), "\x1b[?1049h")
}

func TestHomeClosesTerminalChannels(t *testing.T) {
	for _, mode := range []string{"pipe", "pty"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			p := f.start("sup", supervisor, "--io-mode", mode, "cat")
			p.diagnostics.until(t, "Child started")
			id := f.sessions("a")[0].ID
			ui := f.ui("a", client)
			ui.see("Enter Attach")
			ui.send("\r")
			ui.see("Ctrl-G Home")
			ui.send("\x07")
			ui.see("Enter Attach")
			for _, path := range []string{f.logs()[0], f.journal()} {
				want := []string{"stdin", "pty"}
				if mode == "pipe" {
					want = []string{"stdin", "stdout", "stderr"}
				}
				eventually(t, func() bool {
					ops := map[string]int{}
					for _, e := range records(t, read(t, path)) {
						if e.Session == id && (e.Event == "channel" || e.Event == "channel_close") && e.Outcome != "opened" {
							equal(t, e.Outcome, "closed")
							ops[e.Operation]++
						}
					}
					if len(ops) != len(want) {
						return false
					}
					for _, op := range want {
						equal(t, ops[op], 1)
					}
					return true
				})
			}
			equal(t, f.sessions("a")[0].ID, id)
			ui.send("\r")
			ui.see("Ctrl-G Home")
			ui.send("still-running\r")
			ui.see("still-running")
			ui.send("\x07")
			ui.see("Enter Attach")
			ui.send("q")
			ui.expect(0)
			p.stop()
			contains(t, p.diagnostics.contents(), "Terminal input closed")
			contains(t, p.diagnostics.contents(), "Terminal output closed")
		})
	}
}

func TestSupervisorJournalUnavailable(t *testing.T) {
	f := setup(t)
	write(t, filepath.Join(f.config("sup"), "device", "logs"), "file")
	p := f.start("sup", supervisor, "--once", "echo", "private-output")
	p.expect(0)
	equal(t, p.out.contents(), "")
	contains(t, p.diagnostics.contents(), "log unavailable")
	contains(t, p.diagnostics.contents(), "Child exited (status 0)")
	excludes(t, p.diagnostics.contents(), "private-output")
}
