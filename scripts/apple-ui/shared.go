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
	// The phone's second launch overlaps the active iPad. Preload it while
	// the machine is idle; XCTest itself installs any product still missing.
	phone := os.Getenv("IPHONE_SIMULATOR_ID")
	if err := prepareStage(ctx, "Boot iPhone before pairing", 10*time.Minute, "apple/boot-simulator.sh", phone); err != nil {
		return viewerPlans{}, err
	}
	for _, bundle := range []string{"CLIStream.app", "CLIStreamUITests-Runner.app"} {
		err := prepareStage(ctx, "Preload "+bundle+" on iPhone", 6*time.Minute,
			"xcrun", "simctl", "install", phone, filepath.Join(products, bundle))
		if err == nil {
			continue
		}
		var exit *exec.ExitError
		lostInstaller := errors.As(err, &exit) && exit.ExitCode() == 204
		if ctx.Err() != nil || (!errors.Is(err, context.DeadlineExceeded) && !lostInstaller) {
			return viewerPlans{}, err
		}
		sharedCheckpoint("iPhone preload interrupted. Reboot and use Xcode to finish installation.")
		break
	}
	if err := shutdownSimulator(ctx, "Shut down iPhone before starting iPad", phone); err != nil {
		return viewerPlans{}, err
	}
	// The phone is off throughout the first iPad test launch. The normal
	// XCTest install runs here just as it does on a single-device runner.
	if err := prepareStage(ctx, "Boot iPad for XCTest installation and pairing", 10*time.Minute,
		"apple/boot-simulator.sh", os.Getenv("IPAD_SIMULATOR_ID")); err != nil {
		return viewerPlans{}, err
	}
	return viewerPlans{macPlan, mobilePlan}, nil
}

func shutdownSimulator(ctx context.Context, label, id string) error {
	err := prepareStage(ctx, label, 90*time.Second, "xcrun", "simctl", "shutdown", id)
	if err == nil {
		return nil
	}
	// CoreSimulator may already have shut down the device after losing its
	// installation service. Confirm the state before proceeding to reboot.
	query, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	data, listErr := sharedCommand(query, "xcrun", "simctl", "list", "devices", "--json").Output()
	var list struct {
		Devices map[string][]struct{ UDID, State string }
	}
	if listErr == nil && json.Unmarshal(data, &list) == nil {
		for _, devices := range list.Devices {
			for _, device := range devices {
				if device.UDID == id && device.State == "Shutdown" {
					return nil
				}
			}
		}
	}
	return err
}

func bootPreparedPhone(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 7*time.Minute)
	defer cancel()
	var diagnostic bytes.Buffer
	boot := func(label string) error {
		sharedCheckpoint(label)
		cmd := sharedCommand(ctx, "xcrun", "simctl", "bootstatus", id, "-b")
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, &diagnostic)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", label, errors.Join(err, ctx.Err()))
		}
		return nil
	}
	err := boot("Boot the prepared iPhone")
	var exit *exec.ExitError
	if os.Getenv("RUNNER_ENVIRONMENT") == "github-hosted" && errors.As(err, &exit) &&
		exit.ExitCode() == int(syscall.ECONNABORTED) && ctx.Err() == nil &&
		bytes.Contains(diagnostic.Bytes(), []byte("CoreSimulatorService connection interrupted")) {
		// CoreSimulator can restart its XPC service during boot. A fresh
		// bootstatus reconnects to the service, waiting on the same device
		// if it is already starting. Leave the paired iPad undisturbed.
		return boot("Resume iPhone boot after CoreSimulator connection interruption")
	}
	return err
}

// Run each UI runner against the same broker. The independent CLI joins via
// its normal attach command. A unique marker from each writer is retained in
// the child's history so simulator launch order does not affect the assertion.
func shared(plans viewerPlans, arrivals <-chan string, abortBarriers func(), url, pipeID, pipeRun, terminalID string, command func(string, ...string) *exec.Cmd) error {
	// Includes the first native install on iPad, before other viewers launch.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
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
			if err := bootPreparedPhone(ctx, device.id); err != nil {
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
		if err := shutdownSimulator(ctx, "Shut down iPad after runner startup failure", id); err != nil {
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
