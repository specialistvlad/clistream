package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestSharedCommandCancelsDescendantHoldingDiagnosticPipe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := sharedCommand(ctx, "/bin/sh", "-c", "sleep 30 & wait").CombinedOutput()
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("child did not reach the deadline: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("wrapper was killed but its descendant held the diagnostic pipe for %s", elapsed)
	}
}

func TestSharedPadUsesTheVisibleTerminalAndItsKeyboard(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("INPUT_LOG", filepath.Join(dir, "input"))
	t.Setenv("AX_SCREEN", `{"data":{"result":[{"type":"Application","children":[
{"AXUniqueId":"workspace-confirm","AXLabel":"Pair device"},
{"AXUniqueId":"session-terminal","AXValue":"preview-ready\nout:shared-cli\nout:shared-mac\nout:shared-phone\nout:shared-pad\nout:independent-phone"}]}]}}`)
	for name, stub := range map[string]string{
		"xcrun": "#!/bin/sh\nexit 0\n",
		"axe": `#!/bin/sh
case "$*" in
  *describe-ui*) printf '%s\n' "$AX_SCREEN" ;;
  *'type --stdin'*) IFS= read -r link || :; [ "$link" = 'clistream://example' ] || exit 1; echo "$*" >> "$INPUT_LOG" ;;
  *) echo "$*" >> "$INPUT_LOG" ;;
esac
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(stub), 0700); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var routes []string
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		routes = append(routes, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/QR" {
			fmt.Fprint(w, "clistream://example")
			return
		}
		fmt.Fprint(w, "ready")
	}))
	defer fixture.Close()
	if err := driveSharedPad(t.Context(), "tablet", "App", fixture.URL+"/", "child"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(routes, " "); got != "/QR /ready/pad /switch/pad /return/pad" {
		t.Fatalf("barriers: %s", got)
	}
	raw, err := os.ReadFile(os.Getenv("INPUT_LOG"))
	if err != nil {
		t.Fatal(err)
	}
	const gestures = "tap --id workspace-add-server\ntap --id pairing-link\ntype --stdin --layout us\ntap --label Pair device\ntap --id session-child\ntap --id session-terminal\ntype shared-pad\nkey 40"
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := strings.Split(gestures, "\n")
	if len(lines) != len(want) {
		t.Fatalf("input calls: %v", lines)
	}
	for i, call := range lines {
		if !strings.HasPrefix(call, want[i]+" ") || !strings.HasSuffix(call, "--udid tablet") {
			t.Errorf("gesture %d: %s", i, call)
		}
	}
}

func TestTabletScreenReadsOnlyVisibleTerminalEcho(t *testing.T) {
	raw := []byte(`{"data":{"result":{"children":[
{"type":"Text","AXValue":"out:shared-phone"},
{"hidden":true,"AXUniqueId":"session-terminal","AXValue":"out:shared-pad"},
{"AXUniqueId":"session-terminal","AXValue":"out:shared-cli"}
]}}}`)
	text, err := readTabletTerminal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if text != "out:shared-cli" {
		t.Fatalf("unexpected selected terminal text: %s", text)
	}
}

func TestSharedPhoneResumesOnlyAfterInterruptedHostedBoot(t *testing.T) {
	for _, tc := range []struct {
		name, runner               string
		firstCode, lastCode, calls int
		interrupted, ok            bool
	}{
		{"already booted", "github-hosted", 0, 0, 1, false, true},
		{"service disconnected", "github-hosted", int(syscall.ECONNABORTED), 0, 2, true, true},
		{"service remains down", "github-hosted", int(syscall.ECONNABORTED), int(syscall.ECONNABORTED), 2, true, false},
		{"other connection aborted", "github-hosted", int(syscall.ECONNABORTED), 0, 1, false, false},
		{"invalid simulator", "github-hosted", 2, 0, 1, false, false},
		{"other runner", "", int(syscall.ECONNABORTED), 0, 1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PATH", dir)
			t.Setenv("RUNNER_ENVIRONMENT", tc.runner)
			t.Setenv("BOOT_CALLS", filepath.Join(dir, "calls"))
			t.Setenv("FIRST_CODE", strconv.Itoa(tc.firstCode))
			t.Setenv("LAST_CODE", strconv.Itoa(tc.lastCode))
			t.Setenv("INTERRUPTED", strconv.FormatBool(tc.interrupted))
			const stub = `#!/bin/sh
[ "$*" = 'simctl bootstatus prepared-id -b' ] || exit 1
if [ "$INTERRUPTED" = true ]; then echo "CoreSimulatorService connection interrupted." >&2; fi
if [ -f "$BOOT_CALLS" ]; then echo boot >> "$BOOT_CALLS"; exit "$LAST_CODE"; fi
echo boot > "$BOOT_CALLS"
exit "$FIRST_CODE"
`
			if err := os.WriteFile(filepath.Join(dir, "xcrun"), []byte(stub), 0700); err != nil {
				t.Fatal(err)
			}
			err := bootSharedPhone(t.Context(), "prepared-id")
			if (err == nil) != tc.ok {
				t.Fatalf("unexpected boot result: %v", err)
			}
			raw, err := os.ReadFile(os.Getenv("BOOT_CALLS"))
			if err != nil {
				t.Fatal(err)
			}
			if got := len(strings.Fields(string(raw))); got != tc.calls {
				t.Fatalf("expected %d boot attempts, got %d", tc.calls, got)
			}
		})
	}
}
