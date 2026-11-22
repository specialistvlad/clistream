package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Drive the shipping mobile app over its accessibility interface. Both apps stay
// attached without keeping an additional XCTest runner resident in each simulator.
func mobileViewer(ctx context.Context, name, udid, fixture, pipeID string, invitation []byte) (result error) {
	started := time.Now()
	defer func() {
		status := "Passed"
		if result != nil {
			status = "Failed"
			result = fmt.Errorf("%s: %w", name, result)
		}
		dir := "apple/build/shared-mobile"
		record := map[string]string{"test": "SharedViewer/" + name, "result": status, "duration": time.Since(started).Round(time.Second).String()}
		data, err := json.Marshal(map[string][]map[string]string{"cases": {record}})
		if err == nil {
			err = os.MkdirAll(dir, 0755)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, name+".json"), data, 0644)
		}
		if result == nil {
			result = err
		}
	}()
	run := func(args ...string) error {
		return exec.CommandContext(ctx, "axe", append(args, "--udid", udid)...).Run()
	}
	tap := func(selector, text string) error {
		return run("tap", selector, text, "--wait-timeout", "30")
	}
	barrier := func(stage string) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fixture+stage+"/"+name, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("%s barrier unavailable", stage)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s barrier returned %d", stage, resp.StatusCode)
		}
		return nil
	}
	check := func(marker string) error {
		wait, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		for wait.Err() == nil {
			tree, err := exec.CommandContext(wait, "axe", "describe-ui", "--minimal-keys", "--udid", udid).Output()
			if err == nil && bytes.Contains(tree, []byte("out:"+marker)) {
				return nil
			}
			select {
			case <-time.After(time.Second):
			case <-wait.Done():
			}
		}
		return fmt.Errorf("terminal did not display %s", marker)
	}
	typeLine := func(marker string) error {
		if err := run("type", marker, "--layout", "us"); err != nil {
			return fmt.Errorf("type %s: %w", marker, err)
		}
		if err := run("key", "40"); err != nil {
			return fmt.Errorf("press Return: %w", err)
		}
		return nil
	}

	if err := exec.CommandContext(ctx, "xcrun", "simctl", "openurl", udid, strings.TrimSpace(string(invitation))).Run(); err != nil {
		return fmt.Errorf("open pairing link: %w", err)
	}
	if err := tap("--label", "Pair device"); err != nil {
		return fmt.Errorf("confirm pairing: %w", err)
	}
	if err := tap("--id", "session-"+pipeID); err != nil {
		return fmt.Errorf("select remote session: %w", err)
	}
	if err := tap("--id", "session-terminal"); err != nil {
		return fmt.Errorf("focus terminal: %w", err)
	}
	if err := barrier("ready"); err != nil {
		return err
	}
	for _, peer := range []string{"cli", "mac", "phone", "pad"} {
		marker := "shared-" + peer
		if name == peer {
			if err := typeLine(marker); err != nil {
				return err
			}
		}
		if err := check(marker); err != nil {
			return err
		}
	}
	if err := barrier("switch"); err != nil {
		return err
	}
	if name == "phone" {
		if err := typeLine("independent-phone"); err != nil {
			return err
		}
	}
	if err := check("independent-phone"); err != nil {
		return err
	}
	return barrier("return")
}
