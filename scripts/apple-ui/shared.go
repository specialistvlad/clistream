package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Build each SDK once before starting the simultaneous interaction watchdog.
func buildTestPlan(root, platform, destination string) (string, error) {
	dir := filepath.Join(root, "build-"+platform)
	cmd := exec.Command("xcodebuild", "-quiet", "-project", "apple/CLIStream.xcodeproj",
		"-scheme", "CLIStream-"+platform, "-derivedDataPath", dir, "-destination", destination,
		"-jobs", "3", "CLISTREAM_APP_ID=com.vlk.clistream.acceptance.shared", "build-for-testing")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	plans, err := filepath.Glob(filepath.Join(dir, "Build", "Products", "*.xctestrun"))
	if err != nil {
		return "", err
	}
	if len(plans) != 1 {
		return "", fmt.Errorf("expected one %s test plan, found %d", platform, len(plans))
	}
	return plans[0], nil
}

func prepareViewers(root string) (string, error) {
	for _, key := range []string{"IPHONE_SIMULATOR_ID", "IPAD_SIMULATOR_ID"} {
		if os.Getenv(key) == "" {
			return "", fmt.Errorf("%s required", key)
		}
	}
	fmt.Println("Building the Mac and mobile test plans")
	macPlan, err := buildTestPlan(root, "macOS", "generic/platform=macOS")
	if err != nil {
		return "", err
	}
	mobilePlan, err := buildTestPlan(root, "iOS", "generic/platform=iOS Simulator")
	if err != nil {
		return "", err
	}
	app := filepath.Join(filepath.Dir(mobilePlan), "Debug-iphonesimulator", "CLIStream.app")
	for _, device := range []string{"IPAD_SIMULATOR_ID", "IPHONE_SIMULATOR_ID"} {
		id := os.Getenv(device)
		fmt.Println("Preparing", device)
		for _, cmd := range []*exec.Cmd{
			exec.Command("apple/boot-simulator.sh", id),
			exec.Command("xcrun", "simctl", "install", id, app),
			exec.Command("xcrun", "simctl", "launch", id, "com.vlk.clistream.acceptance.shared"),
		} {
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				return "", err
			}
		}
	}
	return macPlan, nil
}

// Run each UI runner against the same broker. The independent CLI joins via
// its normal attach command. A unique marker from each writer is retained in
// the child's history so simulator launch order does not affect the assertion.
func shared(macPlan string, arrivals <-chan string, url, pipeID, pipeRun, terminalID string, command func(string, ...string) *exec.Cmd) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	var participants sync.WaitGroup
	defer func() {
		cancel()
		participants.Wait()
	}()
	results := make(chan error, 4)
	for _, device := range []struct{ name, id string }{
		{"pad", os.Getenv("IPAD_SIMULATOR_ID")}, {"phone", os.Getenv("IPHONE_SIMULATOR_ID")},
	} {
		fmt.Println("Pairing shared viewer:", device.name)
		participants.Go(func() {
			results <- mobileViewer(ctx, device.name, device.id, url, pipeID, func() ([]byte, error) {
				return command("clistreamd", "invite").Output()
			})
		})
		select {
		case <-arrivals:
		case err := <-results:
			return fmt.Errorf("%s stopped before pairing: %w", device.name, err)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	fmt.Println("Launching shared viewer: mac")
	mac := exec.CommandContext(ctx, "xcodebuild", "-quiet", "-xctestrun", macPlan, "-destination", "platform=macOS", "-parallel-testing-enabled", "NO", "-collect-test-diagnostics", "never", "-resultBundlePath", filepath.Join("apple/build", fmt.Sprintf("shared-%d-mac.xcresult", time.Now().Unix())), "-only-testing:CLIStream-MacUITests/NativeSessionTests/testConcurrentViewers", "test-without-building")
	mac.Env = append(os.Environ(), "TEST_RUNNER_CLISTREAM_FIXTURE="+url, "TEST_RUNNER_CLISTREAM_PARTICIPANT=mac", "TEST_RUNNER_CLISTREAM_PIPE_ID="+pipeID, "TEST_RUNNER_CLISTREAM_PTY_ID="+terminalID)
	mac.Stdout, mac.Stderr = os.Stdout, os.Stderr
	participants.Go(func() { results <- mac.Run() })
	select {
	case <-arrivals:
	case err := <-results:
		return fmt.Errorf("a shared viewer stopped before the Mac paired: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}
	participants.Go(func() { results <- observeCLI(ctx, command("clistream", "attach", pipeRun)) })
	for range 4 {
		select {
		case err := <-results:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Join after all UI clients are attached so the first marker verifies a live broadcast.
func observeCLI(ctx context.Context, cli *exec.Cmd) error {
	in, err := cli.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cli.StdoutPipe()
	if err != nil {
		return err
	}
	cli.Stderr = os.Stderr
	if err := cli.Start(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { cli.Process.Kill() })
	defer func() { stop(); in.Close(); cli.Process.Kill(); cli.Wait() }()
	if _, err := fmt.Fprintln(in, "shared-cli"); err != nil {
		return err
	}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		for _, name := range []string{"mac", "phone", "pad", "cli"} {
			if scanner.Text() == "out:shared-"+name {
				seen[name] = true
			}
		}
		if len(seen) == 4 && scanner.Text() == "out:independent-phone" {
			return nil
		}
	}
	return fmt.Errorf("CLI stream ended before the final marker; observed writers %v (read error %v)", seen, scanner.Err())
}
