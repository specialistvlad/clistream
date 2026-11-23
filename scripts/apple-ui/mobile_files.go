package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// The Files UI owns all mutations after setup. Observe the real supervisor's directory.
func mobileFileRoutes(mux *http.ServeMux, prefix, root string) {
	const original = "CLIStream Marker.png"
	const renamed = "CLIStream Edited.png"
	const folder = "CLIStream Folder"
	mux.HandleFunc(prefix+"files/prepare", func(w http.ResponseWriter, _ *http.Request) {
		source := image.NewRGBA(image.Rect(0, 0, 24, 40))
		source.Set(1, 2, color.White)
		file, err := os.Create(filepath.Join(root, original))
		if err == nil {
			err = png.Encode(file, source)
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
		if err != nil {
			http.Error(w, "workspace fixture unavailable", 500)
			return
		}
		fmt.Fprint(w, "ready")
	})
	base := filepath.Join(root, folder)
	for _, step := range []struct {
		name, file         string
		directory, missing bool
	}{
		{"folder", base, true, false}, {"renamed", filepath.Join(root, renamed), false, false},
		{"moved", filepath.Join(base, renamed), false, false}, {"deleted", filepath.Join(base, renamed), false, true},
	} {
		mux.HandleFunc(prefix+"files/"+step.name, func(w http.ResponseWriter, r *http.Request) {
			timeout := time.NewTimer(35 * time.Second)
			defer timeout.Stop()
			tick := time.NewTicker(250 * time.Millisecond)
			defer tick.Stop()
			for {
				ok := false
				switch {
				case step.directory || step.missing:
					st, err := os.Stat(step.file)
					ok = step.directory && err == nil && st.IsDir() || step.missing && os.IsNotExist(err)
				default:
					if f, err := os.Open(step.file); err == nil {
						cfg, err := png.DecodeConfig(f)
						f.Close()
						ok = err == nil && cfg.Width == 40 && cfg.Height == 24
					}
				}
				if ok {
					fmt.Fprint(w, step.name)
					return
				}
				select {
				case <-tick.C:
				case <-timeout.C:
					http.Error(w, "workspace did not reach "+step.name, 409)
					return
				case <-r.Context().Done():
					return
				}
			}
		})
	}
}
