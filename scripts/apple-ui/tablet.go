package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Only the visible terminal's value is evidence of a child echo. Other
// accessibility elements can contain enrollment links and must stay private.
func readTabletTerminal(raw []byte) (string, error) {
	type node struct {
		ID       string          `json:"AXUniqueId"`
		Value    json.RawMessage `json:"AXValue"`
		Hidden   bool
		Children []node
	}
	var tree struct {
		Data struct{ Result json.RawMessage }
	}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return "", err
	}
	var roots []node
	switch {
	case len(tree.Data.Result) == 0:
		return "", errors.New("missing accessibility tree")
	case tree.Data.Result[0] == '{':
		var root node
		if err := json.Unmarshal(tree.Data.Result, &root); err != nil {
			return "", err
		}
		roots = []node{root}
	default:
		if err := json.Unmarshal(tree.Data.Result, &roots); err != nil {
			return "", err
		}
	}
	var visit func(node) string
	visit = func(n node) string {
		if n.Hidden {
			return ""
		}
		if n.ID == "session-terminal" {
			var text string
			if json.Unmarshal(n.Value, &text) == nil && text != "" {
				return text
			}
		}
		for _, child := range n.Children {
			if text := visit(child); text != "" {
				return text
			}
		}
		return ""
	}
	for _, root := range roots {
		if text := visit(root); text != "" {
			return text, nil
		}
	}
	return "", nil
}

func waitTabletEcho(ctx context.Context, id, expected string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		data, err := sharedCommand(ctx, "axe", "--output", "json", "describe-ui", "--minimal-keys", "--udid", id).Output()
		if err == nil {
			terminal, err := readTabletTerminal(data)
			if err != nil {
				return fmt.Errorf("decode iPad accessibility tree: %w", err)
			}
			if strings.Contains(terminal, expected) {
				sharedCheckpoint("iPad observed " + expected)
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("iPad terminal missing %s: %w", expected, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func driveSharedPad(ctx context.Context, id, app, base, pipeID string) error {
	// Run the shipping app on the iPad with the same accessibility and HID
	// tool used by the standalone mobile suites. It needs no resident XCTest
	// process while the Mac and iPhone start their own native test runners.
	if err := prepareStage(ctx, "Install the shared iPad app", 8*time.Minute, "xcrun", "simctl", "install", id, app); err != nil {
		return err
	}
	fixture := func(route string) (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+route, nil)
		if err != nil {
			return "", errors.New("invalid shared fixture address")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("shared fixture %s unreachable", route)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return "", fmt.Errorf("shared fixture %s returned %d", route, res.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(res.Body, 8192))
		return strings.TrimSpace(string(data)), err
	}
	if err := prepareStage(ctx, "Launch the iPad app", 90*time.Second, "xcrun", "simctl", "launch", id, "com.vlk.clistream.acceptance.shared"); err != nil {
		return err
	}
	axe := func(args ...string) error {
		command, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		return sharedCommand(command, "axe", append(args, "--udid", id)...).Run()
	}
	tap := func(by, name string) error {
		return axe("tap", by, name, "--wait-timeout", "45", "--poll-interval", "1")
	}
	sharedCheckpoint("Open Add Server on iPad")
	if err := tap("--id", "workspace-add-server"); err != nil {
		return fmt.Errorf("open iPad Add Server: %w", err)
	}
	if err := tap("--id", "pairing-link"); err != nil {
		return fmt.Errorf("focus iPad invitation field: %w", err)
	}
	invitation, err := fixture("QR")
	if err != nil {
		return err
	}
	// Pass the fresh link as private stdin to the keyboard driver. The secure
	// invitation field receives real keystrokes without printing the link.
	typing, done := context.WithTimeout(ctx, 90*time.Second)
	input := sharedCommand(typing, "axe", "type", "--stdin", "--layout", "us", "--udid", id)
	input.Stdin = strings.NewReader(invitation)
	err = input.Run()
	done()
	if err != nil {
		return fmt.Errorf("enter signed iPad invitation: %w", err)
	}
	sharedCheckpoint("Confirm the signed invitation on iPad")
	if err := tap("--label", "Pair device"); err != nil {
		return fmt.Errorf("tap Pair device on iPad: %w", err)
	}
	for _, field := range []string{"session-" + pipeID, "session-terminal"} {
		if err := tap("--id", field); err != nil {
			return fmt.Errorf("tap iPad %s: %w", field, err)
		}
	}
	if err := waitTabletEcho(ctx, id, "preview-ready"); err != nil {
		return err
	}
	if _, err := fixture("ready/pad"); err != nil {
		return err
	}
	for _, writer := range []string{"cli", "mac", "phone", "pad"} {
		if writer == "pad" {
			if err := axe("type", "shared-pad", "--layout", "us"); err != nil {
				return fmt.Errorf("type on iPad: %w", err)
			}
			if err := axe("key", "40"); err != nil {
				return fmt.Errorf("press iPad Return: %w", err)
			}
		}
		if err := waitTabletEcho(ctx, id, "out:shared-"+writer); err != nil {
			return err
		}
	}
	if _, err := fixture("switch/pad"); err != nil {
		return err
	}
	if err := waitTabletEcho(ctx, id, "out:independent-phone"); err != nil {
		return err
	}
	_, err = fixture("return/pad")
	return err
}
