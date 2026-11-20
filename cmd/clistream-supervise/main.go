package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/driver"
	"github.com/specialistvlad/clistream/internal/files"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/session"
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
	dir := flag.String("state", identity.Default("supervisor"), "identity directory")
	mode := flag.String("mode", "auto", "auto, pipe or pty")
	name := flag.String("driver", "auto", "auto, generic, codex or claude")
	listDrivers := flag.Bool("drivers", false, "list supported CLI versions and execution modes as JSON")
	pair := flag.Bool("pair", false, "read supervisor invitation from stdin")
	addr := flag.String("server", "", "override paired host:port (same pinned key)")
	local := flag.Bool("local-input", true, "forward local stdin (pipe EOF closes child stdin)")
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
	const usage = "Usage: clistream-supervise [flags] [SUPERVISOR-LINK] PROGRAM [ARG ...]"
	const inviteHelp = "Get a supervisor link on the server with clistreamd -qr=false invite supervisor."
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
			fmt.Fprintln(os.Stderr, "Paste the supervisor link and press Return.")
		}
		var link string
		if _, e := fmt.Fscanln(os.Stdin, &link); e != nil {
			return 1, e
		}
		return 0, identity.Pair(context.Background(), *dir, link, "supervisor")
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
	if _, e := exec.LookPath(path); e != nil {
		return 1, e
	}
	terminal := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	profile, e := driver.Select(args, *name, *mode, terminal)
	if e != nil {
		return 1, e
	}
	if link != "" {
		if e := identity.Pair(context.Background(), *dir, link, "supervisor"); e != nil {
			return 1, fmt.Errorf("pair supervisor: %w. %s", e, inviteHelp)
		}
	}
	saved, e := identity.Load(*dir)
	if errors.Is(e, os.ErrNotExist) {
		return 1, errors.New("This supervisor needs pairing. " + inviteHelp + "\nThen run clistream-supervise SUPERVISOR-LINK PROGRAM.")
	}
	if e != nil {
		return 1, e
	}
	if *addr != "" {
		saved.Address = *addr
	}
	host, _ := os.Hostname()
	exports := map[string]string{}
	for k, v := range ports {
		exports[strings.TrimPrefix(k, "port/")], _, _ = strings.Cut(v, ":")
	}
	info := wire.Session{Name: filepath.Base(path), Mode: profile.Mode, Version: profile.Version, Driver: profile.Name, Host: host, OS: runtime.GOOS, Ports: exports}
	reg := registration{*dir, saved, info}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, e := reg.connect(ctx)
	if e != nil {
		return 1, e
	}
	defer c.CloseWithError(0, "finished")
	root, e := files.Open(".")
	if e != nil {
		return 1, e
	}
	defer root.Close()
	p, e := session.Start(path, args[1:], profile.Mode, ports, root)
	if e != nil {
		return 1, e
	}
	if terminal {
		fmt.Fprintf(os.Stderr, "Sharing %q through %s (%s). Open clistream to attach.\n", path, saved.Address, profile.Mode)
	}
	done := make(chan struct{})
	go func() { defer close(done); reg.serve(ctx, p, c) }()
	defer func() { cancel(); <-done }()
	ctxSig, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	go func() { <-ctxSig.Done(); p.Cmd.Process.Signal(syscall.SIGTERM) }()
	if *local {
		if profile.Mode == "pty" && term.IsTerminal(int(os.Stdin.Fd())) {
			defer termio.Setup(func(w, h uint16) { p.Resize(w, h) })()
		}
		go func() {
			if e := client.CopyInput(os.Stdin, p.Input); e != nil && profile.Mode == "pipe" {
				p.EndInput()
			}
		}()
	}
	return p.Wait(), nil
}
