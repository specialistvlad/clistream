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

type tabletScreen struct {
	pair, openInvitation bool
	terminal             string
}

// Keep only the confirmation state and the visible terminal value. Other
// accessibility elements can contain enrollment links and must stay private.
func readTabletScreen(raw []byte) (tabletScreen, error) {
	type node struct {
		ID          string          `json:"AXUniqueId"`
		Label       string          `json:"AXLabel"`
		Value       json.RawMessage `json:"AXValue"`
		Type, Title string
		Hidden      bool
		Children    []node
	}
	var tree struct {
		Data struct{ Result json.RawMessage }
	}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return tabletScreen{}, err
	}
	var roots []node
	switch {
	case len(tree.Data.Result) == 0:
		return tabletScreen{}, errors.New("missing accessibility tree")
	case tree.Data.Result[0] == '{':
		var root node
		if err := json.Unmarshal(tree.Data.Result, &root); err != nil {
			return tabletScreen{}, err
		}
		roots = []node{root}
	default:
		if err := json.Unmarshal(tree.Data.Result, &roots); err != nil {
			return tabletScreen{}, err
		}
	}
	var screen tabletScreen
	var visit func(node) (named, open bool)
	visit = func(n node) (bool, bool) {
		if n.Hidden {
			return false, false
		}
		named := strings.Contains(n.Label+" "+n.Title, "CLIStream")
		open := n.Label == "Open"
		switch n.ID {
		case "workspace-confirm":
			screen.pair = n.Label == "Pair device"
		case "session-terminal":
			var text string
			if json.Unmarshal(n.Value, &text) == nil && text != "" {
				screen.terminal = text
			}
		}
		for _, child := range n.Children {
			childNamed, childOpen := visit(child)
			named = named || childNamed
			open = open || childOpen
		}
		if strings.EqualFold(n.Type, "Alert") && named && open {
			screen.openInvitation = true
		}
		return named, open
	}
	for _, root := range roots {
		visit(root)
	}
	return screen, nil
}

func waitTabletScreen(ctx context.Context, id string, duration time.Duration, match func(tabletScreen) bool) (tabletScreen, error) {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	for {
		data, err := sharedCommand(ctx, "axe", "--output", "json", "describe-ui", "--minimal-keys", "--udid", id).Output()
		if err == nil {
			screen, err := readTabletScreen(data)
			if err != nil {
				return screen, fmt.Errorf("decode iPad accessibility tree: %w", err)
			}
			if match(screen) {
				return screen, nil
			}
		}
		select {
		case <-ctx.Done():
			return tabletScreen{}, ctx.Err()
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
	simctl := func(args ...string) error {
		command, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		return sharedCommand(command, "xcrun", append([]string{"simctl"}, args...)...).Run()
	}
	sharedCheckpoint("Open a signed invitation in the iPad app")
	if err := simctl("launch", id, "com.vlk.clistream.acceptance.shared"); err != nil {
		return fmt.Errorf("launch iPad app: %w", err)
	}
	invitation, err := fixture("QR")
	if err != nil {
		return err
	}
	if err := simctl("openurl", id, invitation); err != nil {
		return fmt.Errorf("deliver invitation to iPad: %w", err)
	}
	axe := func(args ...string) error {
		command, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		return sharedCommand(command, "axe", append(args, "--udid", id)...).Run()
	}
	tap := func(by, name string) error {
		return axe("tap", by, name, "--wait-timeout", "45", "--poll-interval", "1")
	}
	screen, err := waitTabletScreen(ctx, id, 90*time.Second, func(s tabletScreen) bool { return s.pair || s.openInvitation })
	if err != nil {
		return fmt.Errorf("wait for iPad invitation confirmation: %w", err)
	}
	if !screen.pair && screen.openInvitation {
		if err := tap("--label", "Open"); err != nil {
			return fmt.Errorf("open system invitation prompt: %w", err)
		}
	}
	sharedCheckpoint("Confirm the invitation and select the terminal on iPad")
	for _, field := range []string{"workspace-confirm", "session-" + pipeID, "session-terminal"} {
		if err := tap("--id", field); err != nil {
			return fmt.Errorf("tap iPad %s: %w", field, err)
		}
	}
	echo := func(text string) error {
		_, err := waitTabletScreen(ctx, id, 5*time.Minute, func(s tabletScreen) bool { return strings.Contains(s.terminal, text) })
		if err != nil {
			return fmt.Errorf("iPad terminal missing %s: %w", text, err)
		}
		sharedCheckpoint("iPad observed " + text)
		return nil
	}
	if err := echo("preview-ready"); err != nil {
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
		if err := echo("out:shared-" + writer); err != nil {
			return err
		}
	}
	if _, err := fixture("switch/pad"); err != nil {
		return err
	}
	if err := echo("out:independent-phone"); err != nil {
		return err
	}
	_, err = fixture("return/pad")
	return err
}
