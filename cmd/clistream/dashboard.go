package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/wire"
)

type connectionResult struct {
	conn    *quic.Conn
	managed bool
	err     error
}
type dashboardUI struct {
	screen             *uv.TerminalScreen
	page, host, notice string
	frame              int
	rows               []wire.Supervisor
	index              int
	target             wire.Supervisor
	action             string
	confirm            bool
	args               []string
	arg, cursor        int
	conn               *quic.Conn
	managed            bool
	attachment         *viewport
}

func (d *dashboardUI) refresh() error {
	var rows []wire.Supervisor
	if d.managed {
		res, err := wire.Request(d.conn, wire.Message{Op: "supervisors"})
		if err != nil {
			return err
		}
		rows = res.Supervisors
	} else {
		items, err := client.List(d.conn)
		if err != nil {
			return err
		}
		for _, v := range items {
			rows = append(rows, wire.Supervisor{ID: v.ID, Info: v})
		}
	}
	var id string
	if len(d.rows) > 0 {
		id = d.rows[d.index].ID
	}
	d.index = 0
	for i, v := range rows {
		if v.ID == id {
			d.index = i
			break
		}
	}
	d.rows = rows
	return nil
}
func (d *dashboardUI) request(op string) (wire.Message, error) {
	m := wire.Message{Op: op, ID: d.target.ID, Run: d.target.Info.ID, Revision: d.target.Revision}
	if op == "apply" {
		m.Args = d.args
	}
	return wire.Request(d.conn, m)
}
func (d *dashboardUI) apply() {
	_, err := d.request(d.action)
	d.page = "table"
	d.args = nil
	d.notice = ""
	if err != nil {
		d.notice = "Request failed. Check current state."
	}
	if err = d.refresh(); err != nil {
		d.disconnect(err)
	}
}
func (d *dashboardUI) detach() {
	if v := d.attachment; v != nil {
		v.close()
		d.attachment = nil
	}
	d.screen.SetMouseMode(uv.MouseModeNone)
	d.page = "table"
}
func (d *dashboardUI) disconnect(err error) {
	d.detach()
	if d.conn != nil {
		d.conn.CloseWithError(0, "disconnected")
		d.conn = nil
	}
	d.page = "lost"
	d.notice = err.Error()
	d.rows = nil
	d.args = nil
}
func (d *dashboardUI) attach(target wire.Supervisor) error {
	a, err := client.Attach(d.conn, target.Info.ID)
	if err != nil {
		return err
	}
	if d.attachment != nil {
		d.attachment.close()
	}
	d.target = target
	d.attachment = newViewport(a, d.target.Info.Mode, d.screen.Width(), max(1, d.screen.Height()-2))
	d.screen.SetMouseEncoding(uv.MouseEncodingSGR)
	d.screen.SetMouseMode(uv.MouseModeMotion)
	d.page = "attached"
	d.notice = ""
	return nil
}

// Cycle in dashboard order. Keep the old attachment if the chosen run exits
// between discovery and opening its streams.
func (d *dashboardUI) cycle(direction int) bool {
	if err := d.refresh(); err != nil {
		d.disconnect(err)
		return true
	}
	n := len(d.rows)
	current := slices.IndexFunc(d.rows, func(row wire.Supervisor) bool { return row.ID == d.target.ID })
	if current < 0 {
		return false
	}
	for step := 1; step < n; step++ {
		i := (current + step*direction + n) % n
		if d.rows[i].Info.ID == "" {
			continue
		}
		if err := d.attach(d.rows[i]); err != nil {
			d.notice = "Switch failed"
			return false
		}
		d.index = i
		return true
	}
	return false
}

