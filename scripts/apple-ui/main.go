// Command apple-ui runs a shipping app's XCTest interactions against private Go peers.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/specialistvlad/clistream/internal/identity"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func firstInvitation(root string, command *exec.Cmd) ([]byte, error) {
	// On a new profile the first admin invocation and the server can otherwise
	// compete for daemon.lock. Only a serving daemon publishes admin.sock.
	config := root
	if runtime.GOOS == "darwin" {
		config = filepath.Join(root, "Library", "Application Support")
	}
	socket := filepath.Join(config, "clistream", "server", "admin.sock")
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); {
		info, err := os.Stat(socket)
		if err == nil && info.Mode()&os.ModeSocket != 0 {
			return command.Output()
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		time.Sleep(80 * time.Millisecond)
	}
	// Do not include raw stderr, pairing links, or private state in CI logs.
	log, _ := os.ReadFile(filepath.Join(root, "broker-stderr"))
	lower := bytes.ToLower(log)
	for _, cause := range []string{"no space left on device", "permission denied", "operation not permitted",
		"address already in use", "identity directory must be private", "too many open files",
		"invalid advertised endpoint", "cannot allocate memory", "file name too long"} {
		if bytes.Contains(lower, []byte(cause)) {
			return nil, fmt.Errorf("broker did not publish its administration socket: %s", cause)
		}
	}
	_, lockErr := os.Stat(filepath.Join(filepath.Dir(socket), "daemon.lock"))
	return nil, fmt.Errorf("broker did not publish its administration socket (created lock: %t, wrote diagnostics: %t)",
		lockErr == nil, len(log) > 0)
}

