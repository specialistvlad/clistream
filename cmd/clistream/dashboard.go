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

	"charm.land/bubbles/v2/textinput"
	pager "charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/wire"
)

type connectionResult struct {
	result        bool
	commandEditor bool
	conn          *quic.Conn
	managed       bool
	err           error
}
type dashboardUI struct {
	result             bool
	replayRun          string
	transcript         string
	output             pager.Model
	exitCode           int
	commandEditor      bool
	line               textinput.Model
	width, height      int
	ctx                context.Context
	link, direct       string
	code               int
	err                error
	refreshAt          time.Time
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

func listRows(c *quic.Conn, managed, editor, result bool) ([]wire.Supervisor, error) {
	var rows []wire.Supervisor
	if managed {
		op := "supervisors"
		if editor {
			op = "command-supervisors"
		}
		if result {
			op = "result-supervisors"
		}
		res, err := wire.Request(c, wire.Message{Op: op})
		if err != nil {
			return nil, err
		}
		rows = res.Supervisors
	} else {
		items, err := client.List(c)
		if err != nil {
			return nil, err
		}
		for _, v := range items {
			rows = append(rows, wire.Supervisor{ID: v.ID, Info: v})
		}
	}
	return rows, nil
}
func (d *dashboardUI) refresh() error {
	rows, err := listRows(d.conn, d.managed, d.commandEditor, d.result)
	if err != nil {
		return err
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
	m.Cols, m.Rows = d.size()
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
	d.output = pager.Model{}
	d.transcript = ""
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
	d.line.Reset()
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
	d.attachment = newViewport(a, d.target.Info.Mode, d.width, max(1, d.height-2))
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

func dashboard(link, direct string) (int, error) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer cancel()
	d := &dashboardUI{ctx: ctx, link: link, direct: direct, page: "connecting", host: "server", width: 80, height: 24}
	if link != "" {
		if invite, err := identity.ParseInvitation(link); err == nil {
			d.host = invite.Address
		}
	} else if _, p, err := identity.CLIProfile(ctx, "client", ""); err == nil {
		d.host = p.Address
	}
	defer func() {
		d.detach()
		if d.conn != nil {
			d.conn.CloseWithError(0, "finished")
		}
	}()
	_, err := tea.NewProgram(d, tea.WithContext(ctx), tea.WithoutSignalHandler(), tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run()
	if err != nil && ctx.Err() == nil {
		return 1, err
	}
	return d.code, d.err
}

func (d *dashboardUI) connect() tea.Cmd {
	ctx, invitation := d.ctx, d.link
	return func() tea.Msg {
		directory, p, err := identity.CLIProfile(ctx, "client", invitation)
		var result connectionResult
		if err == nil {
			result.conn, err = identity.Dial(ctx, directory, p)
		}
		if err == nil {
			var caps []string
			caps, err = wire.Capabilities(result.conn)
			result.result = slices.Contains(caps, wire.RunResult)
			result.managed = slices.Contains(caps, wire.ManagedSupervisors)
			result.commandEditor = slices.Contains(caps, wire.EditCommand)
		}
		if errors.Is(err, os.ErrNotExist) {
			err = errors.New("No saved server. Run clistream PAIRING-LINK using the link from clistreamd.")
		}
		result.err = err
		if result.conn != nil {
			// A dial can finish after the dashboard has already quit.
			stop := context.AfterFunc(ctx, func() { result.conn.CloseWithError(0, "finished") })
			context.AfterFunc(result.conn.Context(), func() { stop() })
		}
		return result
	}
}

type dashboardTick time.Time

func (d *dashboardUI) tick() tea.Cmd {
	delay := time.Second
	if d.page == "connecting" {
		delay = 100 * time.Millisecond
	}
	return tea.Tick(delay, func(now time.Time) tea.Msg { return dashboardTick(now) })
}
func (d *dashboardUI) Init() tea.Cmd { return tea.Batch(d.connect(), d.tick()) }

type viewportOutput struct {
	source  *viewport
	records []terminalRecord
}

func readViewport(v *viewport) tea.Cmd {
	return func() tea.Msg {
		first, ok := <-v.output
		if !ok {
			return nil
		}
		batch := viewportOutput{source: v, records: []terminalRecord{first}}
		for range len(v.output) {
			batch.records = append(batch.records, <-v.output)
		}
		return batch
	}
}

func (d *dashboardUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := d.attachment
	cmd := d.update(msg)
	if before != d.attachment && d.attachment != nil {
		cmd = tea.Batch(cmd, readViewport(d.attachment))
	}
	return d, cmd
}

func (d *dashboardUI) update(msg tea.Msg) tea.Cmd {
	if d.conn != nil && d.conn.Context().Err() != nil {
		d.direct = ""
		d.disconnect(errors.New("Connection closed"))
	}
	switch ev := msg.(type) {
	case connectionResult:
		d.conn = ev.conn
		if ev.err != nil {
			d.disconnect(ev.err)
			break
		}
		d.link = ""
		d.commandEditor = ev.commandEditor
		d.result = ev.result
		d.managed = ev.managed
		d.page = "table"
		if err := d.refresh(); err != nil {
			d.disconnect(err)
			break
		}
		if d.direct != "" {
			i := slices.IndexFunc(d.rows, func(v wire.Supervisor) bool { return v.ID == d.direct || v.Info.ID == d.direct })
			if i < 0 {
				d.code, d.err = 1, errors.New("unknown session")
				return tea.Quit
			}
			d.index = i
			d.target = d.rows[i]
			if err := d.attach(d.target); err != nil {
				d.code, d.err = 1, err
				return tea.Quit
			}
		}
	case viewportOutput:
		if ev.source != d.attachment {
			return nil
		}
		for _, rec := range ev.records {
			if rec.done {
				if d.conn.Context().Err() != nil {
					d.direct = ""
					d.disconnect(errors.New("Connection closed"))
					break
				}
				if d.direct != "" {
					d.code, d.err = rec.code, rec.err
					return tea.Quit
				}
				run := d.target.Info.ID
				replay := d.target.RunResult && (d.replayRun == run || (d.target.Info.Mode == "pipe" && rec.err == nil))
				d.detach()
				if replay {
					d.showResult(d.target, run)
					d.replayRun = ""
					return nil
				}
				d.notice = fmt.Sprintf("Child exited · exit %d", rec.code)
				if rec.err != nil {
					d.notice = "Attachment ended"
				}
				if err := d.refresh(); err != nil {
					d.disconnect(err)
				}
			} else {
				d.attachment.term.Write(rec.data)
			}
		}
		if d.attachment == ev.source {
			return readViewport(ev.source)
		}
	case dashboardTick:
		now := time.Time(ev)
		d.frame++
		if d.page == "table" && now.After(d.refreshAt) {
			if err := d.refresh(); err != nil {
				d.disconnect(err)
			}
			d.refreshAt = now.Add(time.Second)
		}
		return d.tick()
	case tea.WindowSizeMsg:
		d.width, d.height = max(1, ev.Width), max(1, ev.Height)
		if d.page == "result" {
			d.resizeOutput()
		}
		if d.attachment != nil {
			d.attachment.resize(d.width, max(1, d.height-2))
		}
	case tea.PasteMsg:
		if d.page == "attached" {
			d.attachment.term.Paste(ev.Content)
			return nil
		} else if d.page == "command" {
			d.notice = ""
			return d.updateInput(ev)
		} else if d.page == "edit" {
			d.appendText(ev.Content)
		}
	case tea.MouseClickMsg:
		if d.attachment != nil {
			d.attachment.mouse(uv.MouseClickEvent(ev))
		}
	case tea.MouseReleaseMsg:
		if d.attachment != nil {
			d.attachment.mouse(uv.MouseReleaseEvent(ev))
		}
	case tea.MouseMotionMsg:
		if d.attachment != nil {
			d.attachment.mouse(uv.MouseMotionEvent(ev))
		}
	case tea.MouseWheelMsg:
		if d.attachment != nil {
			d.attachment.mouse(uv.MouseWheelEvent(ev))
		}
	case tea.KeyPressMsg:
		key := uv.KeyPressEvent(ev)
		k := strings.ToLower(ev.String())
		switch d.page {
		case "result":
			switch k {
			case "ctrl+c":
				return tea.Quit
			case "enter", "esc", "ctrl+g":
				d.detach()
				if err := d.refresh(); err != nil {
					d.disconnect(err)
				}
			case "home":
				d.output.GotoTop()
			case "end":
				d.output.GotoBottom()
			default:
				d.output, _ = d.output.Update(ev)
			}
		case "attached":
			switch {
			case key.MatchString("ctrl+]") || ev.Code == 0x1d:
				if d.cycle(1) {
					d.direct = ""
				}
			case key.MatchString("ctrl+["):
				if d.cycle(-1) {
					d.direct = ""
				}
			case key.MatchString("ctrl+g") || ev.Code == 7:
				d.detach()
				d.direct = ""
			default:
				if d.attachment.mode == "pipe" && ev.Code == uv.KeyEnter {
					d.attachment.term.SendText("\n")
				} else {
					d.attachment.term.SendKey(key)
				}
				return nil
			}
		case "connecting", "lost":
			if k == "q" || k == "ctrl+c" {
				return tea.Quit
			}
			if k == "r" && d.page == "lost" {
				d.direct = ""
				d.page = "connecting"
				return d.connect()
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
		case "command":
			switch k {
			case "esc":
				d.page = "table"
				d.line.Reset()
				d.notice = ""
			case "enter":
				d.submitCommand()
			default:
				d.notice = ""
				return d.updateInput(ev)
			}
		case "edit":
			d.editKey(key)
		case "table":
			if k == "q" || k == "ctrl+c" {
				return tea.Quit
			}
			d.tableKey(k)
		}
	default:
		if d.page == "command" {
			return d.updateInput(msg)
		}
	}
	return nil
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
		if d.target.Info.ID == "" && d.target.Exit != nil && d.target.RunResult {
			d.showResult(d.target, "")
			return
		}
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
		if k != "s" && d.target.EditableCommand {
			d.openCommand()
			return
		}
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
