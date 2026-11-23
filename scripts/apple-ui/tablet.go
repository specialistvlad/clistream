package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type tabletFrame struct{ X, Y, Width, Height float64 }

func (frame tabletFrame) visible() bool {
	return frame.Width > 0 && frame.Height > 0 && frame.X+frame.Width/2 > 0 && frame.Y+frame.Height/2 > 0
}

type tabletScreen struct {
	app, pair           bool
	add, sidebar, field tabletFrame
	terminal            string
}

// Keep only known navigation targets and the visible terminal value. Other
// accessibility elements can contain enrollment links.
func readTabletScreen(raw []byte) (tabletScreen, error) {
	type node struct {
		ID       string          `json:"AXUniqueId"`
		Label    string          `json:"AXLabel"`
		Value    json.RawMessage `json:"AXValue"`
		Type     string
		Frame    tabletFrame
		Hidden   bool
		Children []node
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
	var visit func(node)
	visit = func(n node) {
		if n.Hidden {
			return
		}
		switch {
		case n.ID == "session-terminal" || n.Label == "Terminal":
			var value string
			if json.Unmarshal(n.Value, &value) == nil && value != "" {
				screen.terminal = value
			}
		case n.ID == "workspace-add-server" || n.Label == "Add Server":
			if n.Frame.visible() {
				screen.add = n.Frame
			}
		case n.Label == "Show Sidebar" || n.Label == "Toggle Sidebar" || n.Label == "Sidebar":
			if n.Frame.visible() {
				screen.sidebar = n.Frame
			}
		case n.ID == "pairing-link" || n.Label == "Pairing link" || n.Type == "SecureTextField":
			if n.Frame.visible() {
				screen.field = n.Frame
			}
		case n.ID == "workspace-confirm" || n.Label == "Pair device" || n.Label == "Pair a server":
			screen.pair = true
		case n.Label == "CLIStream" || n.Label == "Select a CLI" || n.Label == "Add a server to see its CLI apps.":
			screen.app = true
		}
		for _, child := range n.Children {
			visit(child)
		}
	}
	for _, root := range roots {
		visit(root)
	}
	return screen, nil
}

func inspectTablet(ctx context.Context, id string) (tabletScreen, error) {
	raw, err := sharedCommand(ctx, "axe", "--output", "json", "describe-ui", "--minimal-keys", "--udid", id).Output()
	if err != nil {
		return tabletScreen{}, err
	}
	return readTabletScreen(raw)
}

func openTabletInvitation(ctx context.Context, id string, input func(context.Context, ...string) error) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	tap := func(frame tabletFrame) error {
		return input(ctx, "tap", "-x", strconv.FormatFloat(frame.X+frame.Width/2, 'f', 1, 64),
			"-y", strconv.FormatFloat(frame.Y+frame.Height/2, 'f', 1, 64))
	}
	var screen tabletScreen
	var tappedAdd, revealed, shortcut bool
	var lastInput time.Time
	for {
		observed, err := inspectTablet(ctx, id)
		if err == nil {
			screen = observed
			switch {
			case screen.pair && screen.field.visible():
				return tap(screen.field)
			case screen.pair || time.Since(lastInput) < 4*time.Second:
				// Wait for a presented sheet or a sidebar transition to settle.
			case screen.add.visible() && !tappedAdd:
				tappedAdd = true
				err = tap(screen.add)
				lastInput = time.Now()
			case !screen.add.visible() && screen.sidebar.visible() && !revealed:
				revealed = true
				err = tap(screen.sidebar)
				lastInput = time.Now()
			case screen.app && !shortcut:
				// Compact iPad layouts can hide the sidebar toolbar. Use
				// the same Command-K shortcut as a hardware keyboard.
				shortcut = true
				err = input(ctx, "key-combo", "--modifiers", "227", "--key", "14")
				lastInput = time.Now()
			}
			if err != nil {
				return fmt.Errorf("iPad navigation input: %w", err)
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("iPad invitation field was unavailable (app %t, sidebar %t, add %t, pair %t, field %t, shortcut %t): %w",
				screen.app, revealed, tappedAdd, screen.pair, screen.field.visible(), shortcut, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

func waitTabletEcho(ctx context.Context, id, expected string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		screen, err := inspectTablet(ctx, id)
		if err == nil {
			if strings.Contains(screen.terminal, expected) {
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
	axe := func(ctx context.Context, args ...string) error {
		command, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		return sharedCommand(command, "axe", append(args, "--udid", id)...).Run()
	}
	tap := func(by, name string) error {
		return axe(ctx, "tap", by, name, "--wait-timeout", "45", "--poll-interval", "1")
	}
	sharedCheckpoint("Open Add Server on iPad")
	if err := openTabletInvitation(ctx, id, axe); err != nil {
		// Capture only before generating an invitation or typing any secret.
		if os.Getenv("GITHUB_ACTIONS") == "true" && os.MkdirAll("results", 0755) == nil {
			snapshot, cancel := context.WithTimeout(ctx, 20*time.Second)
			_ = sharedCommand(snapshot, "xcrun", "simctl", "io", id, "screenshot", "results/shared-ipad-before-pairing.png").Run()
			cancel()
		}
		return err
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
			if err := axe(ctx, "type", "shared-pad", "--layout", "us"); err != nil {
				return fmt.Errorf("type on iPad: %w", err)
			}
			if err := axe(ctx, "key", "40"); err != nil {
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