func dashboard(dir, link, direct string) (int, error) {
	fd := int(os.Stdin.Fd())
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer cancel()
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	original, err := term.MakeRaw(fd)
	if err != nil {
		return 1, err
	}
	defer term.Restore(fd, original)
	reader, err := cancelreader.NewReader(os.Stdin)
	if err != nil {
		return 1, err
	}
	defer reader.Close()
	events := make(chan uv.Event)
	readDone := make(chan error, 1)
	go func() {
		readDone <- uv.NewTerminalReader(reader, os.Getenv("TERM")).StreamEvents(ctx, events)
		close(events)
	}()
	defer func() {
		cancel()
		reader.Cancel()
		for range events {
		}
	}()
	s := uv.NewTerminalScreen(os.Stdout, os.Environ())
	defer func() { s.Reset(); s.Flush() }()
	w, h, _ := term.GetSize(fd)
	if w < 1 || h < 1 {
		w, h = 80, 24
	}
	s.Resize(w, h)
	s.EnterAltScreen()
	s.EnableBracketedPaste()
	s.SetKeyboardEnhancements(&uv.KeyboardEnhancements{DisambiguateEscapeCodes: true})
	if err := s.Flush(); err != nil {
		return 1, err
	}
	d := dashboardUI{screen: s, host: "server"}
	if link != "" {
		if invite, err := identity.ParseInvitation(link); err == nil {
			d.host = invite.Address
		}
	} else if _, p, err := identity.CLIProfile(ctx, dir, "client", ""); err == nil {
		d.host = p.Address
	}
	defer func() {
		d.detach()
		if d.conn != nil {
			d.conn.CloseWithError(0, "finished")
		}
	}()
	ready := make(chan connectionResult)
	connect := func() {
		d.page = "connecting"
		d.notice = ""
		invitation := link
		go func() {
			directory, p, err := identity.CLIProfile(ctx, dir, "client", invitation)
			var result connectionResult
			if err == nil {
				result.conn, err = identity.Dial(ctx, directory, p)
			}
			if err == nil {
				result.managed, err = wire.Managed(result.conn)
			}
			if errors.Is(err, os.ErrNotExist) {
				err = errors.New("No saved server. Run clistream PAIRING-LINK using the link from clistreamd.")
			}
			result.err = err
			select {
			case ready <- result:
			case <-ctx.Done():
				if result.conn != nil {
					result.conn.CloseWithError(0, "finished")
				}
			}
		}()
	}
	d.page = "connecting"
	if err := d.draw(); err != nil {
		return 1, err
	}
	connect()
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	var refreshAt time.Time
	dirty := true
	for {
		var lost <-chan struct{}
		if d.conn != nil {
			lost = d.conn.Context().Done()
		}
		var output <-chan terminalRecord
		if d.attachment != nil {
			output = d.attachment.output
		}
		select {
		case <-ctx.Done():
			return 0, nil
		case result := <-ready:
			d.conn = result.conn
			if result.err != nil {
				d.disconnect(result.err)
				break
			}
			link = ""
			d.managed = result.managed
			d.page = "table"
			if err := d.refresh(); err != nil {
				d.disconnect(err)
				break
			}
			if direct != "" {
				i := slices.IndexFunc(d.rows, func(v wire.Supervisor) bool { return v.Info.ID == direct })
				if i < 0 {
					return 1, errors.New("unknown session")
				}
				d.index = i
				d.target = d.rows[i]
				if err := d.attach(d.target); err != nil {
					return 1, err
				}
			}
		case <-lost:
			direct = ""
			d.disconnect(errors.New("Connection closed"))
		case rec := <-output:
			if rec.done {
				if d.conn.Context().Err() != nil {
					direct = ""
					d.disconnect(errors.New("Connection closed"))
					break
				}
				if direct != "" {
					return rec.code, rec.err
				}
				d.detach()
				d.notice = fmt.Sprintf("Child exited · exit %d", rec.code)
				if rec.err != nil {
					d.notice = "Attachment ended"
				}
				if err := d.refresh(); err != nil {
					d.disconnect(err)
				}
			} else {
				d.attachment.term.Write(rec.data)
				dirty = true
				continue
			}
		case now := <-ticker.C:
			d.frame++
			if d.page == "table" && now.After(refreshAt) {
				if err := d.refresh(); err != nil {
					d.disconnect(err)
				}
				refreshAt = now.Add(time.Second)
				dirty = true
			}
			if !dirty && d.page != "connecting" {
				continue
			}
		case <-winch:
			w, h, err := term.GetSize(fd)
			if err == nil && w > 0 && h > 0 {
				s.Resize(w, h)
				if d.attachment != nil {
					d.attachment.resize(w, max(1, h-2))
				}
			}
		case event, ok := <-events:
			if !ok {
				return 0, <-readDone
			}
			if d.conn != nil && d.conn.Context().Err() != nil {
				direct = ""
				d.disconnect(errors.New("Connection closed"))
				break
			}
			switch ev := event.(type) {
			case uv.PasteEvent:
				if d.page == "attached" {
					d.attachment.term.Paste(ev.Content)
					continue
				} else if d.page == "edit" {
					d.appendText(ev.Content)
				}
			case uv.MouseEvent:
				if d.page == "attached" {
					d.attachment.mouse(ev)
				}
				continue
			case uv.KeyPressEvent:
				k := strings.ToLower(ev.String())
				switch d.page {
				case "attached":
					switch {
					case ev.MatchString("ctrl+]") || ev.Code == 0x1d:
						if d.cycle(1) {
							direct = ""
						}
					case ev.MatchString("ctrl+["):
						if d.cycle(-1) {
							direct = ""
						}
					case ev.MatchString("ctrl+g") || ev.Code == 7:
						d.detach()
						direct = ""
					default:
						if d.attachment.mode == "pipe" && ev.Code == uv.KeyEnter {
							d.attachment.term.SendText("\n")
						} else {
							d.attachment.term.SendKey(ev)
						}
						continue
					}
				case "connecting", "lost":
					if k == "q" || k == "ctrl+c" {
						return 0, nil
					}
					if k == "r" && d.page == "lost" {
						direct = ""
						connect()
					}
				case "confirm":
					switch k {
					case "tab", "left", "right":
						d.confirm = !d.confirm
					case "esc":
						d.page = "table"
					case "enter":
						if d.confirm {
							d.apply()
						} else {
							d.page = "table"
						}
					}
				case "edit":
					d.editKey(ev)
				case "table":
					if k == "q" || k == "ctrl+c" {
						return 0, nil
					}
					d.tableKey(k)
				}
			default:
				continue
			}
		}
		if err := d.draw(); err != nil {
			return 1, err
		}
		dirty = false
	}
}

