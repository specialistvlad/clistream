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

type viewerPlans struct{ mac, mobile string }

func prepareViewers(root string) (viewerPlans, error) {
	for _, key := range []string{"IPHONE_SIMULATOR_ID", "IPAD_SIMULATOR_ID"} {
		if os.Getenv(key) == "" {
			return viewerPlans{}, fmt.Errorf("%s required", key)
		}
	}
	fmt.Println("Building the Mac and mobile test plans")
	macPlan, err := buildTestPlan(root, "macOS", "generic/platform=macOS")
	if err != nil {
		return viewerPlans{}, err
	}
	mobilePlan, err := buildTestPlan(root, "iOS", "generic/platform=iOS Simulator")
	if err != nil {
		return viewerPlans{}, err
	}
	products := filepath.Join(filepath.Dir(mobilePlan), "Debug-iphonesimulator")
	// Migrate and install each simulator while it has the runner to itself.
	// Leave the iPad up for pairing and start the prepared phone only after that.
	for _, device := range []string{"IPHONE_SIMULATOR_ID", "IPAD_SIMULATOR_ID"} {
		id := os.Getenv(device)
		fmt.Println("Preparing", device)
		for _, cmd := range []*exec.Cmd{
			exec.Command("apple/boot-simulator.sh", id),
			exec.Command("xcrun", "simctl", "install", id, filepath.Join(products, "CLIStream.app")),
			exec.Command("xcrun", "simctl", "install", id, filepath.Join(products, "CLIStreamUITests-Runner.app")),
		} {
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				return viewerPlans{}, err
			}
		}
		if device == "IPHONE_SIMULATOR_ID" {
			if err := exec.Command("xcrun", "simctl", "shutdown", id).Run(); err != nil {
				return viewerPlans{}, fmt.Errorf("shut down the prepared phone: %w", err)
			}
		}
	}
	return viewerPlans{macPlan, mobilePlan}, nil
}

// Run each UI runner against the same broker. The independent CLI joins via
// its normal attach command. A unique marker from each writer is retained in
// the child's history so simulator launch order does not affect the assertion.
func shared(plans viewerPlans, arrivals <-chan string, url, pipeID, pipeRun, terminalID string, command func(string, ...string) *exec.Cmd) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	var participants sync.WaitGroup
	defer func() {
		cancel()
		participants.Wait()
	}()
	results := make(chan error, 4)
	for _, device := range []struct{ name, id string }{
		{"pad", os.Getenv("IPAD_SIMULATOR_ID")}, {"phone", os.Getenv("IPHONE_SIMULATOR_ID")}, {"mac", ""},
	} {
		if device.name == "phone" {
			boot := exec.CommandContext(ctx, "xcrun", "simctl", "bootstatus", device.id, "-b")
			boot.Stdout, boot.Stderr = os.Stdout, os.Stderr
			if err := boot.Run(); err != nil {
				return fmt.Errorf("boot the prepared iPhone: %w", err)
			}
		}
		dest, plan, suite := "platform=macOS", plans.mac, "CLIStream-MacUITests"
		if device.id != "" {
			dest, plan, suite = "platform=iOS Simulator,id="+device.id, plans.mobile, "CLIStream-UITests"
		}
		fmt.Println("Pairing shared viewer:", device.name)
		test := exec.CommandContext(ctx, "xcodebuild", "-quiet", "-xctestrun", plan, "-destination", dest,
			"-parallel-testing-enabled", "NO", "-collect-test-diagnostics", "never",
			"-resultBundlePath", filepath.Join("apple/build", fmt.Sprintf("shared-%d-%s.xcresult", time.Now().Unix(), device.name)),
			"-only-testing:"+suite+"/NativeSessionTests/testConcurrentViewers", "test-without-building")
		test.Env = append(os.Environ(), "TEST_RUNNER_CLISTREAM_FIXTURE="+url, "TEST_RUNNER_CLISTREAM_PARTICIPANT="+device.name,
			"TEST_RUNNER_CLISTREAM_PIPE_ID="+pipeID, "TEST_RUNNER_CLISTREAM_PTY_ID="+terminalID, "TEST_RUNNER_CLISTREAM_CAPTURE_GALLERY=0")
		test.Stdout, test.Stderr = os.Stdout, os.Stderr
		participants.Go(func() { results <- test.Run() })
		select {
		case <-arrivals:
		case err := <-results:
			return fmt.Errorf("%s stopped before pairing: %w", device.name, err)
		case <-ctx.Done():
			return ctx.Err()
		}
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
