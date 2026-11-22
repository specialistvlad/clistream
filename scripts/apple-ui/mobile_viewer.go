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

	"github.com/specialistvlad/clistream/internal/identity"
)

// Drive the shipping mobile app over its accessibility interface. Both apps stay
// attached without keeping an additional XCTest runner resident in each simulator.
func mobileViewer(ctx context.Context, name, udid, fixture, pipeID string, invite func() ([]byte, error)) (result error) {
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
	tap := func(id, label, next string) error {
		err := run("tap", "--id", id, "--wait-timeout", "30")
		if err == nil {
			return nil
		}
		// Only known UI labels can enter public CI logs, never the invitation or the terminal contents.
		tree, readErr := exec.CommandContext(ctx, "axe", "describe-ui", "--minimal-keys", "--udid", udid).Output()
		if readErr != nil {
			return fmt.Errorf("%w (could not read the accessibility tree)", err)
		}
		// A lost simulator input receipt can arrive after the app changed screens.
		if next != "" && bytes.Contains(tree, []byte(next)) {
			return nil
		}
		if label != "" && bytes.Contains(tree, []byte(label)) {
			err = run("tap", "--label", label, "--wait-timeout", "15")
		} else if bytes.Contains(tree, []byte(id)) {
			err = run("tap", "--id", id, "--wait-timeout", "15")
		}
		if err == nil {
			return nil
		}
		var visible []string
		for _, label := range []string{"Servers", "Add Server", "Pairing link", "Pair device", "Pair a server", "Couldn’t pair this device", "Open", "Allow", "Don't Allow", "Paste", "SpringBoard", "Lock Screen", "session-terminal"} {
			if bytes.Contains(tree, []byte(label)) {
				visible = append(visible, label)
			}
		}
		return fmt.Errorf("%w (visible controls: %s)", err, strings.Join(visible, ", "))
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
	waitForText := func(text, description string, timeout time.Duration) error {
		wait, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		for wait.Err() == nil {
			tree, err := exec.CommandContext(wait, "axe", "describe-ui", "--minimal-keys", "--udid", udid).Output()
			if err == nil && bytes.Contains(tree, []byte(text)) {
				return nil
			}
			select {
			case <-time.After(time.Second):
			case <-wait.Done():
			}
		}
		return fmt.Errorf("the app did not display %s", description)
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

	// An acknowledged simctl launch can precede SpringBoard's accessibility service.
	// Recover only during first launch, before the app has an invitation or a session.
	var launchErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			fmt.Println("Restarting the simulator before pairing:", name)
			if err := exec.CommandContext(ctx, "xcrun", "simctl", "shutdown", udid).Run(); err != nil {
				return fmt.Errorf("restart unresponsive simulator: %w", err)
			}
			if err := exec.CommandContext(ctx, "xcrun", "simctl", "bootstatus", udid, "-b").Run(); err != nil {
				return fmt.Errorf("wait for simulator restart: %w", err)
			}
		}
		if launchErr = exec.CommandContext(ctx, "xcrun", "simctl", "launch", udid, "com.vlk.clistream.acceptance.shared").Run(); launchErr != nil {
			continue
		}
		if launchErr = waitForText("Add Server", "the Add Server toolbar button", 90*time.Second); launchErr != nil {
			continue
		}
		launchErr = tap("workspace-add-server", "Add Server", "pairing-link")
		if launchErr == nil {
			break
		}
	}
	if launchErr != nil {
		return fmt.Errorf("open the pairing form on the prepared simulator: %w", launchErr)
	}
	if err := tap("pairing-link", "Pairing link", ""); err != nil {
		return fmt.Errorf("focus invitation field: %w", err)
	}
	invitation, err := invite()
	if err != nil {
		return fmt.Errorf("issue invitation: %w", err)
	}
	link := strings.TrimSpace(string(invitation))
	review, err := identity.ParseInvitation(link)
	if err != nil {
		return fmt.Errorf("invalid fixture invitation")
	}
	typing := exec.CommandContext(ctx, "axe", "type", "--stdin", "--layout", "us", "--udid", udid)
	typing.Stdin = strings.NewReader(link)
	if err := typing.Run(); err != nil {
		return fmt.Errorf("enter invitation: %w", err)
	}
	if err := waitForText(review.Address, "the reviewed server address", 45*time.Second); err != nil {
		return err
	}
	if err := tap("workspace-confirm", "Pair device", "session-"+pipeID); err != nil {
		return fmt.Errorf("confirm pairing: %w", err)
	}
	if err := tap("session-"+pipeID, "", "session-terminal"); err != nil {
		return fmt.Errorf("select remote session: %w", err)
	}
	if err := tap("session-terminal", "", ""); err != nil {
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
		if err := waitForText("out:"+marker, marker, 3*time.Minute); err != nil {
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
	if err := waitForText("out:independent-phone", "the independent phone marker", time.Minute); err != nil {
		return err
	}
	return barrier("return")
}
