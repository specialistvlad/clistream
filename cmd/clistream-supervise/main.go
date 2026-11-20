package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"

	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/activity"
	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/driver"
	"github.com/specialistvlad/clistream/internal/files"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/termio"
	"github.com/specialistvlad/clistream/internal/wire"
	"golang.org/x/term"
)

func main() {
	code, e := run()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	os.Exit(code)
}
func run() (int, error) {
	dir := flag.String("state", "", "independent identity directory (default: shared per OS account)")
	mode := flag.String("mode", "auto", "auto, pipe or pty")
	name := flag.String("driver", "generic", "generic, or opt in to claude, codex or name-based auto selection")
	listDrivers := flag.Bool("drivers", false, "list supported CLI versions and execution modes as JSON")
	pair := flag.Bool("pair", false, "read pairing invitation from stdin")
	addr := flag.String("server", "", "override paired host:port (same pinned key)")
	once := flag.Bool("once", false, "exit with the first child's status")
	flag.BoolVar(once, "no-remote-restart", false, "share one attach-only run and exit with its status (alias for -once)")
	interactive := flag.Bool("interactive", false, "show the child terminal or use child streams when redirected")
	local := flag.Bool("local-input", true, "forward local stdin with --interactive (pipe EOF closes child stdin)")
	ports := map[string]string{}
	flag.Func("export", "name=tcp:127.0.0.1:port or name=udp:[::1]:port", func(v string) error {
		k, s, ok := strings.Cut(v, "=")
		proto, a, _ := strings.Cut(s, ":")
		h, _, e := net.SplitHostPort(a)
		if !ok || k == "" || e != nil || !net.ParseIP(h).IsLoopback() || (proto != "tcp" && proto != "udp") {
			return errors.New("invalid loopback export")
		}
		ports["port/"+k] = s
		return nil
	})
	const usage = "Usage: clistream-supervise [flags] [PAIRING-LINK] PROGRAM [ARG ...]"
	const inviteHelp = "Use the link displayed by clistreamd or printed by clistreamd invite."
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), usage)
		fmt.Fprintln(flag.CommandLine.Output(), inviteHelp)
		flag.PrintDefaults()
	}
	flag.Parse()
	if *listDrivers {
		return 0, json.NewEncoder(os.Stdout).Encode(driver.Catalog())
	}
	if *pair {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprintln(os.Stderr, "Paste the pairing link and press Return.")
		}
		var link string
		if _, e := fmt.Fscanln(os.Stdin, &link); e != nil {
			return 1, e
		}
		_, _, err := identity.CLIProfile(context.Background(), *dir, "supervisor", link)
		return 0, err
	}
	args := flag.Args()
	var link string
	if len(args) > 0 && strings.HasPrefix(args[0], "clistream://") {
		link, args = args[0], args[1:]
		if len(args) > 0 && args[0] == "--" {
			args = args[1:]
		}
	}
	if len(args) == 0 {
		return 1, errors.New(usage + "\nExample: clistream-supervise /bin/sh\n" + inviteHelp)
	}
	path := args[0]

	terminal := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	profile, e := driver.Select(args, *name, *mode, terminal)
	if e != nil {
		return 1, e
	}
	directory, saved, e := identity.CLIProfile(context.Background(), *dir, "supervisor", link)
	if link == "" && errors.Is(e, os.ErrNotExist) {
		return 1, errors.New("This supervisor needs pairing. " + inviteHelp + "\nThen run clistream-supervise PAIRING-LINK PROGRAM.")
	}
	if e != nil {
		return 1, e
	}
	savedAddress := saved.Address
	if *addr != "" {
		saved.Address = *addr
	}
	host, _ := os.Hostname()
	exports := map[string]string{}
	for k, v := range ports {
		exports[strings.TrimPrefix(k, "port/")], _, _ = strings.Cut(v, ":")
	}
	info := wire.Session{Name: filepath.Base(path), Mode: profile.Mode, Version: profile.Version, Driver: profile.Name, Host: host, OS: runtime.GOOS, Ports: exports}
	reg := registration{dir: directory, server: saved, info: info}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	root, e := files.Open(".")
	if e != nil {
		return 1, e
	}
	defer root.Close()
	console, closeConsole := startConsole(ctx, cancel, terminal && *interactive, *local, info.Name, saved.Address)
	reg.console = console
	defer closeConsole()
	instance := uniqueID()
	console.logOnly = !*interactive
	if console.logOnly {
		signal.Ignore(syscall.SIGPIPE)
		if terminal {
			restore, err := termio.QuietInput()
			if err != nil {
				return 1, err
			}
			defer restore()
		}
	}
	console.openJournal(directory, instance)
	defer console.journal.Close()
	localInput := *interactive && *local
	state := wire.Supervisor{ID: instance, Revision: 1, Info: info}
	managed := false
	c, e := retry(ctx, reg.console, 0, func(ctx context.Context) (*quic.Conn, error) {
		if !*once {
			probe, err := identity.Dial(ctx, reg.dir, reg.server)
			if err != nil {
				return nil, err
			}
			stop := abortOnCancel(ctx, probe)
			defer stop()
			managed, err = wire.Managed(probe)
			if err == nil && managed {
				err = publishState(probe, state)
			}
			if err == nil && managed {
				console.record(activity.Event{Name: "registered"})
				return probe, nil
			}
			probe.CloseWithError(0, "discovery finished")
			if err != nil {
				return nil, err
			}
			if reg.console.program == nil && !console.logOnly {
				fmt.Fprintln(os.Stderr, "Server supports attach only. Using one-shot supervision.")
			}
		}
		c, err := reg.connect(ctx)
		if errors.Is(err, errLegacyRole) && *dir == "" && directory == identity.Default("client") && link == "" {
			legacy := identity.Default("supervisor")
			if original, e := identity.Load(legacy); e == nil && original.Pin == saved.Pin && original.Address == savedAddress {
				reg.dir = legacy
				return reg.connect(ctx)
			}
		}
		return c, err
	})
	if ctx.Err() != nil {
		if c != nil {
			c.CloseWithError(0, "finished")
		}
		return 0, nil
	}
	if e != nil {
		console.record(activity.Event{Name: "connection_failed"})
		return 1, e
	}
	defer c.CloseWithError(0, "finished")
	if managed {
		return supervise(ctx, reg, c, state, path, args[1:], ports, root, localInput, launchSettings{*name, *mode, terminal})
	}
	if console.program != nil {
		console.program.Send(attachOnly{})
	}
	runID := console.lastSession
	p, e := reg.console.start(runID, path, args[1:], profile.Mode, ports, root, false, info.Name)
	if e != nil {
		if console.logOnly {
			return 1, nil
		}
		return 1, e
	}
	done := make(chan struct{})
	go func() { defer close(done); reg.serve(ctx, p, c) }()
	defer func() { cancel(); <-done }()
	stop := context.AfterFunc(ctx, func() { p.Cmd.Process.Signal(syscall.SIGTERM) })
	defer stop()
	if localInput && !terminal {
		if profile.Mode == "pty" && term.IsTerminal(int(os.Stdin.Fd())) {
			defer termio.Setup(func(w, h uint16) { p.Resize(w, h) })()
		}
		go func() {
			if e := client.CopyInput(os.Stdin, p.Input); e != nil && profile.Mode == "pipe" {
				p.EndInput()
			}
		}()
	}
	code := p.Wait()
	console.exit(runID, code)
	return code, nil
}
