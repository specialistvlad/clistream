package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	if err := bootViewer(context.Background(), "IPAD_SIMULATOR_ID"); err != nil {
		return viewerPlans{}, err
	}
	return viewerPlans{macPlan, mobilePlan}, nil
}

func bootViewer(ctx context.Context, key string) error {
	fmt.Println("Booting", key)
	cmd := exec.CommandContext(ctx, "xcrun", "simctl", "bootstatus", os.Getenv(key), "-b")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// Run each UI runner against the same broker. The independent CLI joins via
// its normal attach command. A unique marker from each writer is retained in
// the child's history so simulator launch order does not affect the assertion.
func shared(plans viewerPlans, arrivals <-chan string, url, pipeID, pipeRun, terminalID string, command func(string, ...string) *exec.Cmd) error {
	cli := command("clistream", "attach", pipeRun)
	cli.Stderr = os.Stderr
	in, err := cli.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cli.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cli.Start(); err != nil {
		return err
	}
	defer func() { in.Close(); cli.Process.Kill(); cli.Wait() }()
	if _, err := fmt.Fprintln(in, "shared-cli"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	results := make(chan error, 4)
	go func() {
		seen := map[string]bool{}
		scanner := bufio.NewScanner(out)
		for scanner.Scan() {
			for _, name := range []string{"mac", "phone", "pad", "cli"} {
				if scanner.Text() == "out:shared-"+name {
					seen[name] = true
				}
			}
			if len(seen) == 4 && scanner.Text() == "out:independent-phone" {
				results <- nil
				return
			}
		}
		results <- fmt.Errorf("CLI stream ended before the final marker; observed participants %v (read error %v)", seen, scanner.Err())
	}()
	for _, name := range []string{"pad", "phone", "mac"} {
		if name == "phone" {
			if err := bootViewer(ctx, "IPHONE_SIMULATOR_ID"); err != nil {
				return err
			}
		}
		fmt.Println("Launching shared viewer:", name)
		dest := "platform=macOS"
		plan := plans.mac
		suite := "CLIStream-MacUITests"
		if name != "mac" {
			key := map[string]string{"phone": "IPHONE_SIMULATOR_ID", "pad": "IPAD_SIMULATOR_ID"}[name]
			dest = "platform=iOS Simulator,id=" + os.Getenv(key)
			plan = plans.mobile
			suite = "CLIStream-UITests"
		}
		test := exec.CommandContext(ctx, "xcodebuild", "-quiet", "-xctestrun", plan, "-destination", dest, "-parallel-testing-enabled", "NO", "-collect-test-diagnostics", "never", "-resultBundlePath", filepath.Join("apple/build", fmt.Sprintf("shared-%d-%s.xcresult", time.Now().Unix(), name)), "-only-testing:"+suite+"/NativeSessionTests/testConcurrentViewers", "test-without-building")
		test.Env = append(os.Environ(), "TEST_RUNNER_CLISTREAM_FIXTURE="+url, "TEST_RUNNER_CLISTREAM_PARTICIPANT="+name, "TEST_RUNNER_CLISTREAM_PIPE_ID="+pipeID, "TEST_RUNNER_CLISTREAM_PTY_ID="+terminalID)
		test.Stdout = os.Stdout
		test.Stderr = os.Stderr
		go func() {
			if err := test.Run(); err != nil {
				results <- fmt.Errorf("%s: %w", name, err)
			} else {
				results <- nil
			}
		}()
		// Install and launch one simulator test runner at a time. Each stays
		// attached at the barrier while the next participant joins.
		if name != "mac" {
			select {
			case <-arrivals:
			case err := <-results:
				return fmt.Errorf("%s stopped before the shared session was ready: %v", name, err)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
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
