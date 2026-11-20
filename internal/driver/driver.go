// Package driver separates CLI-specific version policy from process I/O.
package driver

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Profile struct{ Name, Mode, Version string }

// Release describes the I/O capabilities verified for a particular executable.
// An empty version designates the generic driver, which never probes the program.
type Release struct {
	Version     string   `json:"version,omitempty"`
	Banner      string   `json:"-"`
	DefaultMode string   `json:"defaultMode"`
	Modes       []string `json:"modes"`
}

var catalog = map[string][]Release{
	"generic": {{DefaultMode: "pipe", Modes: []string{"pipe", "pty"}}},
	"claude":  {{Version: "2.1.329", Banner: "2.1.329 (Claude Code)", DefaultMode: "pty", Modes: []string{"pty", "pipe"}}},
	"codex":   {{Version: "0.170.0", Banner: "codex-cli 0.170.0", DefaultMode: "pty", Modes: []string{"pty", "pipe"}}},
}

func Catalog() map[string][]Release {
	result := make(map[string][]Release, len(catalog))
	for name, releases := range catalog {
		result[name] = slices.Clone(releases)
		for i, release := range releases {
			result[name][i].Modes = slices.Clone(release.Modes)
		}
	}
	return result
}

func detect(path, name string, releases []Release) (Release, error) {
	if name == "generic" {
		return releases[0], nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, "--version").Output()
	banner := strings.TrimSpace(string(output))
	var versions []string
	for _, release := range releases {
		if err == nil && banner == release.Banner {
			return release, nil
		}
		versions = append(versions, release.Version)
	}
	return Release{}, fmt.Errorf("%s driver supports %s; use -driver generic for other versions", name, strings.Join(versions, ", "))
}

func Select(path, name, mode string) (Profile, error) {
	if name == "auto" {
		name = filepath.Base(path)
		if _, ok := catalog[name]; !ok {
			name = "generic"
		}
	}
	releases, ok := catalog[name]
	if !ok {
		return Profile{}, fmt.Errorf("unknown driver %q", name)
	}
	release, err := detect(path, name, releases)
	if err != nil {
		return Profile{}, err
	}
	if mode == "auto" {
		mode = release.DefaultMode
	}
	if !slices.Contains(release.Modes, mode) {
		return Profile{}, fmt.Errorf("unsupported mode %q for %s driver", mode, name)
	}
	return Profile{name, mode, release.Version}, nil
}
