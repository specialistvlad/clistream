package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSimulatorClipboardVerifiesTheRetainedInvitation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("SIM_CLIPBOARD", filepath.Join(dir, "pasteboard"))
	stub := `#!/bin/sh
case "$2" in
  pbcopy) cat > "$SIM_CLIPBOARD" ;;
  pbpaste)
    if [ ! -f "$SIM_CLIPBOARD.ready" ]; then touch "$SIM_CLIPBOARD.ready"; exit 1; fi
    if [ -n "$SIM_WRONG_TEXT" ]; then echo other; else cat "$SIM_CLIPBOARD"; printf '\n'; fi ;;
  *) exit 1 ;;
esac`
	if err := os.WriteFile(filepath.Join(dir, "xcrun"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	if err := setSimulatorClipboard(t.Context(), "device", []byte("invitation")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SIM_WRONG_TEXT", "1")
	if err := setSimulatorClipboard(t.Context(), "device", []byte("invitation")); err == nil {
		t.Fatal("accepted different clipboard text")
	}
}

func TestPreparePipeWaitsForTheStartedRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	var calls []string
	err := preparePipe(ctx, "child", func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		switch len(calls) {
		case 1:
			return nil, errors.New("brief discovery outage")
		case 2, 4:
			return []byte(`[{"ID":"child","Run":""}]`), nil
		case 3:
			return nil, nil
		default:
			return []byte(`[{"ID":"child","Run":"running"}]`), nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"list", "list", "child start", "list", "list"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("expected one start followed by discovery, got %v", calls)
	}
}

func TestPreparePipeHonorsRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	err := preparePipe(ctx, "child", func(query context.Context, args ...string) ([]byte, error) {
		cancel()
		<-query.Done()
		return nil, query.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled discovery, got %v", err)
	}
}
