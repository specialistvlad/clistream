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
	flag.Parse()
	if *listDrivers {
		return 0, json.NewEncoder(os.Stdout).Encode(driver.Catalog())
	}
	if *pair {
		var link string
		if _, e := fmt.Fscanln(os.Stdin, &link); e != nil {
			return 1, e
		}
		return 0, identity.Pair(context.Background(), *dir, link, "supervisor")
	}
	if flag.NArg() == 0 {
		return 1, errors.New("supply executable and arguments after --")
	}
	path := flag.Arg(0)
	profile, e := driver.Select(path, *name, *mode)
	if e != nil {
		return 1, e
	}
	saved, e := identity.Load(*dir)
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
	p, e := session.Start(path, flag.Args()[1:], profile.Mode, ports, root)
	if e != nil {
		return 1, e
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
