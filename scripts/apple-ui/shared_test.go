package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func TestSharedViewerRecoversOnlyBeforeAnyNativeTestStarted(t *testing.T) {
	const disconnected = "Failed to establish communication with the test runner. (Underlying Error: Channel disconnected)"
	for _, tc := range []struct {
		name, viewer, host, message string
		total, attempts             int
	}{
		{"first runner terminated", "pad", "github-hosted", "Test crashed with signal term while preparing to run tests.", 0, 2},
		{"assertion ran", "pad", "github-hosted", "Test crashed with signal term while preparing to run tests.", 1, 1},
		{"other initialization error", "pad", "github-hosted", "Timed out waiting for a child echo", 0, 1},
		{"phone other failure", "phone", "github-hosted", "Test crashed with signal term while preparing to run tests.", 0, 1},
		{"phone never connected", "phone", "github-hosted", disconnected, 0, 2},
		{"phone case started", "phone", "github-hosted", disconnected, 1, 1},
		{"phone local process", "phone", "", disconnected, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("RUNNER_ENVIRONMENT", tc.host)
			t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
			t.Setenv("MESSAGE", tc.message)
			t.Setenv("TOTAL", strconv.Itoa(tc.total))
			if err := os.MkdirAll("apple/build", 0700); err != nil {
				t.Fatal(err)
			}
			for path, contents := range map[string]string{
				"xcodebuild": `#!/bin/sh
if [ -f calls ]; then echo second >> calls; exit 0; fi
echo first > calls
while [ "$1" != "-resultBundlePath" ]; do shift; done
mkdir "$2"
echo "$MESSAGE" >&2
exit 65
`,
				"xcrun": `#!/bin/sh
case "$1" in
  xcresulttool) printf '{"totalTestCount":%s}\n' "$TOTAL" ;;
  simctl) [ "$*" = 'simctl shutdown tablet' ] ;;
  *) exit 1 ;;
esac
`,
				"apple/boot-simulator.sh": "#!/bin/sh\n[ \"$1\" = tablet ]\n",
			} {
				if err := os.WriteFile(path, []byte(contents), 0700); err != nil {
					t.Fatal(err)
				}
			}
			err := testSharedViewer(t.Context(), tc.viewer, "tablet", "plan", "destination", "suite", os.Environ())
			if (err == nil) != (tc.attempts == 2) {
				t.Fatalf("unexpected native result: %v", err)
			}
			calls, err := os.ReadFile("calls")
			if err != nil {
				t.Fatal(err)
			}
			if len(strings.Fields(string(calls))) != tc.attempts {
				t.Fatalf("unexpected calls %q", calls)
			}
		})
	}
}
