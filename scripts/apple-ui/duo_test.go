package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDuoMenuVerifiesScreenAndUnsentInputBeforePointTap(t *testing.T) {
	dir := t.TempDir()
	axe := `#!/bin/sh
echo "$*" >> "$DUO_TEST_CALLS"
case "$*" in
  *describe-ui*) printf '{"data":{"result":%s}}' "$DUO_TEST_TREE" ;;
  *--verbose*) printf '{"error":{"code":"timeout"},"data":{"result":{"enqueuedEventCount":%s,"cleanup":"notNeeded"}}}' "$DUO_TEST_SENT"; exit 1 ;;
  *"-x 25 -y 36"*) exit 0 ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "axe"), []byte(axe), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	item := `{"AXLabel":"Left Arrow","frame":{"x":10,"y":20,"width":30,"height":32}}`
	other := `{"AXLabel":"Left Arrow","frame":{"x":50,"y":100,"width":30,"height":32}}`
	for _, tc := range []struct {
		name, tree, sent string
		wantTap          bool
	}{
		{"visible button", "[" + item + "]", "0", true},
		{"ambiguous visible controls", "[" + item + "," + other + "]", "0", false},
		{"input already enqueued", "[" + item + "]", "1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := filepath.Join(t.TempDir(), "calls")
			t.Setenv("DUO_TEST_CALLS", calls)
			t.Setenv("DUO_TEST_TREE", tc.tree)
			t.Setenv("DUO_TEST_SENT", tc.sent)
			err := tapDuoControl(context.Background(), "sim", duoScreen{ID: 3, UUID: "panel", Width: 951, Height: 669}, "Left Arrow")
			if (err == nil) != tc.wantTap {
				t.Fatalf("unexpected menu result: %v", err)
			}
			raw, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			tapped := strings.Contains(string(raw), "tap -x 25 -y 36 --input-mode selected --screen-id 3 --screen-uuid panel")
			if tapped != tc.wantTap {
				t.Fatalf("physical panel input = %t, want %t", tapped, tc.wantTap)
			}
		})
	}
}
