//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		contains(t, f.run("a", client, "ls", id), "origin")
		dst := filepath.Join(t.TempDir(), "download")
		f.run("a", client, "get", id, "origin", dst)
		equal(t, read(t, dst), want)
	}
	download(defaultID, "default")
	download(id, "selected")
	upload := filepath.Join(f.base, "source")
	write(t, upload, "uploaded")
	f.run("a", client, "put", id, upload, "upload")
	equal(t, read(t, filepath.Join(reports, "upload")), "uploaded")
	f.reject("a", client, "put", id, upload, "upload")
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
	for _, args := range [][]string{{"--workspace", "fifo"}, {"--workspace", "missing"}, {"--workspace", "origin"}, {"--workspace="}, {"--workspace", ".", "--no-files"}} {
		contains(t, f.reject("fresh", supervisor, append(args, "sh", "-c", "touch launched")...), "--workspace")
	}
	_, err = os.Stat(filepath.Join(f.work, "launched"))
	equal(t, os.IsNotExist(err), true)
}

func TestNoFiles(t *testing.T) {
	f := setup(t)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hello from program host") }))
	defer web.Close()
	_, id := f.session([]string{"--no-files", "--share-port", "web=tcp:" + strings.TrimPrefix(web.URL, "http://")}, "cat")
	src := filepath.Join(f.base, "local")
	write(t, src, "data")
	for _, args := range [][]string{{"ls", id}, {"get", id, "file", src + ".download"}, {"put", id, src, "file"}} {
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
