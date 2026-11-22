package main

import (
	"bytes"
	"fmt"
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

func poseRoute(mux *http.ServeMux, path, device, plan string) {
	mux.HandleFunc(path+"pose/", func(w http.ResponseWriter, r *http.Request) {
		pose := strings.TrimPrefix(r.URL.Path, path+"pose/")
		if pose != "Closed" && pose != "Open" && pose != "Partially Open" {
			http.NotFound(w, r)
			return
		}
		developer, err := exec.CommandContext(r.Context(), "xcode-select", "-p").Output()
		if err != nil {
			http.Error(w, "Xcode developer directory unavailable", 503)
			return
		}
		resultDir, err := os.MkdirTemp("apple/build", "results-DuoPose-")
		if err != nil {
			http.Error(w, "Device Hub result directory unavailable", 503)
			return
		}
		hub := filepath.Join(strings.TrimSpace(string(developer)), "..", "Applications", "DeviceHub.app")
		cmd := exec.CommandContext(r.Context(), "xcodebuild", "-quiet", "-xctestrun", plan, "-destination", "platform=macOS", "-resultBundlePath", filepath.Join(resultDir, "pose.xcresult"), "-collect-test-diagnostics", "never", "-parallel-testing-enabled", "NO", "-only-testing:CLIStream-MacUITests/DuoPoseTests/testSetDevicePose", "test-without-building")
		cmd.Env = append(os.Environ(), "TEST_RUNNER_CLISTREAM_DEVICE_HUB="+hub, "TEST_RUNNER_CLISTREAM_DUO_POSE="+pose, "TEST_RUNNER_CLISTREAM_DUO_NAME="+device)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			http.Error(w, "Device Hub pose change failed", 503)
			return
		}
		fmt.Fprint(w, "applied")
	})
}
