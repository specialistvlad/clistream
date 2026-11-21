package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Start a second independent broker for the cross-server routing UI test.
func secondServer(root, bin string, mux *http.ServeMux, path string, launch func(*exec.Cmd) (func(), error)) (func(), error) {
	dir := filepath.Join(root, "second")
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, err
	}
	cmd := func(name string, args ...string) *exec.Cmd {
		c := exec.Command(filepath.Join(bin, name), args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "HOME="+dir, "XDG_CONFIG_HOME="+dir)
		return c
	}
	stop, err := launch(cmd("clistreamd", "--listen", "127.0.0.1:0", "--qr=false"))
	if err != nil {
		return nil, err
	}
	var once sync.Once
	stopBroker := func() { once.Do(stop) }
	var invite []byte
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		invite, err = cmd("clistreamd", "invite").Output()
		if err == nil {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	if err != nil {
		stopBroker()
		return nil, err
	}
	child, err := launch(cmd("clistreams", "--io-mode", "pipe", string(bytes.TrimSpace(invite)), "/bin/sh", "-c", `while IFS= read -r line; do echo "second:$line"; done`))
	if err != nil {
		stopBroker()
		return nil, err
	}
	cleanup := func() { child(); stopBroker() }
	mux.HandleFunc(path+"SECOND", func(w http.ResponseWriter, r *http.Request) {
		b, e := cmd("clistreamd", "invite").Output()
		if e != nil {
			http.Error(w, "server unavailable", 503)
			return
		}
		w.Write(b)
	})
	mux.HandleFunc(path+"SECOND-ID", func(w http.ResponseWriter, r *http.Request) {
		b, e := cmd("clistream", "list").Output()
		var rows []struct{ ID string }
		if e != nil || json.Unmarshal(b, &rows) != nil || len(rows) != 1 {
			http.Error(w, "session unavailable", 503)
			return
		}
		fmt.Fprint(w, rows[0].ID)
	})
	mux.HandleFunc(path+"SECOND-OFFLINE", func(w http.ResponseWriter, r *http.Request) { stopBroker(); fmt.Fprint(w, "stopped") })
	return cleanup, nil
}
