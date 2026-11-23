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

func TestPreparedPhoneResumesOnlyAfterInterruptedHostedBoot(t *testing.T) {
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
			err := bootPreparedPhone(t.Context(), "prepared-id")
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