func (d *dashboardUI) tableKey(k string) {
	if len(d.rows) == 0 {
		return
	}
	d.target = d.rows[d.index]
	switch k {
	case "up":
		d.index = max(0, d.index-1)
	case "down":
		d.index = min(len(d.rows)-1, d.index+1)
	case "enter":
		if d.target.Info.ID != "" {
			if err := d.attach(d.target); err != nil {
				d.notice = "Session unavailable"
			}
		}
	case "s", "r", "e":
		if d.target.Revision == 0 {
			return
		}
		d.confirm = false
		d.notice = ""
		if k == "e" {
			res, err := d.request("arguments")
			if err != nil {
				d.notice = "Arguments unavailable"
				return
			}
			d.args = res.Args
			d.arg = 0
			d.endOfArgument()
			d.page = "edit"
			return
		}
		if d.target.Info.ID == "" {
			if k == "r" {
				d.action = "start"
				d.apply()
			}
			return
		}
		d.action = "stop"
		if k == "r" {
			d.action = "restart"
		}
		d.page = "confirm"
	}
}

func (d *dashboardUI) endOfArgument() {
	d.cursor = 0
	if len(d.args) > 0 {
		d.cursor = len([]rune(d.args[d.arg]))
	}
}
func (d *dashboardUI) appendText(text string) {
	if len(d.args) == 0 {
		d.args = []string{""}
	}
	r := []rune(d.args[d.arg])
	added := []rune(text)
	d.args[d.arg] = string(slices.Insert(r, d.cursor, added...))
	d.cursor += len(added)
}
func (d *dashboardUI) editKey(ev uv.KeyPressEvent) {
	switch ev.String() {
	case "esc":
		d.page = "table"
		d.args = nil
	case "up":
		d.arg = max(0, d.arg-1)
		d.endOfArgument()
	case "down":
		d.arg = max(0, min(len(d.args)-1, d.arg+1))
		d.endOfArgument()
	case "enter":
		d.arg = min(len(d.args), d.arg+1)
		d.args = slices.Insert(d.args, d.arg, "")
		d.cursor = 0
	case "ctrl+d":
		if len(d.args) > 0 {
			d.args = slices.Delete(d.args, d.arg, d.arg+1)
			d.arg = max(0, min(len(d.args)-1, d.arg))
			d.endOfArgument()
		}
	case "left":
		d.cursor = max(0, d.cursor-1)
	case "right":
		if len(d.args) > 0 {
			d.cursor = min(len([]rune(d.args[d.arg])), d.cursor+1)
		}
	case "home", "ctrl+a":
		d.cursor = 0
	case "end", "ctrl+e":
		d.endOfArgument()
	case "backspace":
		if d.cursor > 0 {
			r := []rune(d.args[d.arg])
			d.args[d.arg] = string(slices.Delete(r, d.cursor-1, d.cursor))
			d.cursor--
		}
	case "ctrl+r":
		d.action = "apply"
		d.confirm = false
		d.page = "confirm"
	default:
		if ev.Text != "" {
			d.appendText(ev.Text)
		}
	}
}
