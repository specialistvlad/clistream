package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	// The PTY child records actual bytes. Report counts only for known key sequences.
	keys := map[string]string{"Up Arrow": "\x1b[A", "Down Arrow": "\x1b[B", "Right Arrow": "\x1b[C", "Left Arrow": "\x1b[D",
		"Escape": "\x1b", "Control-C": "\x03", "Tab": "\t", "tilde": "~", "pipe": "|", "slash": "/", "dash": "-",
		"F1": "\x1bOP", "F2": "\x1bOQ", "F3": "\x1bOR", "F4": "\x1bOS", "F5": "\x1b[15~", "F6": "\x1b[17~",
		"F7": "\x1b[18~", "F8": "\x1b[19~"}
	mux.HandleFunc(path+"key-count/", func(w http.ResponseWriter, r *http.Request) {
		sequence, ok := keys[strings.TrimPrefix(r.URL.Path, path+"key-count/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile(filepath.Join(root, "keys"))
		if err != nil {
			http.Error(w, "PTY unavailable", 503)
			return
		}
		fmt.Fprint(w, bytes.Count(data, []byte(sequence)))
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
		keys := filepath.Join(root, "keys")
		before, err := os.ReadFile(keys)
		if err != nil {
			http.Error(w, "terminal input unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := exec.CommandContext(r.Context(), "axe", "key-combo", "--modifiers", "224", "--key", "7", "--udid", udid).Run(); err != nil {
			http.Error(w, "keyboard delivery failed", 500)
			return
		}
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			data, _ := os.ReadFile(keys)
			if len(data) > len(before) && bytes.Contains(data[len(before):], []byte{4}) {
				fmt.Fprint(w, "delivered")
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		http.Error(w, "Control-D missing at child", 409)
	})
	return nil
}

type duoScreen struct {
	ID            uint32
	UUID          string
	Role, State   string
	Width, Height float64
}

func captureDuoScreen(ctx context.Context, udid string, panel duoScreen, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return exec.CommandContext(ctx, "xcrun", "simctl", "io", udid, "screenshot",
		fmt.Sprintf("--display=%d", panel.ID), path).Run()
}

// The system keeps Session in the same toolbar rail when the keyboard closes.
// The next native Keys lookup independently verifies a repeat tap there.
type duoToolbarButton struct {
	panel   duoScreen
	degrees float64
	point   [2]int
}

var errDuoKeyboardInputEnqueued = errors.New("keyboard input enqueued, confirm its visible state")

// A menu may be fully drawn before AXe's label tap accepts its hit-test
// receipt. If it certifies that no input was sent, locate the actual visible
// control again and touch its center on the same physical panel.
func (toolbar *duoToolbarButton) tapControl(ctx context.Context, udid string, panel duoScreen, degrees float64, label string) error {
	target := []string{"--input-mode", "selected", "--screen-id", strconv.FormatUint(uint64(panel.ID), 10), "--screen-uuid", panel.UUID, "--udid", udid}
	tap := func(p [2]int) error {
		return exec.CommandContext(ctx, "axe", append([]string{"tap", "-x", strconv.Itoa(p[0]), "-y", strconv.Itoa(p[1])}, target...)...).Run()
	}
	if label == "Session" && toolbar.panel == panel && toolbar.degrees == degrees {
		return tap(toolbar.point)
	}
	remember := func(p [2]int) {
		if label == "Session" && p[0] > 0 && p[1] > 0 && float64(p[0]) < panel.Width && float64(p[1]) < panel.Height {
			*toolbar = duoToolbarButton{panel: panel, degrees: degrees, point: p}
		}
	}
	raw, err := exec.CommandContext(ctx, "axe", append([]string{"--output", "json", "tap", "--verbose", "--label", label, "--wait-timeout", "12"}, target...)...).Output()
	var receipt struct {
		Error struct{ Code string }
		Data  struct {
			Result struct {
				EnqueuedEventCount *int
				Cleanup            string
				AX                 struct{ Point *struct{ X, Y float64 } }
			}
		}
	}
	if err == nil {
		if label == "Session" && json.Unmarshal(raw, &receipt) == nil {
			if p := receipt.Data.Result.AX.Point; p != nil {
				remember([2]int{int(math.Round(p.X)), int(math.Round(p.Y))})
			}
		}
		return nil
	}
	if json.Unmarshal(raw, &receipt) != nil {
		return err
	}
	outcome := receipt.Data.Result
	fmt.Printf("Duo label %s: AXe code %s, input cleanup %s\n", label, receipt.Error.Code, outcome.Cleanup)
	if (label == "Hide Keyboard" || label == "Show Keyboard") && receipt.Error.Code == "inputFailed" &&
		outcome.EnqueuedEventCount != nil && *outcome.EnqueuedEventCount > 0 && outcome.Cleanup == "notNeeded" {
		return errDuoKeyboardInputEnqueued
	}
	if outcome.EnqueuedEventCount == nil || *outcome.EnqueuedEventCount != 0 || outcome.Cleanup == "" || outcome.Cleanup == "uncertain" {
		return err
	}
	type frame struct{ X, Y, Width, Height float64 }
	type node struct {
		Label    string `json:"AXLabel"`
		Frame    frame
		Hidden   bool
		Enabled  *bool
		Children []node
	}
	var tree struct{ Data struct{ Result []node } }
	raw, err = exec.CommandContext(ctx, "axe", append([]string{"--output", "json", "describe-ui"}, target...)...).Output()
	if err != nil || json.Unmarshal(raw, &tree) != nil {
		return fmt.Errorf("cannot read active menu")
	}
	points := map[[2]int]bool{}
	var point [2]int
	var visit func(node)
	visit = func(item node) {
		f := item.Frame
		if !item.Hidden && (item.Enabled == nil || *item.Enabled) && item.Label == label && f.Width > 0 && f.Height > 0 {
			x, y := f.X+f.Width/2, f.Y+f.Height/2
			if x > 0 && y > 0 && x < panel.Width && y < panel.Height {
				point = [2]int{int(math.Round(x)), int(math.Round(y))}
				points[point] = true
			}
		}
		for _, child := range item.Children {
			visit(child)
		}
	}
	for _, root := range tree.Data.Result {
		visit(root)
	}
	if len(points) != 1 {
		return fmt.Errorf("active display has %d distinct visible centers for %s", len(points), label)
	}
	if err := tap(point); err != nil {
		return err
	}
	remember(point)
	return nil
}

// AXe reports the lit panel's role and oriented size in points.
func duoDisplay(ctx context.Context, udid string) (duoScreen, float64, error) {
	data, err := exec.CommandContext(ctx, "axe", "--output", "json", "displays", "--udid", udid).Output()
	if err != nil {
		return duoScreen{}, 0, err
	}
	var response struct {
		Data struct {
			Result struct {
				ActiveScreen duoScreen
				Hinge        *struct{ Degrees float64 }
			}
		}
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return duoScreen{}, 0, err
	}
	display := response.Data.Result
	if display.Hinge == nil || display.ActiveScreen.ID == 0 {
		return duoScreen{}, 0, fmt.Errorf("missing hinge or active display")
	}
	return display.ActiveScreen, display.Hinge.Degrees, nil
}

func poseRoute(mux *http.ServeMux, path, udid, keysFile string) {
	poses := map[string]int{"Closed": 0, "Partially Open": 90, "Open": 180}
	// The partially open display can settle several degrees away from the
	// mechanical target. Keep the end stops precise and check the tabletop pose.
	matchesPose := func(actual float64, target int) bool {
		if target == 90 {
			return actual >= 80 && actual <= 100
		}
		return math.Abs(actual-float64(target)) <= 0.5
	}
	settledDisplay := func(ctx context.Context, target int, limit time.Duration) (duoScreen, float64, error) {
		wait, cancel := context.WithTimeout(ctx, limit)
		defer cancel()
		role := "inner"
		if target == 0 {
			role = "outer"
		}
		lastMeasurement := "no completed display reading"
		for {
			panel, angle, err := duoDisplay(wait, udid)
			if err == nil {
				if matchesPose(angle, target) && panel.Role == role && panel.Width > 0 && panel.Height > 0 {
					return panel, angle, nil
				}
				lastMeasurement = fmt.Sprintf("%.1f degrees, %s %s display %d, %.0fx%.0f points", angle, panel.Role, panel.State, panel.ID, panel.Width, panel.Height)
			}
			select {
			case <-time.After(time.Second):
			case <-wait.Done():
				fmt.Printf("Duo %s panel did not settle at %d degrees. Last measurement: %s (last read error %v)\n", role, target, lastMeasurement, err)
				return duoScreen{}, 0, wait.Err()
			}
		}
	}
	mux.HandleFunc(path+"pose/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, path+"pose/")
		scenario, pose, staged := strings.Cut(name, "/")
		if staged {
			if scenario != "fold" && scenario != "input" {
				http.NotFound(w, r)
				return
			}
			name = pose
		}
		degrees, ok := poses[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		for attempt := 0; attempt < 2; attempt++ {
			// A movement receipt can arrive while the previous panel still owns input.
			cmd := exec.CommandContext(r.Context(), "axe", "hinge", "set", strconv.Itoa(degrees), "--udid", udid, "--timeout", "15", "--no-wait")
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			moveErr := cmd.Run()
			wait := time.Minute
			if moveErr == nil && attempt == 0 {
				// The physical hinge acknowledged this movement. Give panel migration
				// the remaining observation budget without sending another movement.
				wait = 2 * time.Minute
			}
			panel, angle, err := settledDisplay(r.Context(), degrees, wait)
			if err != nil {
				if moveErr == nil || r.Context().Err() != nil {
					break
				}
				continue
			}
			fmt.Printf("Duo %s at %.1f degrees: %s %s, display %d, %.0fx%.0f points\n", name, angle, panel.Role, panel.State, panel.ID, panel.Width, panel.Height)
			if !staged {
				fmt.Fprint(w, "applied")
				return
			}
			if os.Getenv("TEST_RUNNER_CLISTREAM_CAPTURE_GALLERY") == "1" {
				filename := "duo-" + scenario + "-" + strings.ToLower(strings.ReplaceAll(name, " ", "-")) + "-active.png"
				if err := captureDuoScreen(r.Context(), udid, panel, filepath.Join("results", filename)); err != nil {
					http.Error(w, "active display capture failed", http.StatusServiceUnavailable)
					return
				}
			}
			_ = json.NewEncoder(w).Encode(panel)
			return
		}
		http.Error(w, "simulator hinge did not settle", http.StatusServiceUnavailable)
	})
	var menuMu sync.Mutex
	var toolbar duoToolbarButton
	mux.HandleFunc(path+"duo-key/", func(w http.ResponseWriter, r *http.Request) {
		menuMu.Lock()
		defer menuMu.Unlock()
		role, key, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, path+"duo-key/"), "/")
		if !ok || (role != "outer" && role != "inner") {
			http.NotFound(w, r)
			return
		}
		controls := []string{"Session", "Keys"}
		switch key {
		case "Control-C":
			controls = append(controls, "Control Key")
		case "Up Arrow", "Down Arrow", "Right Arrow", "Left Arrow", "Escape", "Show Keyboard", "Hide Keyboard":
		default:
			http.NotFound(w, r)
			return
		}
		controls = append(controls, key)
		want := map[string]string{"Up Arrow": "\x1b[A", "Down Arrow": "\x1b[B", "Right Arrow": "\x1b[C", "Left Arrow": "\x1b[D", "Escape": "\x1b", "Control-C": "\x03"}[key]
		before, err := os.Stat(keysFile)
		if err != nil {
			http.Error(w, "child keyboard receipt unavailable", http.StatusServiceUnavailable)
			return
		}
		panel, degrees, err := duoDisplay(r.Context(), udid)
		if err != nil || panel.Role != role || panel.UUID == "" {
			http.Error(w, "requested display unavailable", http.StatusServiceUnavailable)
			return
		}
		// Touch each real menu item on the same measured panel even as UIKit
		// animates from the Session menu to its nested keyboard commands.
		for _, label := range controls {
			err := toolbar.tapControl(r.Context(), udid, panel, degrees, label)
			if err == errDuoKeyboardInputEnqueued && label == key {
				fmt.Printf("Duo sent %s on display %d. Checking keyboard visibility in XCTest.\n", key, panel.ID)
				fmt.Fprint(w, "input enqueued")
				return
			}
			if label == key && want != "" {
				received := false
				for deadline := time.Now().Add(5 * time.Second); r.Context().Err() == nil && time.Now().Before(deadline); {
					data, _ := os.ReadFile(keysFile)
					if int64(len(data)) >= before.Size() && string(data[before.Size():]) == want {
						received = true
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
				if received {
					fmt.Printf("Duo child received %s on %s\n", key, role)
					err = nil
				} else if err == nil {
					err = fmt.Errorf("the PTY did not receive %s", key)
				}
			}
			if err != nil || (key == "Hide Keyboard" && os.Getenv("TEST_RUNNER_CLISTREAM_CAPTURE_GALLERY") == "1") {
				name := "duo-" + role + "-menu-" + strings.ToLower(strings.ReplaceAll(label, " ", "-")) + ".png"
				if e := captureDuoScreen(r.Context(), udid, panel, filepath.Join("results", name)); e != nil {
					http.Error(w, "active display capture failed", http.StatusServiceUnavailable)
					return
				}
			}
			if err != nil {
				fmt.Printf("Duo could not tap %s on display %d: %v\n", label, panel.ID, err)
				http.Error(w, "active display control unavailable", http.StatusConflict)
				return
			}
			fmt.Printf("Duo tapped %s on display %d\n", label, panel.ID)
		}
		fmt.Fprint(w, "delivered")
	})
	if os.Getenv("TEST_RUNNER_CLISTREAM_CAPTURE_GALLERY") != "1" {
		return
	}
	mux.HandleFunc(path+"panel-capture/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, path+"panel-capture/")
		board := strings.TrimSuffix(name, "-dark")
		role := "inner"
		switch board {
		case "duo-outer-list", "duo-outer-terminal":
			role = "outer"
		case "duo-inner", "duo-inner-ports", "duo-partially-open":
		default:
			http.NotFound(w, r)
			return
		}
		panel, _, err := duoDisplay(r.Context(), udid)
		if err != nil || panel.Role != role {
			http.Error(w, "requested display unavailable", 409)
			return
		}
		if err := captureDuoScreen(r.Context(), udid, panel, filepath.Join("gallery", name+".png")); err != nil {
			http.Error(w, "active display capture failed", 503)
			return
		}
		fmt.Fprint(w, "captured")
	})
	mux.HandleFunc(path+"pose-screenshot/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, path+"pose-screenshot/")
		degrees, ok := poses[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		filename := "duo-" + strings.ToLower(strings.ReplaceAll(name, " ", "-")) + ".png"
		panel, angle, err := settledDisplay(r.Context(), degrees, time.Minute)
		if err != nil {
			http.Error(w, "requested display is not active", http.StatusConflict)
			return
		}
		if err := captureDuoScreen(r.Context(), udid, panel, filepath.Join("gallery", filename)); err != nil {
			http.Error(w, "active display capture failed", http.StatusServiceUnavailable)
			return
		}
		fmt.Printf("Captured %s at %.1f degrees on display %d\n", filename, angle, panel.ID)
		fmt.Fprint(w, "captured")
	})
}
