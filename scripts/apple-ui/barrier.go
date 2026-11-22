package main

import (
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// Hold each XCTest runner at its open terminal until every other UI is attached.
func rendezvous(mux *http.ServeMux, route string) <-chan string {
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
			fmt.Printf("%s reached %s\n", name, path.Base(route))
			arrivals <- name
			if len(arrived) == 3 {
				close(ready)
			}
		}
		mu.Unlock()
		select {
		case <-ready:
			w.Write([]byte("ready"))
		case <-r.Context().Done():
		case <-time.After(16 * time.Minute):
			http.Error(w, "viewers did not attach", http.StatusRequestTimeout)
		}
	})
	return arrivals
}
