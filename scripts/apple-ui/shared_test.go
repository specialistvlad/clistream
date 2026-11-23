package main

import (
	"context"
	"errors"
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
