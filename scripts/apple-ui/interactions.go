package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/specialistvlad/clistream/internal/identity"
)

func interactionRoutes(mux *http.ServeMux, path, root string, link []byte) error {
	invitation, err := identity.ParseInvitation(strings.TrimSpace(string(link)))
	if err != nil {
		return err
	}
	mux.HandleFunc(path+"EXPIRED", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Until(invitation.Expires.Add(time.Second))):
			w.Write(link)
		}
	})
	mux.HandleFunc(path+"HISTORY", func(w http.ResponseWriter, r *http.Request) {
		if err := os.WriteFile(filepath.Join(root, "history"), nil, 0600); err != nil {
			http.Error(w, "history unavailable", 500)
			return
		}
		fmt.Fprint(w, "released")
	})
	mux.HandleFunc(path+"RESET", func(w http.ResponseWriter, r *http.Request) {
		if err := os.WriteFile(filepath.Join(root, "reset-terminal"), nil, 0600); err != nil {
			http.Error(w, "terminal unavailable", 500)
			return
		}
		fmt.Fprint(w, "reset")
	})
	mux.HandleFunc(path+"CONTROL-COUNT", func(w http.ResponseWriter, _ *http.Request) {
		data, err := os.ReadFile(filepath.Join(root, "keys"))
		if err != nil {
			http.Error(w, "terminal input unavailable", http.StatusServiceUnavailable)
			return
		}
		// Expose only the count of ETX bytes received by this fixture child.
		fmt.Fprint(w, bytes.Count(data, []byte{3}))
	})
	grids := func() []string {
		data, _ := os.ReadFile(filepath.Join(root, "grid"))
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
	var observed atomic.Int64
	mux.HandleFunc(path+"GRID", func(w http.ResponseWriter, r *http.Request) {
		sizes := grids()
		observed.Store(int64(len(sizes)))
		fmt.Fprint(w, sizes[len(sizes)-1])
	})
	mux.HandleFunc(path+"RESIZED", func(w http.ResponseWriter, r *http.Request) {
		offset := int(observed.Load())
		for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
			sizes := grids()
			if offset > 0 && len(sizes) > offset {
				for _, size := range sizes[offset:] {
					if size != "" && size != sizes[offset-1] {
						fmt.Fprint(w, size)
						return
					}
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		http.Error(w, "grid did not change", 409)
	})
	udid := os.Getenv("SIMULATOR_ID")
	if udid == "" {
		return nil
	}
	mux.HandleFunc(path+"content-size/", func(w http.ResponseWriter, r *http.Request) {
		size := strings.TrimPrefix(r.URL.Path, path+"content-size/")
		args := []string{"simctl", "ui", udid, "content_size"}
		switch size {
		case "current":
		case "extra-small", "small", "medium", "large", "extra-large", "extra-extra-large",
			"extra-extra-extra-large", "accessibility-medium", "accessibility-large",
			"accessibility-extra-large", "accessibility-extra-extra-large", "accessibility-extra-extra-extra-large":
			if err := exec.CommandContext(r.Context(), "xcrun", append(args, size)...).Run(); err != nil {
				http.Error(w, "unable to change simulator text size", http.StatusServiceUnavailable)
				return
			}
		default:
			http.NotFound(w, r)
			return
		}
		value, err := exec.CommandContext(r.Context(), "xcrun", args...).Output()
		if err != nil {
			http.Error(w, "unable to read simulator text size", http.StatusServiceUnavailable)
			return
		}
		w.Write(bytes.TrimSpace(value))
	})
	if os.Getenv("TEST_RUNNER_CLISTREAM_CAPTURE_GALLERY") == "1" {
		mux.HandleFunc(path+"appearance/", func(w http.ResponseWriter, r *http.Request) {
			mode := strings.TrimPrefix(r.URL.Path, path+"appearance/")
			if mode != "light" && mode != "dark" {
				http.NotFound(w, r)
				return
			}
			if err := exec.CommandContext(r.Context(), "xcrun", "simctl", "ui", udid, "appearance", mode).Run(); err != nil {
				http.Error(w, "unable to set simulator appearance", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprint(w, mode)
		})
	}
	touch := func(w http.ResponseWriter, r *http.Request) {
		keys := []string{"x", "y"}
		extend := r.URL.Path == path+"EXTEND"
		if extend {
			keys = append(keys, "endX", "endY")
		}
		var pos []string
		for _, key := range keys {
			v, err := strconv.ParseFloat(r.URL.Query().Get(key), 64)
			if err != nil || !(v >= 0 && v <= 8192) {
				http.Error(w, "invalid point", 400)
				return
			}
			pos = append(pos, strconv.FormatFloat(v, 'f', 2, 64))
		}
		args := []string{"tap", "-x", pos[0], "-y", pos[1], "--hold", "1"}
		if extend {
			args = []string{"swipe", "--start-x", pos[0], "--start-y", pos[1], "--end-x", pos[2], "--end-y", pos[3], "--duration", "1", "--delta", "2"}
		}
		if err := exec.CommandContext(r.Context(), "axe", append(args, "--udid", udid)...).Run(); err != nil {
			http.Error(w, "touch delivery failed", 500)
			return
		}
		fmt.Fprint(w, "delivered")
	}
	mux.HandleFunc(path+"POINTERS", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(filepath.Join(root, "keys"))
		if err != nil {
			http.Error(w, "pointer log unavailable", 503)
			return
		}
		fmt.Fprint(w, len(pointerPress.FindAllIndex(data, -1)))
	})
	mux.HandleFunc(path+"WORD", touch)
	mux.HandleFunc(path+"EXTEND", touch)
	mux.HandleFunc(path+"HID", func(w http.ResponseWriter, r *http.Request) {
		if err := exec.CommandContext(r.Context(), "axe", "key-combo", "--modifiers", "224", "--key", "7", "--udid", udid).Run(); err != nil {
			http.Error(w, "keyboard delivery failed", 500)
			return
		}
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			data, _ := os.ReadFile(filepath.Join(root, "keys"))
			if bytes.Contains(data, []byte{4}) {
				fmt.Fprint(w, "delivered")
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		http.Error(w, "Control-D missing at child", 409)
	})
	return nil
}

// Read the settled screen and physical hinge angle before a capture or recovery.
func duoDisplay(ctx context.Context, udid string) (uint32, float64, error) {
	data, err := exec.CommandContext(ctx, "axe", "--output", "json", "displays", "--udid", udid).Output()
	if err != nil {
		return 0, 0, err
	}
	var response struct {
		Data struct {
			Result struct {
				ActiveScreen struct{ ID uint32 }
				Hinge        *struct{ Degrees float64 }
			}
		}
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return 0, 0, err
	}
	display := response.Data.Result
	if display.Hinge == nil || display.ActiveScreen.ID == 0 {
		return 0, 0, fmt.Errorf("missing hinge or active display")
	}
	return display.ActiveScreen.ID, display.Hinge.Degrees, nil
}

func poseRoute(mux *http.ServeMux, path, udid string) {
	poses := map[string]int{"Closed": 0, "Partially Open": 90, "Open": 180}
	// The partially open display can settle several degrees away from the
	// mechanical target. Keep the end stops precise and check the tabletop pose.
	matchesPose := func(actual float64, target int) bool {
		if target == 90 {
			return actual >= 80 && actual <= 100
		}
		return math.Abs(actual-float64(target)) <= 0.5
	}
	settledDisplay := func(ctx context.Context, target int) (uint32, float64, error) {
		wait, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		for {
			id, angle, err := duoDisplay(wait, udid)
			if err == nil && matchesPose(angle, target) {
				return id, angle, nil
			}
			select {
			case <-time.After(time.Second):
			case <-wait.Done():
				return 0, 0, fmt.Errorf("active screen did not settle at %d degrees: %w", target, wait.Err())
			}
		}
	}
	mux.HandleFunc(path+"pose/", func(w http.ResponseWriter, r *http.Request) {
		degrees, ok := poses[strings.TrimPrefix(r.URL.Path, path+"pose/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		for attempt := 0; attempt < 2; attempt++ {
			// The movement receipt confirms the mechanical angle. XCTest waits for
			// the app's live layout; screenshots independently resolve the active display.
			cmd := exec.CommandContext(r.Context(), "axe", "hinge", "set", strconv.Itoa(degrees), "--udid", udid, "--timeout", "15", "--no-wait")
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err == nil {
				fmt.Fprint(w, "applied")
				return
			}
			// AXe can lose its input receipt during a panel transition. Read the actual
			// angle before deciding whether a second movement is necessary.
			if _, _, err := settledDisplay(r.Context(), degrees); err == nil {
				fmt.Fprint(w, "applied")
				return
			}
		}
		http.Error(w, "simulator hinge did not settle", http.StatusServiceUnavailable)
	})
	if os.Getenv("TEST_RUNNER_CLISTREAM_CAPTURE_GALLERY") != "1" {
		return
	}
	mux.HandleFunc(path+"pose-screenshot/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, path+"pose-screenshot/")
		degrees, ok := poses[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if err := os.MkdirAll("gallery", 0755); err != nil {
			http.Error(w, "gallery unavailable", http.StatusInternalServerError)
			return
		}
		filename := "duo-" + strings.ToLower(strings.ReplaceAll(name, " ", "-")) + ".png"
		id, angle, err := settledDisplay(r.Context(), degrees)
		if err != nil {
			http.Error(w, "requested display is not active", http.StatusConflict)
			return
		}
		cmd := exec.CommandContext(r.Context(), "xcrun", "simctl", "io", udid, "screenshot", fmt.Sprintf("--display=%d", id), filepath.Join("gallery", filename))
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			http.Error(w, "active display capture failed", http.StatusServiceUnavailable)
			return
		}
		fmt.Printf("Captured %s at %.1f degrees on display %d\n", filename, angle, id)
		fmt.Fprint(w, "captured")
	})
}
