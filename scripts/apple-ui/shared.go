package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Kill the wrapper and its simctl/xcodebuild subprocesses together. A child
// inheriting a diagnostic pipe must not keep Wait blocked after XCTest exits.
func sharedCommand(ctx context.Context, program string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

func sharedCheckpoint(label string) {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		fmt.Println("::notice title=Shared UI test::" + label)
	} else {
		fmt.Println(label)
	}
}

func prepareStage(ctx context.Context, label string, limit time.Duration, program string, args ...string) error {
	stage, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	sharedCheckpoint(label)
	cmd := sharedCommand(stage, program, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	err := cmd.Run()
	if stage.Err() != nil {
		return fmt.Errorf("%s: %w", label, stage.Err())
	}
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

// Build each SDK once before starting the simultaneous interaction watchdog.
func buildTestPlan(ctx context.Context, root, platform, destination string) (string, error) {
	dir := filepath.Join(root, "build-"+platform)
	err := prepareStage(ctx, "Build the "+platform+" test plan", 8*time.Minute,
		"apple/acceptance-xcodebuild.sh", platform, "-quiet", "-project", "apple/CLIStream.xcodeproj",
		"-scheme", "CLIStream-"+platform, "-derivedDataPath", dir, "-destination", destination,
		"-jobs", "3", "CLISTREAM_APP_ID=com.vlk.clistream.acceptance.shared", "build-for-testing")
	if err != nil {
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
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	for _, key := range []string{"IPHONE_SIMULATOR_ID", "IPAD_SIMULATOR_ID"} {
		if os.Getenv(key) == "" {
			return viewerPlans{}, fmt.Errorf("%s required", key)
		}
	}
	macPlan, err := buildTestPlan(ctx, root, "macOS", "generic/platform=macOS")
	if err != nil {
		return viewerPlans{}, err
	}
	mobilePlan, err := buildTestPlan(ctx, root, "iOS", "generic/platform=iOS Simulator")
	if err != nil {
		return viewerPlans{}, err
	}
	products := filepath.Join(filepath.Dir(mobilePlan), "Debug-iphonesimulator")
	// Migrate and install each simulator while it has the runner to itself.
	// Leave the iPad up for pairing and start the prepared phone only after that.
	for _, device := range []string{"IPHONE_SIMULATOR_ID", "IPAD_SIMULATOR_ID"} {
		id := os.Getenv(device)
		if err := prepareSimulator(ctx, device, id, products); err != nil {
			return viewerPlans{}, fmt.Errorf("prepare %s: %w", device, err)
		}
		if device == "IPHONE_SIMULATOR_ID" {
			if err := prepareStage(ctx, "Shut down the prepared iPhone", 90*time.Second,
				"xcrun", "simctl", "shutdown", id); err != nil {
				return viewerPlans{}, err
			}
		}
	}
	return viewerPlans{macPlan, mobilePlan}, nil
}

// CoreSimulator can lose its install service immediately after an otherwise
// successful first boot (simctl exits 204, NSMachErrorDomain -308).
func prepareSimulator(ctx context.Context, device, id, products string) error {
	for attempt := 0; ; attempt++ {
		if err := prepareStage(ctx, "Boot "+device, 10*time.Minute, "apple/boot-simulator.sh", id); err != nil {
			return err
		}
		var err error
		for _, bundle := range []string{"CLIStream.app", "CLIStreamUITests-Runner.app"} {
			err = prepareStage(ctx, "Install "+bundle+" on "+device, 4*time.Minute,
				"xcrun", "simctl", "install", id, filepath.Join(products, bundle))
			if err != nil {
				break
			}
		}
		var exit *exec.ExitError
		if attempt != 0 || !errors.As(err, &exit) || exit.ExitCode() != 204 {
			return err
		}
		fmt.Println("Simulator installation service terminated; rebooting before retry:", err)
		// boot-simulator may erase a failed first boot. Reinstall both products.
		if err := prepareStage(ctx, "Shut down "+device+" before installation retry", 90*time.Second,
			"xcrun", "simctl", "shutdown", id); err != nil {
			return err
		}
	}
}

// Run each UI runner against the same broker. The independent CLI joins via
// its normal attach command. A unique marker from each writer is retained in
// the child's history so simulator launch order does not affect the assertion.
func shared(plans viewerPlans, arrivals <-chan string, abortBarriers func(), url, pipeID, pipeRun, terminalID string, command func(string, ...string) *exec.Cmd) error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	var participants sync.WaitGroup
	defer func() {
		cancel()
		participants.Wait()
	}()
	results := make(chan error, 4)
	participants.Go(func() { observeSharedRunner(ctx) })
	for _, device := range []struct{ name, id string }{
		{"pad", os.Getenv("IPAD_SIMULATOR_ID")}, {"phone", os.Getenv("IPHONE_SIMULATOR_ID")}, {"mac", ""},
	} {
		if device.name == "phone" {
			if err := prepareStage(ctx, "Boot the prepared iPhone", 5*time.Minute,
				"xcrun", "simctl", "bootstatus", device.id, "-b"); err != nil {
				return err
			}
		}
		dest, plan, suite := "platform=macOS", plans.mac, "CLIStream-MacUITests"
		if device.id != "" {
			dest, plan, suite = "platform=iOS Simulator,id="+device.id, plans.mobile, "CLIStream-UITests"
		}
		sharedCheckpoint("Pair shared viewer " + device.name)
		env := append(os.Environ(), "TEST_RUNNER_CLISTREAM_FIXTURE="+url, "TEST_RUNNER_CLISTREAM_PARTICIPANT="+device.name,
			"TEST_RUNNER_CLISTREAM_PIPE_ID="+pipeID, "TEST_RUNNER_CLISTREAM_PTY_ID="+terminalID, "TEST_RUNNER_CLISTREAM_CAPTURE_GALLERY=0")
		participants.Go(func() {
			err := testSharedViewer(ctx, device.name, device.id, plan, dest, suite, env)
			if err != nil {
				err = fmt.Errorf("%s XCTest: %w", device.name, err)
			}
			results <- err
		})
		select {
		case <-arrivals:
		case err := <-results:
			return fmt.Errorf("%s stopped before pairing: %w", device.name, err)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	sharedCheckpoint("Join the three native viewers with the CLI")
	participants.Go(func() { results <- observeCLI(ctx, command("clistream", "attach", pipeRun)) })
	var failed error
	var grace <-chan time.Time
	for range 4 {
		select {
		case err := <-results:
			if err != nil && failed == nil {
				failed = err
				fmt.Fprintln(os.Stderr, "Shared viewer exited:", err)
				// Release blocked UI tests and let XCTest finalize their native
				// result bundles before cancelling the other subprocesses.
				abortBarriers()
				grace = time.After(90 * time.Second)
			}
		case <-grace:
			return failed
		case <-ctx.Done():
			return errors.Join(failed, ctx.Err())
		}
	}
	return failed
}

func observeSharedRunner(ctx context.Context) {
	// Three resident XCTest drivers can contend with the two simulators.
	// Sample only system pressure and scheduling, never process arguments.
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	previous := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			probe, cancel := context.WithTimeout(ctx, 3*time.Second)
			data, _ := sharedCommand(probe, "/usr/sbin/sysctl", "kern.memorystatus_vm_pressure_level", "vm.swapusage", "vm.loadavg").Output()
			cancel()
			fmt.Printf("Shared runner interval %s: %s\n", now.Sub(previous).Round(time.Second), bytes.TrimSpace(data))
			previous = now
		}
	}
}

func testSharedViewer(ctx context.Context, name, id, plan, destination, suite string, env []string) error {
	for attempt := 0; ; attempt++ {
		bundle := filepath.Join("apple/build", fmt.Sprintf("shared-%d-%s.xcresult", time.Now().Unix(), name))
		test := sharedCommand(ctx, "xcodebuild", "-quiet", "-xctestrun", plan, "-destination", destination,
			"-parallel-testing-enabled", "NO", "-collect-test-diagnostics", "never",
			"-resultBundlePath", bundle,
			"-only-testing:"+suite+"/NativeSessionTests/testConcurrentViewers", "test-without-building")
		test.Env = env
		var diagnostics bytes.Buffer
		test.Stdout = io.MultiWriter(os.Stdout, &diagnostics)
		test.Stderr = test.Stdout
		err := test.Run()
		if errors.Is(err, exec.ErrWaitDelay) && test.ProcessState.Success() {
			// XCTest finished successfully; one of its detached helpers held a pipe open.
			err = nil
		}
		if err == nil || attempt != 0 || name != "pad" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" ||
			!bytes.Contains(diagnostics.Bytes(), []byte("Test crashed with signal term while preparing to run tests.")) {
			return err
		}
		// Only the first iPad can recover here, before any viewer has paired.
		// Never retry a testcase that Xcode actually started.
		summaryCtx, cancelSummary := context.WithTimeout(ctx, 30*time.Second)
		data, readErr := sharedCommand(summaryCtx, "xcrun", "xcresulttool", "get", "test-results", "summary", "--path", bundle).Output()
		cancelSummary()
		var summary struct{ TotalTestCount *int }
		if readErr != nil || json.Unmarshal(data, &summary) != nil || summary.TotalTestCount == nil || *summary.TotalTestCount != 0 {
			return err
		}
		fmt.Println("iPad runner terminated before its first test. Rebooting the isolated iPad and retrying once.")
		if err := os.Rename(bundle, bundle+".startup"); err != nil {
			return err
		}
		if err := prepareStage(ctx, "Shut down iPad after runner startup failure", 90*time.Second,
			"xcrun", "simctl", "shutdown", id); err != nil {
			return err
		}
		if err := prepareStage(ctx, "Boot iPad after runner startup failure", 5*time.Minute,
			"apple/boot-simulator.sh", id); err != nil {
			return err
		}
	}
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
