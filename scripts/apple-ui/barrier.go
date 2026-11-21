package main

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// Hold each XCTest runner at its open terminal until every other UI is attached.
func rendezvous(mux *http.ServeMux, path string) {
	var mu sync.Mutex
	arrived := make(map[string]bool)
	ready := make(chan struct{})
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, path)
		if name != "mac" && name != "phone" && name != "pad" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		if !arrived[name] {
			arrived[name] = true
			if len(arrived) == 3 {
				close(ready)
			}
		}
		mu.Unlock()
		select {
		case <-ready:
			w.Write([]byte("ready"))
		case <-r.Context().Done():
		case <-time.After(5 * time.Minute):
			http.Error(w, "viewers did not attach", http.StatusRequestTimeout)
		}
	})
}
