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
	"slices"
	"strings"
	"syscall"

	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/cliopts"
	"github.com/specialistvlad/clistream/internal/forward"
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
	end := flag.Bool("send-eof", false, "close shared child stdin at local EOF in pipe mode")
	flag.Usage = help
	cliopts.Parse()
	args := flag.Args()
	var link string
	if len(args) > 0 && strings.HasPrefix(args[0], "clistream://") {
		link, args = args[0], args[1:]
	}
	automatic := len(args) == 0
	action, id := "attach", ""
	var operands []string
	if !automatic {
		action = args[0]
		if _, global := commands[action]; global {
			if err := cliopts.Validate("clistream", args, commands); err != nil {
				return 1, err
			}
			if len(args) > 1 {
				id, operands = args[1], args[2:]
			}
		} else {
			if len(args) < 2 || args[0] == "" {
				return 1, errors.New("usage: clistream SESSION_ID ACTION [ARG ...]")
			}
			id, action, operands = args[0], args[1], args[2:]
			switch action {
			case "stop", "start", "restart":
				if len(operands) != 0 {
					return 1, errors.New("unexpected session action arguments")
				}
			case "edit":
				if len(operands) > 0 && operands[0] == "--" {
					operands = operands[1:]
				}
				if len(operands) == 0 {
					return 1, errors.New("usage: clistream SESSION_ID edit -- PROGRAM [ARG ...]")
				}
			case "fs":
				if err := validateFS(operands); err != nil {
					return 1, err
				}
			default:
				return 1, errors.New("usage: clistream SESSION_ID {start|stop|restart|edit|fs}")
			}
		}
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
		_, _, err := identity.CLIProfile(context.Background(), "client", link)
		return 0, err
	}
	if action == "attach" && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		return dashboard(link, id)
	}
	directory, saved, e := identity.CLIProfile(context.Background(), "client", link)
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
	caps, e := wire.Capabilities(c)
	if e != nil {
		return 1, e
	}
	rows, e := client.Supervisors(c, slices.Contains(caps, wire.ManagedSupervisors), slices.Contains(caps, wire.EditCommand), slices.Contains(caps, wire.RunResult))
	if e != nil {
		if automatic && strings.Contains(e.Error(), "client authorization required") {
			return 1, errors.New("device is no longer authorized. Request a fresh pairing link from clistreamd")
		}
		return 1, e
	}
	if automatic || action == "list" {
		type listing struct {
			wire.Session
			Run   string
			State string
		}
		items := make([]listing, 0, len(rows))
		for _, row := range rows {
			info := row.Info
			info.ID = row.ID
			items = append(items, listing{info, row.Info.ID, stateLabel(row)})
		}
		return 0, json.NewEncoder(os.Stdout).Encode(items)
	}
	for _, row := range rows {
		item := row.Info
		if id == "" || (row.ID != id && item.ID != id) {
			continue
		}
		switch action {
		case "start", "stop", "restart", "edit":
			if id != row.ID {
				return 1, errors.New("use the stable session ID from clistream list")
			}
			if action == "start" && item.ID != "" {
				return 1, errors.New("session is already running")
			}
			if (action == "stop" || action == "restart") && item.ID == "" {
				return 1, errors.New("session is stopped")
			}
			if row.Revision == 0 {
				return 1, errors.New("session was started in attach-only mode")
			}
			m := wire.Message{Op: action, ID: row.ID, Run: item.ID, Revision: row.Revision}
			if term.IsTerminal(int(os.Stdin.Fd())) {
				if cols, rows, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
					m.Cols, m.Rows = uint16(cols), uint16(rows)
				}
			}
			if action == "edit" {
				m.Op = "run-command"
				for _, arg := range operands {
					m.Command = append(m.Command, []byte(arg))
				}
			}
			_, err := wire.Request(c, m)
			return 0, err
		}
		id = item.ID
		if id == "" {
			return 1, errors.New("session is stopped")
		}
		if action == "fs" {
			return 0, runFS(c, id, operands)
		}
		if action == "forward" {
			name := operands[0]
			l, e := forward.Start(c, id, name, item.Ports[name], operands[1])
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
