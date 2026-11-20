//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWorkspace(t *testing.T) {
	f := setup(t)
	reports := filepath.Join(f.work, "reports")
	check(t, os.Mkdir(reports, 0700))
	write(t, filepath.Join(reports, "origin"), "selected")
	write(t, filepath.Join(f.work, "origin"), "default")
	// Both supervisors launch in the same directory.
	def, defaultID := f.session(nil, "cat")
	proc := f.managed("--workspace", "reports", "sh", "-c", "cat origin; echo; exec cat")
	id := proc.sessionID()
	proc.see("default\n")
	download := func(id, want string) {
		t.Helper()
		contains(t, f.run("a", client, id, "fs", "ls"), "origin")
		dst := filepath.Join(t.TempDir(), "download")
		f.run("a", client, id, "fs", "cp", "origin", "local:"+dst)
		equal(t, read(t, dst), want)
	}
	download(defaultID, "default")
	download(id, "selected")
	upload := filepath.Join(f.base, "source")
	write(t, upload, "uploaded")
	f.run("a", client, id, "fs", "cp", "local:"+upload, "upload")
	equal(t, read(t, filepath.Join(reports, "upload")), "uploaded")
	f.run("a", client, id, "fs", "cp", "local:"+upload, "upload")
	_, err := os.Stat(filepath.Join(f.work, "upload"))
	equal(t, os.IsNotExist(err), true)
	def.stop()
	ui := f.ui("a", client)
	ui.see("R Restart")
	ui.restart()
	replacement := proc.sessionID()
	equal(t, replacement != id, true)
	download(replacement, "selected")
	ui.stop()
	f.server.stop()
	f.startServer()
	eventually(t, func() bool { items := f.sessions("b"); return len(items) == 1 && items[0].ID == replacement })
	download(replacement, "selected")
	check(t, syscall.Mkfifo(filepath.Join(f.work, "fifo"), 0600))
	for _, args := range [][]string{{"--workspace", "fifo"}, {"--workspace", "missing"}, {"--workspace", "origin"}, {"--workspace="}, {"--workspace", ".", "--files=FALSE"}} {
		contains(t, f.reject("fresh", supervisor, append(args, "sh", "-c", "touch launched")...), "--workspace")
	}
	_, err = os.Stat(filepath.Join(f.work, "launched"))
	equal(t, os.IsNotExist(err), true)
}

func TestNoFiles(t *testing.T) {
	f := setup(t)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hello from program host") }))
	defer web.Close()
	_, id := f.session([]string{"--files=FALSE", "--share-port", "web=tcp:" + strings.TrimPrefix(web.URL, "http://")}, "cat")
	src := filepath.Join(f.base, "local")
	write(t, src, "data")
	for _, args := range [][]string{{id, "fs", "ls"}, {id, "fs", "cp", "file", "local:" + src + ".download"}, {id, "fs", "cp", "local:" + src, "file"}} {
		contains(t, f.reject("a", client, args...), "file sharing is disabled")
	}
	f.echo(f.start("b", client, "attach", id), "still running")
	forward := f.portForward(id, "web")
	browser := http.Client{Timeout: 5 * time.Second}
	res, err := browser.Get("http://" + forward.forwardAddress())
	check(t, err)
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	check(t, err)
	equal(t, string(body), "hello from program host")
}

func TestSessionCommands(t *testing.T) {
	f := setup(t)
	p := f.managed("cat")
	first := p.sessionID()
	var rows []struct{ ID, Run, State string }
	list := func() {
		t.Helper()
		check(t, json.Unmarshal([]byte(f.run("a", client, "list")), &rows))
		equal(t, len(rows), 1)
	}
	list()
	id := rows[0].ID
	equal(t, rows[0].Run, first)
	f.run("a", client, id, "stop")
	list()
	equal(t, rows[0].ID, id)
	equal(t, rows[0].Run, "")
	f.reject("a", client, id, "stop")
	f.run("a", client, id, "start")
	p.sessionID()
	f.reject("a", client, id, "start")
	f.run("a", client, id, "restart")
	p.sessionID()
	link := strings.TrimSpace(f.run("srv", daemon, "invite"))
	f.run("new-viewer", client, link, id, "edit", "--", "sh", "-c", "printf '%s' \"$1\" > marker; exec cat", "child", "literal $HOME")
	eventually(t, func() bool {
		data, _ := os.ReadFile(filepath.Join(f.work, "marker"))
		return string(data) == "literal $HOME"
	})
	f.reject("a", client, id, "fs", "cp", "marker", "missing/")
	f.reject("a", client, id, "fs", "rm", "marker/")
	f.run("a", client, id, "fs", "mkdir", "dir/")
	f.run("a", client, id, "fs", "cp", "marker", "dir")
	f.run("a", client, id, "fs", "mv", "dir/marker", "dir/renamed")
	equal(t, f.run("a", client, id, "fs", "ls", "dir"), "renamed\n")
	f.reject("a", client, id, "fs", "rmdir", "dir")
	f.run("a", client, id, "fs", "rm", "dir/renamed")
	f.run("a", client, id, "fs", "rmdir", "dir")
	list()
	equal(t, rows[0].ID, id)
	equal(t, rows[0].Run != first, true)
}

func TestCompletedCommand(t *testing.T) {
	f := setup(t)
	p := f.managed("cat")
	p.sessionID()
	ui := f.ui("a", client)
	ui.see("R Restart")
	ui.send("r")
	ui.see("Enter Restart")
	app := filepath.Join(f.work, "usage")
	write(t, app, "#!/bin/sh\nif [ -t 1 ]; then echo tty; fi\nprintf 'usage line %d\\n' $(seq 1 120); echo diagnostic >&2; exit 7\n")
	check(t, os.Chmod(app, 0700))
	ui.send("\x15" + app + " --help\r")
	expectClientScreen(t, ui, "Exit 7", "tty", "usage line 1")
	ui.send("\x1b[F")
	expectClientScreen(t, ui, "Exit 7", "diagnostic", "usage line 120")
	excludes(t, ui.out.contents(), "\x1b[?1049l")
	ui.send("\r")
	ui.see("R Start")
	if codex, err := exec.LookPath("codex"); err == nil {
		ui.send("r")
		ui.see("Enter Start")
		ui.send("\x15" + strconv.Quote(codex) + " --help\r")
		expectClientScreen(t, ui, "Exit 0", "Usage:")
		ui.send("\r")
		ui.see("R Start")
	}
	ui.send("r")
	ui.see("Enter Start")
	ui.send("\x15/bin/sh\r")
	ui.see("Ctrl-G Home")
	ui.send("test -t 0 && printf 'interactive-%s\\n' again\r")
	ui.see("interactive-again")
}
