package main

import (
	"github.com/specialistvlad/clistream/internal/commandline"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/rivo/uniseg"
	"github.com/specialistvlad/clistream/internal/wire"
)

func newCommandInput(value string) textinput.Model {
	input := textinput.New()
	input.Prompt = "› "
	input.SetVirtualCursor(false)
	input.SetValue(value)
	input.CursorEnd()
	input.Focus()
	return input
}

// Bubbles indexes the value by rune. Apply its character-edit actions to a
// whole grapheme so an arrow or Backspace cannot split a combining sequence.
func (d *dashboardUI) updateInput(msg tea.Msg) tea.Cmd {
	steps := 1
	if press, ok := msg.(tea.KeyPressMsg); ok {
		km := d.line.KeyMap
		backward := key.Matches(press, km.CharacterBackward, km.DeleteCharacterBackward)
		if backward || key.Matches(press, km.CharacterForward, km.DeleteCharacterForward) {
			value := []rune(d.line.Value())
			pos := d.line.Position()
			if backward {
				g := uniseg.NewGraphemes(string(value[:pos]))
				for g.Next() {
					steps = len(g.Runes())
				}
			} else {
				g := uniseg.NewGraphemes(string(value[pos:]))
				if g.Next() {
					steps = len(g.Runes())
				}
			}
		}
	}
	var cmd tea.Cmd
	for range steps {
		d.line, cmd = d.line.Update(msg)
	}
	return cmd
}

func (d *dashboardUI) openCommand() {
	res, err := d.request("command")
	if err != nil {
		d.notice = "Command unavailable"
		return
	}
	d.line = newCommandInput(commandline.Format(res.Command))
	d.page = "command"
}

func (d *dashboardUI) submitCommand() {
	argv, err := commandline.Parse(d.line.Value())
	if err != nil {
		d.notice = err.Error()
		return
	}
	cols, rows := d.size()
	res, err := wire.Request(d.conn, wire.Message{Cols: cols, Rows: rows, Op: "run-command", ID: d.target.ID, Run: d.target.Info.ID, Revision: d.target.Revision, Command: argv})
	if res.Error == "invalid command" {
		d.notice = "Check the executable on the host."
		return
	}
	d.page = "table"
	if e := d.refresh(); e != nil {
		d.disconnect(e)
		return
	}
	for i, row := range d.rows {
		if row.ID != d.target.ID {
			continue
		}
		d.index = i
		if res.Error == "child launch failed" && row.Revision == res.Revision {
			d.page = "command"
			d.target = row
			d.notice = "Launch failed. Edit the command to try again."
			return
		}
		if err == nil && res.Run != "" {
			d.line.Reset()
			if row.Info.ID == res.Run && d.attach(row) == nil {
				if row.RunResult {
					d.replayRun = res.Run
				}
				return
			}
			if row.RunResult {
				d.showResult(row, res.Run)
			} else {
				d.notice = "Run ended before attachment."
			}
			return
		}
	}
	d.line.Reset()
	d.notice = "Request ended. Check the current run."
}