// Reconcile the test fixture through the public CLI. A supervisor may still
// be registering its new run when the previous UI case exits.
func preparePipe(ctx context.Context, id string, cli func(context.Context, ...string) ([]byte, error)) error {
	last := "supervisor unavailable"
	started := false
	for {
		query, cancel := context.WithTimeout(ctx, 10*time.Second)
		data, err := cli(query, "list")
		cancel()
		var rows []struct{ ID, Run string }
		switch {
		case err != nil:
			last = "discovery unavailable"
		case json.Unmarshal(data, &rows) != nil:
			return errors.New("invalid discovery response")
		default:
			last = "supervisor unavailable"
			for _, row := range rows {
				if row.ID != id {
					continue
				}
				if row.Run != "" {
					return nil
				}
				last = "waiting for the new run"
				if !started {
					start, cancel := context.WithTimeout(ctx, 10*time.Second)
					_, err := cli(start, id, "start")
					cancel()
					started = err == nil
					if err != nil {
						last = "start unavailable"
					}
				}
				break
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w", last, ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: apple-ui COMMAND [ARG ...]")
	}
	if args[0] == "terminal-child" {
		return terminalChild()
	}
	bin, err := filepath.Abs("bin")
	if err != nil {
		return err
	}
	root, err := os.MkdirTemp("/tmp", "cs-ui-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	if args[0] == "shared" {
		sharedCheckpoint("Start the two brokers and register supervised children before booting simulators")
	}
	commandIn := func(ctx context.Context, tool string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, filepath.Join(bin, tool), args...)
		cmd.Env = append(os.Environ(), "HOME="+root, "XDG_CONFIG_HOME="+root)
		cmd.Dir = root
		return cmd
	}
	command := func(tool string, args ...string) *exec.Cmd { return commandIn(context.Background(), tool, args...) }
	launch := func(cmd *exec.Cmd) (func(), error) {
		if filepath.Base(cmd.Path) == "clistreamd" {
			log, err := os.OpenFile(filepath.Join(cmd.Dir, "broker-stderr"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return nil, err
			}
			defer log.Close()
			cmd.Stderr = log
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return func() {
			_ = cmd.Process.Signal(os.Interrupt)
			// A suspended fixture must not prevent XCTest results from being
			// collected after the client tests have already finished.
			kill := time.AfterFunc(5*time.Second, func() { _ = cmd.Process.Kill() })
			_ = cmd.Wait()
			kill.Stop()
		}, nil
	}
	brokerArgs := []string{"--listen", "127.0.0.1:0", "--qr=false"}
	if port := os.Getenv("CLISTREAM_UI_BROKER_PORT"); port != "" {
		brokerArgs = []string{"--listen", ":" + port, "--advertise", "127.0.0.1:" + port, "--qr=false"}
	}
	cleanup, err := launch(command("clistreamd", brokerArgs...))
	if err != nil {
		return err
	}
	defer cleanup()
	link, err := firstInvitation(root, command("clistreamd", "invite"))
	if err != nil {
		return fmt.Errorf("primary broker did not issue an invitation: %w", err)
	}
	exports, closeServices, err := reverseServices()
	if err != nil {
		return err
	}
	defer closeServices()
	// Register children in order and remember the stable supervisor and initial run.
	type registration struct{ ID, Run string }
	seen := map[string]bool{}
	startSupervisor := func(args ...string) (registration, func(), error) {
		stop, err := launch(command("clistreams", args...))
		if err != nil {
			return registration{}, nil, err
		}
		for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); {
			data, err := command("clistream", "list").Output()
			var rows []registration
			if err == nil && json.Unmarshal(data, &rows) == nil {
				for _, row := range rows {
					if row.Run != "" && !seen[row.ID] {
						seen[row.ID] = true
						return row, stop, nil
					}
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		stop()
		return registration{}, nil, errors.New("supervised child did not register")
	}
	const echoScript = `echo "copyword selection"
echo preview-ready
printf "args=%s\nfirst=<%s>\nsecond=<%s>\n" "$#" "$1" "$2"
while IFS= read -r line; do echo "out:$line"; echo "err:$line" >&2; done
`
	if err := os.WriteFile(filepath.Join(root, "echo.sh"), []byte(echoScript), 0600); err != nil {
		return err
	}
	// Finder enumerates every enabled workspace. Keep fixture keys and invitations
	// in the private parent, outside the directory any test can browse.
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		return err
	}
	pipe, stop, err := startSupervisor(append(exports, "--workspace", workspace, "--io-mode", "pipe", string(bytes.TrimSpace(link)), "/bin/sh", "echo.sh")...)
	if err != nil {
		return err
	}
	defer stop()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	terminal, stop, err := startSupervisor("--files=FALSE", "--once", "--io-mode", "pty", "--", exe, "terminal-child")
	if err != nil {
		return err
	}
	defer stop()
	var measured, eof, touch registration
	var shell, compare registration
	if os.Getenv("CLISTREAM_MAC_BOARD") == "1" {
		// Use installed programs for the Mac boards. The shell supplies the PTY
		// window and comm stays running while its input stream is open.
		shell, stop, err = startSupervisor("--files=FALSE", "--io-mode", "pty", "--", "/bin/sh", "-i")
		if err != nil {
			return err
		}
		defer stop()
		compare, stop, err = startSupervisor("--files=FALSE", "--io-mode", "pipe", "--", "/usr/bin/comm", "-", "/dev/null")
		if err != nil {
			return err
		}
		defer stop()
	}
	if os.Getenv("SIMULATOR_ID") != "" && args[0] != "shared" {
		// Keep the selectable paragraph stable while UIKit moves the selection handles.
		touch, stop, err = startSupervisor("--files=FALSE", "--once", "--io-mode", "pipe", "--", "/bin/sh", "-c",
			`printf "copyword selection\n"; while IFS= read -r line; do printf "out:%s\n" "$line" >&2; done`)
		if err != nil {
			return err
		}
		defer stop()
	}
	if os.Getenv("CLISTREAM_HOSTED_TESTS") == "1" {
		measured, stop, err = startSupervisor("--files=FALSE", "--once", "--io-mode", "pty", "--", "/bin/sh", "-c", `stty -echo; read word; stty size; printf '\033[32m%s\033[0m\n' "$word"; exit 23`)
		if err != nil {
			return err
		}
		defer stop()
		eof, stop, err = startSupervisor("--files=FALSE", "--io-mode", "pipe", "--", "/bin/cat")
		if err != nil {
			return err
		}
		defer stop()
	}
	bind := "127.0.0.1:0"
	if port := os.Getenv("CLISTREAM_UI_FIXTURE_PORT"); port != "" {
		bind = ":" + port
	}
	listener, err := net.Listen("tcp", bind)
	if err != nil {
		return err
	}
	var secret [24]byte
	if _, err = rand.Read(secret[:]); err != nil {
		return err
	}
	path := "/" + hex.EncodeToString(secret[:]) + "/"
	mux := http.NewServeMux()
	finderRoutes(mux, path, workspace)
	mobileFileRoutes(mux, path, workspace)
	// Cases share the same published supervisor. An interrupted lifecycle or
	// Finder case may leave its child stopped. Restore the fixture before the
	// next case, without replacing a running child or discarding its replay.
	mux.HandleFunc(path+"prepare-pipe", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		err := preparePipe(ctx, pipe.ID, func(ctx context.Context, args ...string) ([]byte, error) {
			return commandIn(ctx, "clistream", args...).Output()
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "Prepare supervised pipe:", err)
			http.Error(w, "fixture pipe unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "ready")
	})
	if args[0] == "shared" || os.Getenv("CLISTREAM_HOSTED_TESTS") == "1" {
		stopSecond, err := secondServer(root, bin, mux, path, launch)
		if err != nil {
			return err
		}
		defer stopSecond()
	}
	// Keep the fixture's own enrollment when testing removal of viewer credentials.
	devices := func() (map[string]bool, error) {
		raw, err := command("clistreamd", "devices").Output()
		keys := make(map[string]bool)
		for _, line := range strings.Split(string(raw), "\n") {
			if fields := strings.Fields(line); len(fields) == 2 {
				keys[fields[0]] = true
			}
		}
		return keys, err
	}
	initial, err := devices()
	if err != nil {
		return err
	}
	mux.HandleFunc(path+"REVOKE", func(w http.ResponseWriter, r *http.Request) {
		keys, err := devices()
		if err == nil {
			for key := range keys {
				if !initial[key] {
					if err = command("clistreamd", "revoke", key).Run(); err != nil {
						break
					}
				}
			}
		}
		if err != nil {
			http.Error(w, "revocation failed", 500)
			return
		}
		fmt.Fprint(w, "revoked")
	})
	if os.Getenv("CLISTREAM_DUO_NAME") != "" {
		poseRoute(mux, path, os.Getenv("SIMULATOR_ID"), filepath.Join(root, "keys"))
	}
	if err := interactionRoutes(mux, path, root, link); err != nil {
		return err
	}
	var arrivals <-chan string
	aborted := make(chan struct{})
	if args[0] == "shared" {
		arrivals = rendezvous(mux, path+"ready/", aborted)
		rendezvous(mux, path+"switch/", aborted)
		rendezvous(mux, path+"return/", aborted)
		sharedMacStages(mux, path+"mac-stage/")
	}
	issue := func(w http.ResponseWriter, r *http.Request) {
		data, err := command("clistreamd", "invite").Output()
		if err != nil {
			http.Error(w, "fixture unavailable", 503)
			return
		}
		if r.URL.Path == path+"open-repair-invitation" {
			id := os.Getenv("SIMULATOR_ID")
			// Deliver as an external link to the live scene. XCUIApplication's
			// open(URL) is a launch operation and can replace that scene.
			if err := exec.CommandContext(r.Context(), "xcrun", "simctl", "openurl", id, strings.TrimSpace(string(data))).Run(); err != nil {
				http.Error(w, "unable to deliver invitation to the simulator", http.StatusServiceUnavailable)
				return
			}
			fmt.Println("Delivered the fresh repair invitation through the simulator URL dispatcher.")
			fmt.Fprint(w, "opened")
			return
		}
		w.Write(data)
	}
	mux.HandleFunc(path+"files/provider-state", func(w http.ResponseWriter, r *http.Request) {
		id := os.Getenv("SIMULATOR_ID")
		if id == "" {
			fmt.Fprint(w, "Simulator diagnostics are collected on the Mac host.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "bash", "apple/inspect-mobile-provider.sh", id)
		cmd.Stdout = w
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(w, "Simulator inspection interrupted.")
		}
	})
	if id := os.Getenv("SIMULATOR_ID"); id != "" {
		mux.HandleFunc(path+"open-repair-invitation", issue)
		prepareInput := func(w http.ResponseWriter, r *http.Request) {
			var value []byte
			switch filepath.Base(r.URL.Path) {
			case "invitation":
				data, err := command("clistreamd", "invite").Output()
				if err != nil {
					http.Error(w, "invitation unavailable", http.StatusServiceUnavailable)
					return
				}
				value = bytes.TrimSpace(data)
			case "invalid":
				value = []byte("https://example.invalid/")
			case "clear":
			default:
				http.NotFound(w, r)
				return
			}
			if strings.HasPrefix(r.URL.Path, path+"scan-sample/") {
				folder, err := exec.CommandContext(r.Context(), "xcrun", "simctl", "get_app_container", id, "com.vlk.clistream.acceptance", "data").Output()
				if err != nil || os.WriteFile(filepath.Join(strings.TrimSpace(string(folder)), "tmp", "uitest-scanned-qr"), value, 0600) != nil {
					http.Error(w, "scanner sample unavailable", http.StatusServiceUnavailable)
					return
				}
				fmt.Fprint(w, "ready")
				return
			}
			// The XCTest runner is a background process. Set the clipboard on the
			// selected simulator and verify its actual contents before tapping Paste.
			copy := exec.CommandContext(r.Context(), "xcrun", "simctl", "pbcopy", id)
			copy.Stdin = bytes.NewReader(value)
			if err := copy.Run(); err != nil {
				http.Error(w, "simulator pasteboard unavailable", http.StatusServiceUnavailable)
				return
			}
			if len(value) > 0 {
				stored, err := exec.CommandContext(r.Context(), "xcrun", "simctl", "pbpaste", id).Output()
				if err != nil || !bytes.Equal(stored, value) {
					http.Error(w, "simulator pasteboard did not retain the text", http.StatusServiceUnavailable)
					return
				}
			}
			fmt.Fprint(w, "ready")
		}
		mux.HandleFunc(path+"pasteboard/", prepareInput)
		mux.HandleFunc(path+"scan-sample/", prepareInput)
	}
	for _, op := range []string{"QR", "PIPE", "PORTS", "PTY", "FLUSH", "DRIVERS"} {
		mux.HandleFunc(path+op, issue)
	}
	mux.HandleFunc(path+"FLUSHED", func(w http.ResponseWriter, r *http.Request) {
		cli := exec.CommandContext(r.Context(), command("clistream").Path, "attach", pipe.Run)
		cli.Env = command("clistream").Env
		reader, err := cli.StdoutPipe()
		if err != nil {
			http.Error(w, "observer unavailable", 500)
			return
		}
		if err = cli.Start(); err != nil {
			http.Error(w, "observer unavailable", 500)
			return
		}
		defer func() { cli.Process.Kill(); cli.Wait() }()
		lines := bufio.NewScanner(reader)
		for lines.Scan() {
			if lines.Text() == "out:before-detach" {
				fmt.Fprint(w, "received")
				return
			}
		}
		http.Error(w, "last input missing", 409)
	})
	server := http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Close()
	url := fmt.Sprintf("http://127.0.0.1:%d%s", listener.Addr().(*net.TCPAddr).Port, path)
	if args[0] == "shared" {
		// Pair supervisors while the machine is idle, before simulator
		// migration and Xcode builds compete with broker initialization.
		// Each actual viewer receives a newly minted invitation via HTTP.
		plans, err := prepareViewers(root)
		if err != nil {
			return err
		}
		return shared(plans, arrivals, func() { close(aborted) }, url, pipe.ID, pipe.Run, terminal.ID, command)
	}
	test := exec.Command(args[0], args[1:]...)
	test.Stdout = os.Stdout
	test.Stderr = os.Stderr
	invitation, err := identity.ParseInvitation(string(bytes.TrimSpace(link)))
	if err != nil {
		return err
	}
	test.Env = append(os.Environ(), "TEST_RUNNER_CLISTREAM_ENDPOINT="+invitation.Address, "TEST_RUNNER_CLISTREAM_SESSION="+pipe.ID, "TEST_RUNNER_CLISTREAM_UI_PTY="+terminal.ID, "TEST_RUNNER_CLISTREAM_PTY_SESSION="+measured.ID, "TEST_RUNNER_CLISTREAM_PIPE_RUN="+pipe.Run, "TEST_RUNNER_CLISTREAM_FIXTURE="+url, "TEST_RUNNER_CLISTREAM_DUO_NAME="+os.Getenv("CLISTREAM_DUO_NAME"), "TEST_RUNNER_CLISTREAM_TOUCH_SESSION="+touch.ID, "TEST_RUNNER_CLISTREAM_EOF_ID="+eof.ID, "TEST_RUNNER_CLISTREAM_PIPE_ID="+pipe.ID, "TEST_RUNNER_CLISTREAM_PTY_ID="+terminal.ID)
	test.Env = append(test.Env, "TEST_RUNNER_CLISTREAM_SHELL_ID="+shell.ID, "TEST_RUNNER_CLISTREAM_SEARCH_ID="+compare.ID)
	return test.Run()
}
