package main

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/rivo/uniseg"
	"github.com/specialistvlad/clistream/internal/wire"
)

// Quote control characters and arbitrary Unix argument bytes into one printable line.
func formatCommand(argv [][]byte) string {
	parts := make([]string, len(argv))
	for i, b := range argv {
		s := string(b)
		if s == "" || !utf8.ValidString(s) || strings.IndexFunc(s, func(r rune) bool {
			return !unicode.IsPrint(r) || unicode.IsSpace(r) || strings.ContainsRune("'\"\\", r)
		}) >= 0 {
			s = strconv.Quote(s)
		}
		parts[i] = s
	}
	return strings.Join(parts, " ")
}

func parseCommand(line string) ([][]byte, error) {
	var argv [][]byte
	var word strings.Builder
	var quote rune
	started := false
	for line != "" {
		r, n := utf8.DecodeRuneInString(line)
		line = line[n:]
		switch {
		case quote == 0 && unicode.IsSpace(r):
			if started {
				argv = append(argv, []byte(word.String()))
				word.Reset()
				started = false
			}
			continue
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '\'' || r == '"'):
			quote = r
		case r == '\\' && quote != '\'':
			if line == "" {
				return nil, errors.New("Complete the trailing escape.")
			}
			if quote == '"' {
				value, multi, rest, err := strconv.UnquoteChar("\\"+line, '"')
				if err != nil {
					return nil, errors.New("Invalid escape in double quotes.")
				}
				if multi {
					word.WriteRune(value)
				} else {
					word.WriteByte(byte(value))
				}
				line = rest
			} else {
				v, size := utf8.DecodeRuneInString(line)
				word.WriteRune(v)
				line = line[size:]
			}
		default:
			word.WriteRune(r)
		}
		started = true
	}
	if quote != 0 {
		return nil, errors.New("Close the unfinished quote.")
	}
	if started {
		argv = append(argv, []byte(word.String()))
	}
	if len(argv) == 0 || len(argv[0]) == 0 {
		return nil, errors.New("Enter an executable.")
	}
	return argv, nil
}

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
	d.line = newCommandInput(formatCommand(res.Command))
	d.page = "command"
}

func (d *dashboardUI) submitCommand() {
	argv, err := parseCommand(d.line.Value())
	if err != nil {
		d.notice = err.Error()
		return
	}
	res, err := wire.Request(d.conn, wire.Message{Op: "run-command", ID: d.target.ID, Run: d.target.Info.ID, Revision: d.target.Revision, Command: argv})
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
		if err == nil && res.Run != "" && row.Info.ID == res.Run {
			d.line.Reset()
			if d.attach(row) != nil {
				d.notice = "Run ended before attachment."
			}
			return
		}
	}
	d.line.Reset()
	d.notice = "Request ended. Check the current run."
}
