package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/forward"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/termio"
	"github.com/specialistvlad/clistream/internal/wire"
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
	end := flag.Bool("eof", false, "close shared child stdin at local EOF in pipe mode")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: clistream [flags] [PAIRING-LINK | list | attach ID | forward ID NAME LOOPBACK:PORT | ls ID [DIR] | get ID REMOTE LOCAL | put ID LOCAL REMOTE | pair]")
		fmt.Fprintln(flag.CommandLine.Output(), "First time: paste the command printed by clistreamd. Next time: run clistream to reconnect.")
		flag.PrintDefaults()
	}
	flag.Parse()
	action := flag.Arg(0)
	automatic := action == "" || strings.HasPrefix(action, "clistream://")
	if automatic && flag.NArg() > 1 {
		return 1, errors.New("supply only the pairing link")
	}
	var link string
	if automatic {
		link = action
		action = "attach"
	}
	// Reserve stderr for the attached child's error stream. Operators can opt into transport diagnostics.
	if action == "attach" {
		if _, set := os.LookupEnv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING"); !set {
			os.Setenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING", "true")
		}
	}
	if action == "pair" {
		if _, e := fmt.Fscanln(os.Stdin, &link); e != nil {
			return 1, e
		}
		_, _, err := identity.CLIProfile(context.Background(), *dir, "client", link)
		return 0, err
	}
	if action != "list" && action != "attach" && action != "forward" && action != "ls" && action != "get" && action != "put" {
		return 1, errors.New("usage: clistream [flags] pair | list | attach ID | forward ID NAME LOOPBACK:PORT | ls ID [DIR] | get ID REMOTE LOCAL | put ID LOCAL REMOTE")
	}
	if action == "attach" && !automatic && flag.NArg() != 2 {
		return 1, errors.New("usage: clistream attach ID")
	}
	if action == "attach" && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		return dashboard(*dir, link, flag.Arg(1))
	}
	directory, saved, e := identity.CLIProfile(context.Background(), *dir, "client", link)
	if e != nil {
		if automatic && link == "" && errors.Is(e, os.ErrNotExist) {
			return 1, errors.New("no saved server. Run clistream with the pairing link printed by clistreamd")
		}
		return 1, e
	}
	c, e := client.ConnectTo(directory, saved)
	if e != nil {
		return 1, e
	}
	defer c.CloseWithError(0, "")
	items, e := client.List(c)
	if e != nil {
		if automatic && strings.Contains(e.Error(), "client authorization required") {
			return 1, errors.New("device is no longer authorized. Request a fresh pairing link from clistreamd")
		}
		return 1, e
	}
	if automatic {
		if items == nil {
			items = []wire.Session{}
		}
		action = "list"
	}
	if action == "list" {
		return 0, json.NewEncoder(os.Stdout).Encode(items)
	}
	id := flag.Arg(1)
	for _, item := range items {
		if item.ID != id {
			continue
		}
		switch action {
		case "ls":
			path := flag.Arg(2)
			if path == "" {
				path = "."
			}
			return 0, client.Files(c, id, path, func(i wire.FileEntry) error { return json.NewEncoder(os.Stdout).Encode(i) })
		case "get":
			return 0, client.Get(c, id, flag.Arg(2), flag.Arg(3))
		case "put":
			return 0, client.Put(c, id, flag.Arg(3), flag.Arg(2))
		}
		if action == "forward" {
			name := flag.Arg(2)
			l, e := forward.Start(c, id, name, item.Ports[name], flag.Arg(3))
			if e != nil {
				return 1, e
			}
			defer l.Close()
			fmt.Fprintln(os.Stderr, "forwarding", l.Address)
			<-c.Context().Done()
			return 1, context.Cause(c.Context())
		}
		a, e := client.Attach(c, id)
		if e != nil {
			return 1, e
		}
		defer a.Close()
		ctx, detach := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer detach()
		stop := context.AfterFunc(ctx, func() { c.CloseWithError(0, "detached") })
		defer stop()
		interactive := item.Mode == "pty" && term.IsTerminal(int(os.Stdin.Fd()))
		if item.Mode == "pty" {
			defer termio.Setup(func(w, h uint16) { a.Send(wire.Message{Op: "resize", Cols: w, Rows: h}) })()
		}
		go func() {
			client.CopyInput(os.Stdin, func(b []byte) error {
				if interactive && bytes.Equal(b, []byte{0x1d}) {
					detach()
					return context.Canceled
				}
				return a.Send(wire.Message{Op: "data", Data: b})
			})
			if *end && item.Mode == "pipe" {
				a.Send(wire.Message{Op: "eof"})
			}
		}()
		code, e := a.Receive(func(ch string, b []byte) error {
			w := os.Stdout
			if strings.EqualFold(ch, "stderr") {
				w = os.Stderr
			}
			_, e := w.Write(b)
			return e
		})
		if ctx.Err() != nil {
			return 0, nil
		}
		return code, e
	}
	return 1, errors.New("unknown session")
}
