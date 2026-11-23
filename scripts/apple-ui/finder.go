package main

import (
	"crypto/sha256"
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
					fmt.Fprintf(os.Stderr, "Finder checkpoint %s passed\n", check.operation)
					fmt.Fprint(w, check.operation)
					return
				}
				select {
				case <-r.Context().Done():
					return
				case <-deadline.C:
					// Only report the known fixture names and fixture contents. Never log
					// the request URL; it contains the short-lived enrollment endpoint.
					fmt.Fprintf(os.Stderr, "Finder checkpoint %s timed out\n", check.operation)
					for _, name := range []string{incoming, folder, filepath.Join(folder, incoming)} {
						st, statErr := os.Stat(filepath.Join(workspace, name))
						switch {
						case statErr != nil:
							fmt.Fprintf(os.Stderr, "Finder fixture %q missing=%t\n", name, errors.Is(statErr, os.ErrNotExist))
						case st.IsDir():
							fmt.Fprintf(os.Stderr, "Finder fixture %q is a directory\n", name)
						default:
							actual, _ := os.ReadFile(filepath.Join(workspace, name))
							switch string(actual) {
							case "", "written on the Mac\n", "edited on the host\n", "saved on the Mac\n":
								fmt.Fprintf(os.Stderr, "Finder fixture %q bytes=%q\n", name, actual)
							default:
								fmt.Fprintf(os.Stderr, "Finder fixture %q bytes=%d sha256=%x\n", name, len(actual), sha256.Sum256(actual))
							}
						}
					}
					http.Error(w, "remote workspace did not reach "+check.operation, 409)
					return
				case <-ticker.C:
				}
			}
		})
	}
}
