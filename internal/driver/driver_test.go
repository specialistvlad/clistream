package driver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutomaticModelCommandMode(t *testing.T) {
	for _, tc := range []struct {
		name, arg, mode, want string
	}{
		{"codex", "exec", "auto", "pipe"},
		{"codex", "exec", "pty", "pty"},
		{"claude", "--print", "auto", "pipe"},
		{"claude", "-p", "auto", "pipe"},
		{"claude", "interactive prompt", "auto", "pty"},
	} {
		t.Run(tc.name+" "+tc.arg+" "+tc.mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name)
			// The driver's version probe is independent of provider credentials.
			if err := os.WriteFile(path, []byte("#!/bin/sh\necho '"+catalog[tc.name][0].Banner+"'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			profile, err := Select([]string{path, tc.arg}, "auto", tc.mode, true)
			if err != nil || profile.Mode != tc.want {
				t.Fatalf("mode = %q, error = %v, want %q", profile.Mode, err, tc.want)
			}
		})
	}
}
