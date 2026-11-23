package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// The Finder test uses the production provider for all file access. These
// endpoints observe the other end of the transfer on either supported host.
func finderRoutes(mux *http.ServeMux, prefix, workspace string) {
	const incoming = "finder-roundtrip.txt"
	const folder = "Finder Round Trip"
	mux.HandleFunc(prefix+"finder/host-edit", func(w http.ResponseWriter, _ *http.Request) {
		if err := os.WriteFile(filepath.Join(workspace, incoming), []byte("edited on the host\n"), 0600); err != nil {
			http.Error(w, "host edit failed", 500)
			return
		}
		fmt.Fprint(w, "edited")
	})
	for _, check := range []struct{ operation, path, contents string }{
		{"uploaded", incoming, "written on the Mac\n"},
		{"moved", filepath.Join(folder, incoming), "edited on the host\n"},
		{"saved", filepath.Join(folder, incoming), "saved on the Mac\n"},
		{"deleted", folder, ""},
	} {
		mux.HandleFunc(prefix+"finder/"+check.operation, func(w http.ResponseWriter, r *http.Request) {
			deadline := time.NewTimer(30 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(150 * time.Millisecond)
			defer ticker.Stop()
			for {
				data, err := os.ReadFile(filepath.Join(workspace, check.path))
				if (check.operation == "deleted" && errors.Is(err, os.ErrNotExist)) ||
					(check.operation != "deleted" && err == nil && string(data) == check.contents) {
					fmt.Fprint(w, check.operation)
					return
				}
				select {
				case <-r.Context().Done():
					return
				case <-deadline.C:
					http.Error(w, "remote workspace did not reach "+check.operation, 409)
					return
				case <-ticker.C:
				}
			}
		})
	}
}
