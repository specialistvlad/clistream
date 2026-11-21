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

// Run each UI runner against the same broker. The independent CLI joins via
// its normal attach command. A unique marker from each writer is retained in
// the child's history so simulator launch order does not affect the assertion.
func shared(root, url, pipeID, pipeRun, terminalID string, command func(string, ...string) *exec.Cmd) error {
	cli := command("clistream", "attach", pipeRun)
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
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
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
		results <- fmt.Errorf("CLI missed a participant")
	}()
	for _, name := range []string{"mac", "phone", "pad"} {
		dest := "platform=macOS"
		scheme := "macOS"
		suite := "CLIStream-MacUITests"
		if name != "mac" {
			key := map[string]string{"phone": "IPHONE_SIMULATOR_ID", "pad": "IPAD_SIMULATOR_ID"}[name]
			if os.Getenv(key) == "" {
				return fmt.Errorf("%s required", key)
			}
			dest = "platform=iOS Simulator,id=" + os.Getenv(key)
			scheme = "iOS"
			suite = "CLIStream-UITests"
		}
		test := exec.CommandContext(ctx, "xcodebuild", "-quiet", "-project", "apple/CLIStream.xcodeproj", "-scheme", "CLIStream-"+scheme, "-derivedDataPath", filepath.Join(root, "build-"+name), "-destination", dest, "CLISTREAM_APP_ID=com.vlk.clistream.acceptance.shared", "-parallel-testing-enabled", "NO", "-collect-test-diagnostics", "never", "-resultBundlePath", filepath.Join("apple/build", fmt.Sprintf("shared-%d-%s.xcresult", time.Now().Unix(), name)), "-only-testing:"+suite+"/NativeSessionTests/testConcurrentViewers", "test")
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
