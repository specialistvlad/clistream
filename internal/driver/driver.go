// Package driver separates CLI-specific version policy from terminal mechanics.
package driver

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Profile struct{ Name, Mode, Version string }
type spec struct{ version, banner string }

var supported = map[string]spec{"claude": {"2.1.329", "2.1.329 (Claude Code)"}, "codex": {"0.170.0", "codex-cli 0.170.0"}}

func Select(path, name, mode string) (Profile, error) {
	if name == "auto" {
		name = filepath.Base(path)
		if _, ok := supported[name]; !ok {
			name = "generic"
		}
	}
	p := Profile{Name: name, Mode: mode}
	if name != "generic" {
		s, ok := supported[name]
		if !ok {
			return p, fmt.Errorf("unknown driver %q", name)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, e := exec.CommandContext(ctx, path, "--version").Output()
		if e != nil || strings.TrimSpace(string(out)) != s.banner {
			return p, fmt.Errorf("%s driver supports %s; use -driver generic for other versions", name, s.version)
		}
		p.Version = s.version
	}
	if mode == "auto" {
		p.Mode = "pipe"
		if name != "generic" {
			p.Mode = "pty"
		}
	}
	if p.Mode != "pty" && p.Mode != "pipe" {
		return p, fmt.Errorf("unknown mode %q", p.Mode)
	}
	return p, nil
}
