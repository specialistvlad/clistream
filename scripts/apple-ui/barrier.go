package main

import (
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// Hold each XCTest runner at its open terminal until every other UI is attached.
func rendezvous(mux *http.ServeMux, route string, aborted <-chan struct{}) <-chan string {
	var mu sync.Mutex
	arrived := make(map[string]bool)
	arrivals := make(chan string, 3)
	ready := make(chan struct{})
	mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, route)
		if name != "mac" && name != "phone" && name != "pad" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		if !arrived[name] {
			arrived[name] = true
			sharedCheckpoint(name + " reached " + path.Base(route))
			arrivals <- name
			if len(arrived) == 3 {
				close(ready)
			}
		}
		mu.Unlock()
		select {
		case <-ready:
			w.Write([]byte("ready"))
		case <-aborted:
			http.Error(w, "a shared viewer exited", http.StatusConflict)
		case <-r.Context().Done():
		case <-time.After(16 * time.Minute):
			http.Error(w, "viewers did not attach", http.StatusRequestTimeout)
		}
	})
	return arrivals
}

func sharedMacStages(mux *http.ServeMux, route string) {
	mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
		stage := strings.TrimPrefix(r.URL.Path, route)
		switch stage {
		case "select-pty", "pty-ready", "control-received", "paste-invitation", "confirm-second-server", "select-second-session", "second-session-echo":
			sharedCheckpoint("Mac " + stage)
			w.Write([]byte("recorded"))
		default:
			http.NotFound(w, r)
		}
	})
}
